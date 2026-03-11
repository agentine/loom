package loom

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

// Opcodes defined in RFC 6455 §5.2.
const (
	opContinuation = 0
	opText         = 1
	opBinary       = 2
	// 3-7 reserved for further non-control frames
	opClose = 8
	opPing  = 9
	opPong  = 10
	// 11-15 reserved for further control frames
)

// maxControlPayload is the maximum payload length for control frames (RFC 6455 §5.5).
const maxControlPayload = 125

// errFrameTooLarge is returned when a frame header declares a payload length
// that would overflow int64 or exceed safe limits.
var errFrameTooLarge = errors.New("websocket: frame payload length overflow")

// errControlTooLong is returned for control frames with payload > 125 bytes.
var errControlTooLong = errors.New("websocket: control frame payload exceeds 125 bytes")

// errControlFragmented is returned for fragmented control frames.
var errControlFragmented = errors.New("websocket: control frame must not be fragmented")

// errReservedOpcode is returned for reserved (undefined) opcodes.
var errReservedOpcode = errors.New("websocket: reserved opcode")

// frameHeader holds the parsed WebSocket frame header.
type frameHeader struct {
	fin    bool
	rsv1   bool // used by permessage-deflate compression
	rsv2   bool
	rsv3   bool
	opcode int
	masked bool
	length int64
	mask   [4]byte
}

// isControl returns true if the opcode is a control frame (opcode >= 8).
func (h frameHeader) isControl() bool {
	return h.opcode >= 8
}

// readFrameHeader reads a WebSocket frame header from r.
// It validates the header per RFC 6455 and returns security-relevant errors.
func readFrameHeader(r io.Reader) (frameHeader, error) {
	var h frameHeader
	var buf [8]byte

	// Read first 2 bytes: FIN/RSV/opcode and MASK/payload-length.
	if _, err := io.ReadFull(r, buf[:2]); err != nil {
		return h, err
	}

	b0, b1 := buf[0], buf[1]

	h.fin = b0&0x80 != 0
	h.rsv1 = b0&0x40 != 0
	h.rsv2 = b0&0x20 != 0
	h.rsv3 = b0&0x10 != 0
	h.opcode = int(b0 & 0x0F)
	h.masked = b1&0x80 != 0

	// Validate opcode: reject reserved opcodes.
	if !validOpcode(h.opcode) {
		return h, errReservedOpcode
	}

	// Parse payload length.
	payloadLen := int64(b1 & 0x7F)

	switch {
	case payloadLen <= 125:
		h.length = payloadLen
	case payloadLen == 126:
		// 16-bit extended payload length.
		if _, err := io.ReadFull(r, buf[:2]); err != nil {
			return h, err
		}
		h.length = int64(binary.BigEndian.Uint16(buf[:2]))
	case payloadLen == 127:
		// 64-bit extended payload length.
		if _, err := io.ReadFull(r, buf[:8]); err != nil {
			return h, err
		}
		v := binary.BigEndian.Uint64(buf[:8])
		// RFC 6455 §5.2: most significant bit must be 0.
		// Also protect against int64 overflow (CVE-2020-27813).
		if v > math.MaxInt64 {
			return h, errFrameTooLarge
		}
		h.length = int64(v)
	}

	// Control frame validation (RFC 6455 §5.5).
	if h.isControl() {
		if h.length > maxControlPayload {
			return h, errControlTooLong
		}
		if !h.fin {
			return h, errControlFragmented
		}
	}

	// Read masking key if present.
	if h.masked {
		if _, err := io.ReadFull(r, h.mask[:]); err != nil {
			return h, err
		}
	}

	return h, nil
}

// writeFrameHeader writes a WebSocket frame header to w.
func writeFrameHeader(w io.Writer, h frameHeader) error {
	var buf [14]byte // max header size: 2 + 8 (extended length) + 4 (mask key)
	n := 0

	var b0 byte
	if h.fin {
		b0 |= 0x80
	}
	if h.rsv1 {
		b0 |= 0x40
	}
	if h.rsv2 {
		b0 |= 0x20
	}
	if h.rsv3 {
		b0 |= 0x10
	}
	b0 |= byte(h.opcode & 0x0F)
	buf[0] = b0
	n++

	var b1 byte
	if h.masked {
		b1 |= 0x80
	}

	switch {
	case h.length <= 125:
		b1 |= byte(h.length)
		buf[1] = b1
		n++
	case h.length <= 65535:
		b1 |= 126
		buf[1] = b1
		n++
		binary.BigEndian.PutUint16(buf[n:], uint16(h.length))
		n += 2
	default:
		b1 |= 127
		buf[1] = b1
		n++
		binary.BigEndian.PutUint64(buf[n:], uint64(h.length))
		n += 8
	}

	if h.masked {
		copy(buf[n:], h.mask[:])
		n += 4
	}

	_, err := w.Write(buf[:n])
	return err
}

// validOpcode returns true for defined opcodes (0-2, 8-10).
func validOpcode(op int) bool {
	switch op {
	case opContinuation, opText, opBinary, opClose, opPing, opPong:
		return true
	default:
		return false
	}
}
