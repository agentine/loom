package loom

import (
	"net"
	"sync"
	"testing"
)

func TestPreparedMessage_Basic(t *testing.T) {
	pm, err := NewPreparedMessage(TextMessage, []byte("broadcast"))
	if err != nil {
		t.Fatalf("NewPreparedMessage: %v", err)
	}

	server, client := connPair()
	defer server.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- client.WritePreparedMessage(pm)
	}()

	mt, p, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if mt != TextMessage {
		t.Fatalf("type: got %d, want %d", mt, TextMessage)
	}
	if string(p) != "broadcast" {
		t.Fatalf("payload: got %q, want %q", string(p), "broadcast")
	}

	if err := <-done; err != nil {
		t.Fatalf("WritePreparedMessage: %v", err)
	}
}

func TestPreparedMessage_Broadcast(t *testing.T) {
	pm, err := NewPreparedMessage(TextMessage, []byte("hello everyone"))
	if err != nil {
		t.Fatalf("NewPreparedMessage: %v", err)
	}

	const numConns = 3
	type pair struct {
		server, client *Conn
	}
	pairs := make([]pair, numConns)
	for i := 0; i < numConns; i++ {
		s, c := net.Pipe()
		pairs[i] = pair{
			server: newConn(s, true),
			client: newConn(c, false),
		}
	}

	// Writers: send the prepared message to all clients.
	var wg sync.WaitGroup
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(c *Conn) {
			defer wg.Done()
			c.WritePreparedMessage(pm)
		}(pairs[i].client)
	}

	// Readers: read from all servers.
	results := make([]string, numConns)
	var rg sync.WaitGroup
	for i := 0; i < numConns; i++ {
		rg.Add(1)
		go func(idx int, s *Conn) {
			defer rg.Done()
			_, p, err := s.ReadMessage()
			if err != nil {
				return
			}
			results[idx] = string(p)
		}(i, pairs[i].server)
	}

	wg.Wait()
	for i := 0; i < numConns; i++ {
		pairs[i].client.conn.Close()
	}
	rg.Wait()

	for i, r := range results {
		if r != "hello everyone" {
			t.Fatalf("conn %d: got %q, want %q", i, r, "hello everyone")
		}
	}

	for i := 0; i < numConns; i++ {
		pairs[i].server.conn.Close()
	}
}
