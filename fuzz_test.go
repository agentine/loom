package loom

import (
	"bytes"
	"testing"
)

// FuzzReadFrameHeader feeds arbitrary bytes into the frame header parser
// to verify it never panics and always returns a valid header or an error.
func FuzzReadFrameHeader(f *testing.F) {
	// Seed corpus: minimal valid frames.
	// Unmasked, fin=true, opcode=text, length=0
	f.Add([]byte{0x81, 0x00})
	// Masked, fin=true, opcode=binary, length=5 + 4-byte mask + 5 payload bytes
	f.Add([]byte{0x82, 0x85, 0x01, 0x02, 0x03, 0x04, 'h', 'e', 'l', 'l', 'o'})
	// 16-bit extended length
	f.Add([]byte{0x81, 126, 0x00, 0x80})
	// 64-bit extended length
	f.Add([]byte{0x81, 127, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00})
	// Close frame with status code
	f.Add([]byte{0x88, 0x02, 0x03, 0xe8})
	// Ping frame
	f.Add([]byte{0x89, 0x04, 'p', 'i', 'n', 'g'})

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		h, err := readFrameHeader(r)
		if err != nil {
			return // errors are fine — we just must not panic
		}
		// Sanity-check parsed header invariants.
		if h.isControl() {
			if h.length > maxControlPayload {
				t.Errorf("control frame with length %d > %d", h.length, maxControlPayload)
			}
			if !h.fin {
				t.Error("fragmented control frame accepted")
			}
		}
		if h.length < 0 {
			t.Errorf("negative length: %d", h.length)
		}
	})
}

// FuzzMaskBytes ensures the masking function never panics on arbitrary input.
func FuzzMaskBytes(f *testing.F) {
	f.Add([]byte{0xAA, 0xBB, 0xCC, 0xDD}, 0, []byte("hello world"))
	f.Add([]byte{0x00, 0x00, 0x00, 0x00}, 0, []byte{})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 3, make([]byte, 1024))

	f.Fuzz(func(t *testing.T, keyB []byte, pos int, data []byte) {
		if len(keyB) < 4 {
			return
		}
		var key [4]byte
		copy(key[:], keyB[:4])
		if pos < 0 {
			pos = 0
		}
		pos = pos % 4

		// Must not panic.
		result := maskBytes(key, pos, data)
		if result < 0 || result > 3 {
			t.Errorf("maskBytes returned invalid position: %d", result)
		}
	})
}
