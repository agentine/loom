package loom

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"sync"
)

// flateWriterPools caches flate.Writers by compression level to reduce allocations.
var flateWriterPools = sync.Map{}

// flateTail is the 4-byte sync marker that permessage-deflate strips from
// the end of compressed data (RFC 7692 §7.2.1).
var flateTail = []byte{0x00, 0x00, 0xff, 0xff}

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

// compressData compresses data using flate and strips the trailing sync marker
// per RFC 7692 §7.2.1.
func compressData(data []byte, level int) ([]byte, error) {
	if level == 0 {
		level = flate.DefaultCompression
	}

	var buf bytes.Buffer

	// Get or create a pool for this compression level.
	poolI, _ := flateWriterPools.LoadOrStore(level, &sync.Pool{
		New: func() interface{} {
			w, _ := flate.NewWriter(nil, level)
			return w
		},
	})
	pool := poolI.(*sync.Pool)
	fw := pool.Get().(*flate.Writer)
	fw.Reset(&buf)

	if _, err := fw.Write(data); err != nil {
		pool.Put(fw)
		return nil, err
	}
	if err := fw.Flush(); err != nil {
		pool.Put(fw)
		return nil, err
	}
	pool.Put(fw)

	// Strip the trailing 4-byte sync marker (0x00 0x00 0xFF 0xFF).
	b := buf.Bytes()
	if len(b) >= 4 && bytes.Equal(b[len(b)-4:], flateTail) {
		b = b[:len(b)-4]
	}

	return b, nil
}

// decompressData decompresses permessage-deflate data by appending the sync
// marker and running it through flate. Enforces a size limit on the
// decompressed output.
func decompressData(data []byte, limit int64) ([]byte, error) {
	// Append the sync marker that was stripped during compression.
	r := io.MultiReader(bytes.NewReader(data), bytes.NewReader(flateTail))
	fr := flate.NewReader(r)
	defer fr.Close()

	var buf bytes.Buffer
	var reader io.Reader = fr
	if limit > 0 {
		reader = io.LimitReader(fr, limit+1) // +1 to detect over-limit
	}

	n, err := io.Copy(&buf, reader)

	if limit > 0 && n > limit {
		return nil, ErrReadLimit
	}

	// io.ErrUnexpectedEOF is expected for permessage-deflate streams
	// because the DEFLATE stream is not terminated with a final block —
	// it's flushed with a sync point then the trailing bytes are stripped.
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	return buf.Bytes(), nil
}
