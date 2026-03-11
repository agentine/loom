package loom

import (
	"bytes"
	"testing"
)

func TestMaskBytes_SingleByte(t *testing.T) {
	key := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	b := []byte{0x01}
	maskBytes(key, 0, b)
	if b[0] != 0x01^0xAA {
		t.Fatalf("got %x, want %x", b[0], 0x01^0xAA)
	}
}

func TestMaskBytes_FourBytes(t *testing.T) {
	key := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	b := []byte{0x01, 0x02, 0x03, 0x04}
	want := []byte{0x01 ^ 0xAA, 0x02 ^ 0xBB, 0x03 ^ 0xCC, 0x04 ^ 0xDD}
	maskBytes(key, 0, b)
	if !bytes.Equal(b, want) {
		t.Fatalf("got %x, want %x", b, want)
	}
}

func TestMaskBytes_SevenBytes(t *testing.T) {
	key := [4]byte{0x11, 0x22, 0x33, 0x44}
	original := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70}
	b := make([]byte, len(original))
	copy(b, original)
	maskBytes(key, 0, b)
	// Verify each byte
	for i, v := range original {
		want := v ^ key[i%4]
		if b[i] != want {
			t.Fatalf("byte %d: got %x, want %x", i, b[i], want)
		}
	}
}

func TestMaskBytes_100Bytes(t *testing.T) {
	key := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	original := make([]byte, 100)
	for i := range original {
		original[i] = byte(i)
	}
	b := make([]byte, len(original))
	copy(b, original)
	maskBytes(key, 0, b)
	for i, v := range original {
		want := v ^ key[i%4]
		if b[i] != want {
			t.Fatalf("byte %d: got %x, want %x", i, b[i], want)
		}
	}
}

func TestMaskBytes_Idempotent(t *testing.T) {
	key := [4]byte{0xCA, 0xFE, 0xBA, 0xBE}
	original := []byte("Hello, WebSocket!")
	b := make([]byte, len(original))
	copy(b, original)

	maskBytes(key, 0, b)
	if bytes.Equal(b, original) {
		t.Fatal("masking should change data")
	}
	maskBytes(key, 0, b)
	if !bytes.Equal(b, original) {
		t.Fatal("double masking should restore original")
	}
}

func TestMaskBytes_VariousSizes(t *testing.T) {
	key := [4]byte{0x12, 0x34, 0x56, 0x78}
	for _, size := range []int{0, 1, 3, 4, 7, 8, 15, 16, 31, 32, 100, 1024} {
		original := make([]byte, size)
		for i := range original {
			original[i] = byte(i * 3)
		}

		b := make([]byte, size)
		copy(b, original)
		maskBytes(key, 0, b)

		// Verify each byte.
		for i, v := range original {
			want := v ^ key[i%4]
			if b[i] != want {
				t.Fatalf("size=%d, byte %d: got %x, want %x", size, i, b[i], want)
			}
		}
	}
}

func TestMaskBytes_WithOffset(t *testing.T) {
	key := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	b := []byte{0x01, 0x02, 0x03}
	// Start at pos=1, so first byte XORs with key[1]=0xBB
	want := []byte{0x01 ^ 0xBB, 0x02 ^ 0xCC, 0x03 ^ 0xDD}
	pos := maskBytes(key, 1, b)
	if !bytes.Equal(b, want) {
		t.Fatalf("got %x, want %x", b, want)
	}
	if pos != 0 { // (1+3)%4 == 0
		t.Fatalf("pos: got %d, want 0", pos)
	}
}

func TestMaskBytes_Empty(t *testing.T) {
	key := [4]byte{0xFF, 0xFF, 0xFF, 0xFF}
	pos := maskBytes(key, 2, nil)
	if pos != 2 {
		t.Fatalf("pos: got %d, want 2", pos)
	}
}
