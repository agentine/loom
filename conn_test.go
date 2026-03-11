package loom

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// connPair creates a server and client Conn connected via net.Pipe().
func connPair() (server, client *Conn) {
	s, c := net.Pipe()
	server = newConn(s, true)
	client = newConn(c, false)
	return
}

func TestReadWriteMessage_Text(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	want := "hello, world"
	done := make(chan error, 1)

	go func() {
		err := client.WriteMessage(TextMessage, []byte(want))
		done <- err
	}()

	msgType, p, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msgType != TextMessage {
		t.Fatalf("type: got %d, want %d", msgType, TextMessage)
	}
	if string(p) != want {
		t.Fatalf("payload: got %q, want %q", string(p), want)
	}

	if err := <-done; err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

func TestReadWriteMessage_Binary(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	want := []byte{0x00, 0x01, 0x02, 0xFF}
	done := make(chan error, 1)

	go func() {
		done <- client.WriteMessage(BinaryMessage, want)
	}()

	msgType, p, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msgType != BinaryMessage {
		t.Fatalf("type: got %d, want %d", msgType, BinaryMessage)
	}
	if !bytes.Equal(p, want) {
		t.Fatalf("payload: got %x, want %x", p, want)
	}

	if err := <-done; err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

func TestReadWriteMessage_Empty(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- client.WriteMessage(TextMessage, []byte{})
	}()

	msgType, p, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msgType != TextMessage {
		t.Fatalf("type: got %d, want %d", msgType, TextMessage)
	}
	if len(p) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(p))
	}

	if err := <-done; err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

func TestNextReaderNextWriter(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	want := "streaming data"
	done := make(chan error, 1)

	go func() {
		w, err := client.NextWriter(TextMessage)
		if err != nil {
			done <- err
			return
		}
		if _, err := w.Write([]byte(want)); err != nil {
			done <- err
			return
		}
		done <- w.Close()
	}()

	msgType, r, err := server.NextReader()
	if err != nil {
		t.Fatalf("NextReader: %v", err)
	}
	if msgType != TextMessage {
		t.Fatalf("type: got %d, want %d", msgType, TextMessage)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != want {
		t.Fatalf("got %q, want %q", string(data), want)
	}

	if err := <-done; err != nil {
		t.Fatalf("writer: %v", err)
	}
}

func TestSetReadLimit(t *testing.T) {
	server, client := connPair()

	server.SetReadLimit(10)

	go func() {
		client.WriteMessage(TextMessage, []byte("this message is way too long"))
	}()

	_, _, err := server.ReadMessage()
	if err != ErrReadLimit {
		t.Fatalf("got %v, want ErrReadLimit", err)
	}

	// Close both sides to unblock any pending writes.
	server.conn.Close()
	client.conn.Close()
}

func TestPingPong(t *testing.T) {
	server, client := connPair()

	pongReceived := make(chan string, 1)
	client.SetPongHandler(func(data string) error {
		pongReceived <- data
		return nil
	})

	// Start readers first so pipes don't block.
	go func() { server.ReadMessage() }()
	go func() { client.ReadMessage() }()

	// Client sends ping — server's default handler auto-replies with pong.
	err := client.WriteControl(PingMessage, []byte("hello"), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("WriteControl ping: %v", err)
	}

	select {
	case data := <-pongReceived:
		if data != "hello" {
			t.Fatalf("pong data: got %q, want %q", data, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pong")
	}

	server.conn.Close()
	client.conn.Close()
}

func TestCloseHandshake(t *testing.T) {
	server, client := connPair()

	closeReceived := make(chan *CloseError, 1)
	server.SetCloseHandler(func(code int, text string) error {
		msg := FormatCloseMessage(code, "")
		server.writeMu.Lock()
		server.writeFrame(true, opClose, msg)
		server.closeSent = true
		server.writeMu.Unlock()
		closeReceived <- &CloseError{Code: code, Text: text}
		return &CloseError{Code: code, Text: text}
	})

	// Start readers so pipe writes don't block.
	go func() { server.ReadMessage() }()
	go func() { client.ReadMessage() }()

	// Client sends close.
	done := make(chan error, 1)
	go func() {
		msg := FormatCloseMessage(CloseNormalClosure, "bye")
		done <- client.WriteControl(CloseMessage, msg, time.Now().Add(time.Second))
	}()

	select {
	case ce := <-closeReceived:
		if ce.Code != CloseNormalClosure {
			t.Fatalf("close code: got %d, want %d", ce.Code, CloseNormalClosure)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for close")
	}

	if err := <-done; err != nil {
		t.Fatalf("WriteControl close: %v", err)
	}

	server.conn.Close()
	client.conn.Close()
}

func TestIsCloseError_Integration(t *testing.T) {
	err := &CloseError{Code: CloseGoingAway, Text: "leaving"}
	if !IsCloseError(err, CloseGoingAway) {
		t.Fatal("should match")
	}
	if IsCloseError(err, CloseNormalClosure) {
		t.Fatal("should not match")
	}
}

func TestConcurrentWrites(t *testing.T) {
	server, client := connPair()

	// Read all messages on server side.
	received := make(chan []byte, 20)
	go func() {
		for {
			_, p, err := server.ReadMessage()
			if err != nil {
				close(received)
				return
			}
			received <- p
		}
	}()

	// Concurrent writes from client.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := []byte{byte(n)}
			client.WriteMessage(BinaryMessage, msg)
		}(i)
	}
	wg.Wait()

	// Close client to signal server reader to stop.
	client.conn.Close()

	count := 0
	for range received {
		count++
	}
	if count != 10 {
		t.Fatalf("received %d messages, want 10", count)
	}
	server.conn.Close()
}

func TestServerAndClientRoles(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	if !server.isServer {
		t.Fatal("server should be isServer")
	}
	if client.isServer {
		t.Fatal("client should not be isServer")
	}
}

func TestSubprotocol(t *testing.T) {
	server, _ := connPair()
	server.subprotocol = "graphql-ws"
	if server.Subprotocol() != "graphql-ws" {
		t.Fatalf("got %q, want %q", server.Subprotocol(), "graphql-ws")
	}
}

func TestNetConn(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	server := newConn(s, true)
	if server.NetConn() != s {
		t.Fatal("NetConn should return underlying connection")
	}
	if server.UnderlyingConn() != s {
		t.Fatal("UnderlyingConn should return underlying connection")
	}
}

func TestLocalRemoteAddr(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	server := newConn(s, true)
	// net.Pipe addresses are not nil.
	if server.LocalAddr() == nil {
		t.Fatal("LocalAddr should not be nil")
	}
	if server.RemoteAddr() == nil {
		t.Fatal("RemoteAddr should not be nil")
	}
}
