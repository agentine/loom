package loom

import (
	"compress/flate"
	"errors"
)

// EnableWriteCompression enables or disables write compression for
// the connection. Compression is only used if it was negotiated during
// the handshake (via EnableCompression on the Upgrader or Dialer).
func (c *Conn) EnableWriteCompression(enable bool) {
	c.writeCompress = enable
}

// SetCompressionLevel sets the flate compression level for writes.
// Valid levels are flate.BestSpeed through flate.BestCompression,
// flate.DefaultCompression, and flate.HuffmanOnly.
func (c *Conn) SetCompressionLevel(level int) error {
	if level < flate.HuffmanOnly || level > flate.BestCompression {
		if level != flate.DefaultCompression {
			return errors.New("websocket: invalid compression level")
		}
	}
	c.compressionLevel = level
	return nil
}
