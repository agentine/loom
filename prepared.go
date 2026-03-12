package loom

import (
	"bytes"
	"crypto/rand"
)

// PreparedMessage caches the pre-encoded wire frames of a message so it
// can be sent efficiently to multiple connections without re-encoding.
// Server-side connections write the cached frame bytes directly (zero-copy).
// Client-side connections reuse the pre-compressed payload but apply a
// fresh masking key per write (required by RFC 6455 §5.1).
type PreparedMessage struct {
	messageType int
	payload     []byte // raw uncompressed payload

	// Pre-encoded complete frames for server connections (no masking).
	frameBytes           []byte // header + payload, uncompressed
	compressedFrameBytes []byte // header + compressed payload (nil if empty data)
	compressedPayload    []byte // compressed payload only, for client reuse
}

// NewPreparedMessage creates a PreparedMessage from the given message
// type and payload. The prepared message can then be sent to many
// connections via Conn.WritePreparedMessage.
func NewPreparedMessage(messageType int, data []byte) (*PreparedMessage, error) {
	pm := &PreparedMessage{
		messageType: messageType,
		payload:     data,
	}

	opcode := opText
	if messageType == BinaryMessage {
		opcode = opBinary
	}

	// Pre-encode the uncompressed server frame (header + payload).
	var buf bytes.Buffer
	h := frameHeader{
		fin:    true,
		opcode: opcode,
		length: int64(len(data)),
	}
	if err := writeFrameHeader(&buf, h); err != nil {
		return nil, err
	}
	buf.Write(data)
	pm.frameBytes = buf.Bytes()

	// Pre-encode the compressed variant for permessage-deflate connections.
	if len(data) > 0 {
		compressed, err := compressData(data, -1) // default compression
		if err == nil {
			pm.compressedPayload = compressed
			var cbuf bytes.Buffer
			ch := frameHeader{
				fin:    true,
				rsv1:   true,
				opcode: opcode,
				length: int64(len(compressed)),
			}
			if err := writeFrameHeader(&cbuf, ch); err == nil {
				cbuf.Write(compressed)
				pm.compressedFrameBytes = cbuf.Bytes()
			}
		}
	}

	return pm, nil
}

// WritePreparedMessage writes a PreparedMessage to the connection.
// For server connections, pre-encoded frame bytes are written directly.
// For client connections, the pre-compressed payload is reused but a
// fresh masking key is applied per RFC 6455 §5.1.
func (c *Conn) WritePreparedMessage(pm *PreparedMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closeSent {
		return ErrCloseSent
	}

	if err := c.setWriteDeadline(); err != nil {
		return err
	}

	// Determine whether to use compressed variant.
	useCompressed := c.compressionNegotiated && c.writeCompress &&
		pm.compressedFrameBytes != nil

	if c.isServer {
		// Server: write pre-encoded frame directly — no masking needed.
		frame := pm.frameBytes
		if useCompressed {
			frame = pm.compressedFrameBytes
		}
		if _, err := c.bw.Write(frame); err != nil {
			return err
		}
		return c.bw.Flush()
	}

	// Client: must mask each frame with a unique key. Reuse the
	// pre-compressed payload to skip compression, but build a new
	// masked frame.
	payload := pm.payload
	opcode := opText
	if pm.messageType == BinaryMessage {
		opcode = opBinary
	}

	h := frameHeader{
		fin:    true,
		opcode: opcode,
		masked: true,
	}

	if useCompressed {
		payload = pm.compressedPayload
		h.rsv1 = true
	}
	h.length = int64(len(payload))
	rand.Read(h.mask[:])

	if err := writeFrameHeader(c.bw, h); err != nil {
		return err
	}

	masked := make([]byte, len(payload))
	copy(masked, payload)
	maskBytes(h.mask, 0, masked)
	if _, err := c.bw.Write(masked); err != nil {
		return err
	}

	return c.bw.Flush()
}
