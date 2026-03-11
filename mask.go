package loom

// maskBytes applies the WebSocket masking algorithm (RFC 6455 §5.3).
// The mask key is XORed byte-by-byte with the payload, rotating through
// the 4-byte key. pos is the starting offset into the key for fragmented
// messages. It returns the new position (pos + len(b)) mod 4.
func maskBytes(key [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= key[(pos+i)&3]
	}
	return (pos + len(b)) & 3
}
