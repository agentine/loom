package loom

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
)

// roundTrip writes a header and reads it back, checking for equality.
func roundTrip(t *testing.T, h frameHeader) {
	t.Helper()
	var buf bytes.Buffer
	if err := writeFrameHeader(&buf, h); err != nil {
		t.Fatalf("writeFrameHeader: %v", err)
	}
	got, err := readFrameHeader(&buf)
	if err != nil {
		t.Fatalf("readFrameHeader: %v", err)
	}
	if got.fin != h.fin {
		t.Errorf("fin: got %v, want %v", got.fin, h.fin)
	}
	if got.rsv1 != h.rsv1 {
		t.Errorf("rsv1: got %v, want %v", got.rsv1, h.rsv1)
	}
	if got.opcode != h.opcode {
		t.Errorf("opcode: got %d, want %d", got.opcode, h.opcode)
	}
	if got.masked != h.masked {
		t.Errorf("masked: got %v, want %v", got.masked, h.masked)
	}
	if got.length != h.length {
		t.Errorf("length: got %d, want %d", got.length, h.length)
	}
	if got.masked && got.mask != h.mask {
		t.Errorf("mask: got %x, want %x", got.mask, h.mask)
	}
}

func TestFrameRoundTrip_TextSmall(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opText,
		length: 5,
	})
}

func TestFrameRoundTrip_BinaryMedium(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opBinary,
		length: 300, // > 125 triggers 16-bit length
	})
}

func TestFrameRoundTrip_BinaryLarge(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opBinary,
		length: 100000, // > 65535 triggers 64-bit length
	})
}

func TestFrameRoundTrip_Masked(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opText,
		masked: true,
		length: 42,
		mask:   [4]byte{0xDE, 0xAD, 0xBE, 0xEF},
	})
}

func TestFrameRoundTrip_Ping(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opPing,
		length: 5,
	})
}

func TestFrameRoundTrip_Pong(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opPong,
		length: 0,
	})
}

func TestFrameRoundTrip_Close(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opClose,
		length: 2, // close code
	})
}

func TestFrameRoundTrip_RSV1(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		rsv1:   true,
		opcode: opText,
		length: 10,
	})
}

func TestFrameRoundTrip_Continuation(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    false,
		opcode: opContinuation,
		length: 50,
	})
}

func TestFrameReject_OversizedControlFrame(t *testing.T) {
	h := frameHeader{
		fin:    true,
		opcode: opPing,
		length: 200, // > 125
	}
	var buf bytes.Buffer
	writeFrameHeader(&buf, h)
	_, err := readFrameHeader(&buf)
	if err != errControlTooLong {
		t.Fatalf("got %v, want errControlTooLong", err)
	}
}

func TestFrameReject_FragmentedControlFrame(t *testing.T) {
	h := frameHeader{
		fin:    false, // control frame must have FIN set
		opcode: opClose,
		length: 2,
	}
	var buf bytes.Buffer
	writeFrameHeader(&buf, h)
	_, err := readFrameHeader(&buf)
	if err != errControlFragmented {
		t.Fatalf("got %v, want errControlFragmented", err)
	}
}

func TestFrameReject_ReservedOpcode(t *testing.T) {
	// Build raw bytes with reserved opcode 3
	var buf bytes.Buffer
	buf.WriteByte(0x80 | 3) // FIN + opcode 3 (reserved)
	buf.WriteByte(0)        // no mask, length 0
	_, err := readFrameHeader(&buf)
	if err != errReservedOpcode {
		t.Fatalf("got %v, want errReservedOpcode", err)
	}
}

func TestFrameReject_IntegerOverflow_CVE_2020_27813(t *testing.T) {
	// Craft a frame with 64-bit length > math.MaxInt64
	var buf bytes.Buffer
	buf.WriteByte(0x82) // FIN + binary
	buf.WriteByte(127)  // 64-bit extended length

	var lenBytes [8]byte
	// Set MSB to 1, making it > MaxInt64 when interpreted as uint64
	binary.BigEndian.PutUint64(lenBytes[:], uint64(math.MaxInt64)+1)
	buf.Write(lenBytes[:])

	_, err := readFrameHeader(&buf)
	if err != errFrameTooLarge {
		t.Fatalf("got %v, want errFrameTooLarge", err)
	}
}

func TestFrameReject_EOF(t *testing.T) {
	// Empty reader
	_, err := readFrameHeader(bytes.NewReader(nil))
	if err != io.EOF && err != io.ErrUnexpectedEOF {
		t.Fatalf("got %v, want EOF or ErrUnexpectedEOF", err)
	}
}

func TestFrameRoundTrip_ZeroLength(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opText,
		length: 0,
	})
}

func TestFrameRoundTrip_MaxSmallLength(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opBinary,
		length: 125,
	})
}

func TestFrameRoundTrip_MinMediumLength(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opBinary,
		length: 126,
	})
}

func TestFrameRoundTrip_MaxMediumLength(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opBinary,
		length: 65535,
	})
}

func TestFrameRoundTrip_MinLargeLength(t *testing.T) {
	roundTrip(t, frameHeader{
		fin:    true,
		opcode: opBinary,
		length: 65536,
	})
}
