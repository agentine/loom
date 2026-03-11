package loom

import "unsafe"

// maskBytes applies the WebSocket masking algorithm (RFC 6455 §5.3).
// The mask key is XORed byte-by-byte with the payload, rotating through
// the 4-byte key. pos is the starting offset into the key for fragmented
// messages. It returns the new position (pos + len(b)) mod 4.
func maskBytes(key [4]byte, pos int, b []byte) int {
	newPos := (pos + len(b)) & 3

	if len(b) == 0 {
		return newPos
	}

	// Align to key boundary first with byte-by-byte XOR.
	n := pos & 3
	for len(b) > 0 && n != 0 {
		b[0] ^= key[n]
		b = b[1:]
		n = (n + 1) & 3
	}
	// n == 0 now, key-aligned.

	if len(b) >= 8 {
		// Build a 64-bit mask key (repeating the 4-byte key twice).
		var key64 uint64
		kp := (*[8]byte)(unsafe.Pointer(&key64))
		kp[0] = key[0]
		kp[1] = key[1]
		kp[2] = key[2]
		kp[3] = key[3]
		kp[4] = key[0]
		kp[5] = key[1]
		kp[6] = key[2]
		kp[7] = key[3]

		// XOR 8 bytes at a time.
		for len(b) >= 8 {
			v := (*uint64)(unsafe.Pointer(&b[0]))
			*v ^= key64
			b = b[8:]
		}
	}

	// Handle remaining bytes.
	for i := range b {
		b[i] ^= key[i&3]
	}

	return newPos
}

// maskBytesSafe is the pure byte-by-byte fallback, used for testing.
func maskBytesSafe(key [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= key[(pos+i)&3]
	}
	return (pos + len(b)) & 3
}
