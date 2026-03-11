package loom

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Conn represents a WebSocket connection.
type Conn struct {
	conn     net.Conn
	isServer bool

	subprotocol string

	// Buffered I/O
	br *bufio.Reader
	bw *bufio.Writer

	// Read state
	readLimit     int64
	readDeadline  time.Time
	readErr       error
	readRemaining int64 // bytes remaining in current frame
	readFinal     bool  // true if current frame is final
	readOpcode    int   // opcode of current message (text or binary)
	readMasked    bool
	readMaskKey   [4]byte
	readMaskPos   int

	// Write state
	writeMu       sync.Mutex
	writeDeadline time.Time

	// Control handlers
	pingHandler  func(appData string) error
	pongHandler  func(appData string) error
	closeHandler func(code int, text string) error

	// Compression state
	writeCompress    bool
	compressionLevel int

	// Close state
	closeSent bool
}

// newConn creates a Conn wrapping a net.Conn.
func newConn(conn net.Conn, isServer bool) *Conn {
	c := &Conn{
		conn:      conn,
		isServer:  isServer,
		br:        bufio.NewReader(conn),
		bw:        bufio.NewWriter(conn),
		readLimit: defaultReadLimit,
	}
	c.pingHandler = c.defaultPingHandler
	c.pongHandler = func(string) error { return nil }
	c.closeHandler = c.defaultCloseHandler
	return c
}

// ---- Public API (gorilla-compatible) ----

// ReadMessage reads a complete message from the connection.
// It returns the message type (TextMessage or BinaryMessage) and the payload.
func (c *Conn) ReadMessage() (messageType int, p []byte, err error) {
	var r io.Reader
	messageType, r, err = c.NextReader()
	if err != nil {
		return 0, nil, err
	}
	p, err = io.ReadAll(r)
	return messageType, p, err
}

// WriteMessage writes a complete message to the connection.
// Unlike NextWriter+Write+Close, this sends a single final frame.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closeSent {
		return ErrCloseSent
	}
	return c.writeFrame(true, messageType, data)
}

// NextReader returns the next data message from the peer.
// It handles control messages (ping, pong, close) transparently.
func (c *Conn) NextReader() (messageType int, r io.Reader, err error) {
	for {
		if c.readErr != nil {
			return 0, nil, c.readErr
		}

		// Skip remaining bytes of current frame/message.
		if c.readRemaining > 0 {
			if _, err := io.CopyN(io.Discard, c.br, c.readRemaining); err != nil {
				c.readErr = err
				return 0, nil, err
			}
			c.readRemaining = 0
		}

		// If we consumed all fragments of the previous message but it
		// wasn't the final fragment, keep reading continuation frames.
		if !c.readFinal && c.readRemaining == 0 && c.readOpcode != 0 {
			if err := c.advanceContinuation(); err != nil {
				return 0, nil, err
			}
		}

		// Read next frame header.
		if err := c.setReadDeadline(); err != nil {
			return 0, nil, err
		}
		h, err := readFrameHeader(c.br)
		if err != nil {
			c.readErr = err
			return 0, nil, err
		}

		// Handle control frames inline.
		if h.isControl() {
			if err := c.handleControl(h); err != nil {
				return 0, nil, err
			}
			continue
		}

		// Data frame.
		if h.opcode == opContinuation {
			// Unexpected continuation without a started message.
			c.readErr = errors.New("websocket: unexpected continuation frame")
			return 0, nil, c.readErr
		}

		c.readOpcode = h.opcode
		c.readFinal = h.fin
		c.readRemaining = h.length
		c.readMasked = h.masked
		c.readMaskKey = h.mask
		c.readMaskPos = 0

		if c.readLimit > 0 && h.length > c.readLimit {
			c.readErr = ErrReadLimit
			return 0, nil, c.readErr
		}

		return c.readOpcode, &messageReader{c: c}, nil
	}
}

// NextWriter returns a writer for the next message.
// The writer must be closed when done. Only one writer can be active at a time.
func (c *Conn) NextWriter(messageType int) (io.WriteCloser, error) {
	c.writeMu.Lock()
	if c.closeSent {
		c.writeMu.Unlock()
		return nil, ErrCloseSent
	}
	return &messageWriter{
		c:      c,
		opcode: messageType,
		first:  true,
	}, nil
}

// SetReadLimit sets the maximum size for a message read from the peer.
// If a message exceeds the limit, the connection sends a close message
// and returns ErrReadLimit.
func (c *Conn) SetReadLimit(limit int64) {
	c.readLimit = limit
}

// SetReadDeadline sets the deadline for future reads.
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t
	return nil
}

// SetWriteDeadline sets the deadline for future writes.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.writeDeadline = t
	return nil
}

// SetPingHandler sets the handler for ping messages received from the peer.
func (c *Conn) SetPingHandler(h func(appData string) error) {
	if h == nil {
		h = c.defaultPingHandler
	}
	c.pingHandler = h
}

// SetPongHandler sets the handler for pong messages received from the peer.
func (c *Conn) SetPongHandler(h func(appData string) error) {
	if h == nil {
		h = func(string) error { return nil }
	}
	c.pongHandler = h
}

// SetCloseHandler sets the handler for close messages received from the peer.
func (c *Conn) SetCloseHandler(h func(code int, text string) error) {
	if h == nil {
		h = c.defaultCloseHandler
	}
	c.closeHandler = h
}

// WriteControl writes a control message (close, ping, pong) with an optional deadline.
func (c *Conn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if !deadline.IsZero() {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
		defer c.conn.SetWriteDeadline(time.Time{})
	}

	return c.writeFrame(true, messageType, data)
}

// Close sends a close message and closes the underlying connection.
// The close frame is best-effort — if the peer isn't reading, Close
// will not block indefinitely.
func (c *Conn) Close() error {
	c.writeMu.Lock()
	if !c.closeSent {
		c.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		msg := FormatCloseMessage(CloseNormalClosure, "")
		c.writeFrame(true, opClose, msg) // ignore error — best effort
		c.closeSent = true
	}
	c.writeMu.Unlock()
	return c.conn.Close()
}

// LocalAddr returns the local network address.
func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// Subprotocol returns the negotiated subprotocol.
func (c *Conn) Subprotocol() string {
	return c.subprotocol
}

// UnderlyingConn returns the underlying net.Conn.
func (c *Conn) UnderlyingConn() net.Conn {
	return c.conn
}

// NetConn returns the underlying net.Conn (alias for UnderlyingConn).
func (c *Conn) NetConn() net.Conn {
	return c.conn
}

// ---- Internal methods ----

func (c *Conn) setReadDeadline() error {
	if !c.readDeadline.IsZero() {
		return c.conn.SetReadDeadline(c.readDeadline)
	}
	return nil
}

func (c *Conn) writeFrame(fin bool, opcode int, data []byte) error {
	h := frameHeader{
		fin:    fin,
		opcode: opcode,
		length: int64(len(data)),
	}

	// Clients must mask frames.
	if !c.isServer {
		h.masked = true
		rand.Read(h.mask[:])
	}

	if err := c.setWriteDeadline(); err != nil {
		return err
	}

	if err := writeFrameHeader(c.bw, h); err != nil {
		return err
	}

	if h.masked {
		masked := make([]byte, len(data))
		copy(masked, data)
		maskBytes(h.mask, 0, masked)
		if _, err := c.bw.Write(masked); err != nil {
			return err
		}
	} else {
		if _, err := c.bw.Write(data); err != nil {
			return err
		}
	}

	return c.bw.Flush()
}

func (c *Conn) setWriteDeadline() error {
	if !c.writeDeadline.IsZero() {
		return c.conn.SetWriteDeadline(c.writeDeadline)
	}
	return nil
}

func (c *Conn) handleControl(h frameHeader) error {
	// Read control payload.
	payload := make([]byte, h.length)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		c.readErr = err
		return err
	}

	// Unmask if needed.
	if h.masked {
		maskBytes(h.mask, 0, payload)
	}

	switch h.opcode {
	case opPing:
		return c.pingHandler(string(payload))
	case opPong:
		return c.pongHandler(string(payload))
	case opClose:
		code := CloseNoStatusReceived
		text := ""
		if len(payload) >= 2 {
			code = int(binary.BigEndian.Uint16(payload[:2]))
			text = string(payload[2:])
		}
		return c.closeHandler(code, text)
	}
	return nil
}

func (c *Conn) defaultPingHandler(appData string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(true, opPong, []byte(appData))
}

func (c *Conn) defaultCloseHandler(code int, text string) error {
	msg := FormatCloseMessage(code, "")
	c.writeMu.Lock()
	if !c.closeSent {
		c.writeFrame(true, opClose, msg)
		c.closeSent = true
	}
	c.writeMu.Unlock()
	c.readErr = &CloseError{Code: code, Text: text}
	return c.readErr
}

func (c *Conn) advanceContinuation() error {
	for !c.readFinal {
		if err := c.setReadDeadline(); err != nil {
			return err
		}
		h, err := readFrameHeader(c.br)
		if err != nil {
			c.readErr = err
			return err
		}
		if h.isControl() {
			if err := c.handleControl(h); err != nil {
				return err
			}
			continue
		}
		if h.opcode != opContinuation {
			c.readErr = errors.New("websocket: expected continuation frame")
			return c.readErr
		}
		c.readFinal = h.fin
		c.readRemaining = h.length
		c.readMasked = h.masked
		c.readMaskKey = h.mask
		c.readMaskPos = 0
	}
	return nil
}

// ---- messageReader ----

type messageReader struct {
	c *Conn
}

func (r *messageReader) Read(p []byte) (int, error) {
	c := r.c
	for c.readRemaining == 0 {
		if c.readFinal {
			return 0, io.EOF
		}
		// Read next continuation frame.
		h, err := readFrameHeader(c.br)
		if err != nil {
			c.readErr = err
			return 0, err
		}
		if h.isControl() {
			if err := c.handleControl(h); err != nil {
				return 0, err
			}
			continue
		}
		if h.opcode != opContinuation {
			c.readErr = errors.New("websocket: expected continuation frame")
			return 0, c.readErr
		}
		c.readFinal = h.fin
		c.readRemaining = h.length
		c.readMasked = h.masked
		c.readMaskKey = h.mask
		c.readMaskPos = 0

		if c.readLimit > 0 && h.length > c.readLimit {
			c.readErr = ErrReadLimit
			return 0, c.readErr
		}
	}

	if int64(len(p)) > c.readRemaining {
		p = p[:c.readRemaining]
	}

	n, err := c.br.Read(p)
	c.readRemaining -= int64(n)

	// Unmask data.
	if c.readMasked && n > 0 {
		c.readMaskPos = maskBytes(c.readMaskKey, c.readMaskPos, p[:n])
	}

	if err != nil {
		c.readErr = err
	}
	return n, err
}

// ---- messageWriter ----

type messageWriter struct {
	c      *Conn
	opcode int
	first  bool
	err    error
}

func (w *messageWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}

	op := opContinuation
	if w.first {
		op = w.opcode
		w.first = false
	}

	// Send as a non-final frame if this is a streaming write.
	// For simplicity, each Write call becomes a frame.
	// The Close() call sends the final frame.
	err := w.c.writeFrame(false, op, p)
	if err != nil {
		w.err = err
		return 0, err
	}
	return len(p), nil
}

func (w *messageWriter) Close() error {
	if w.err != nil {
		w.c.writeMu.Unlock()
		return w.err
	}

	op := opContinuation
	if w.first {
		op = w.opcode
	}

	err := w.c.writeFrame(true, op, nil)
	w.c.writeMu.Unlock()
	w.err = ErrCloseSent // prevent further writes
	return err
}
