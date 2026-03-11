package loom

import (
	"net"
	"sync"
	"testing"
)

func benchmarkWriteMessage(b *testing.B, size int) {
	s, c := net.Pipe()
	server := newConn(s, true)
	client := newConn(c, false)

	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}

	// Reader goroutine to drain incoming frames.
	go func() {
		buf := make([]byte, size+128) // header overhead
		for {
			_, err := s.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	b.SetBytes(int64(size))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.WriteMessage(BinaryMessage, data)
	}

	b.StopTimer()
	server.conn.Close()
	client.conn.Close()
}

func BenchmarkWriteMessage_64B(b *testing.B)   { benchmarkWriteMessage(b, 64) }
func BenchmarkWriteMessage_4KB(b *testing.B)   { benchmarkWriteMessage(b, 4096) }
func BenchmarkWriteMessage_1MB(b *testing.B)   { benchmarkWriteMessage(b, 1024*1024) }

func benchmarkReadMessage(b *testing.B, size int) {
	s, c := net.Pipe()
	server := newConn(s, true)
	client := newConn(c, false)

	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}

	// Writer goroutine to send frames continuously.
	go func() {
		for {
			err := server.WriteMessage(BinaryMessage, data)
			if err != nil {
				return
			}
		}
	}()

	b.SetBytes(int64(size))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := client.ReadMessage()
		if err != nil {
			b.Fatal(err)
		}
	}

	b.StopTimer()
	server.conn.Close()
	client.conn.Close()
}

func BenchmarkReadMessage_64B(b *testing.B)   { benchmarkReadMessage(b, 64) }
func BenchmarkReadMessage_4KB(b *testing.B)   { benchmarkReadMessage(b, 4096) }
func BenchmarkReadMessage_1MB(b *testing.B)   { benchmarkReadMessage(b, 1024*1024) }

func BenchmarkMask(b *testing.B) {
	key := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maskBytes(key, 0, data)
	}
}

func BenchmarkMask_Small(b *testing.B) {
	key := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	data := make([]byte, 64)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maskBytes(key, 0, data)
	}
}

func BenchmarkMask_Large(b *testing.B) {
	key := [4]byte{0xAA, 0xBB, 0xCC, 0xDD}
	data := make([]byte, 1024*1024)

	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		maskBytes(key, 0, data)
	}
}

func BenchmarkWritePreparedMessage(b *testing.B) {
	const numConns = 10
	pm, _ := NewPreparedMessage(BinaryMessage, make([]byte, 256))

	type connPairB struct {
		s, c net.Conn
		conn *Conn
	}
	pairs := make([]connPairB, numConns)
	for i := 0; i < numConns; i++ {
		s, c := net.Pipe()
		pairs[i] = connPairB{s: s, c: c, conn: newConn(c, false)}
		// Drain readers.
		go func(conn net.Conn) {
			buf := make([]byte, 1024)
			for {
				if _, err := conn.Read(buf); err != nil {
					return
				}
			}
		}(s)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < numConns; j++ {
			wg.Add(1)
			go func(c *Conn) {
				defer wg.Done()
				c.WritePreparedMessage(pm)
			}(pairs[j].conn)
		}
		wg.Wait()
	}

	b.StopTimer()
	for i := 0; i < numConns; i++ {
		pairs[i].s.Close()
		pairs[i].c.Close()
	}
}
