package loom

import (
	"bytes"
	"encoding/binary"
	"errors"
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

// writeRawFrame writes a raw WebSocket frame (unmasked, from a server) to w.
func writeRawFrame(w io.Writer, fin bool, opcode int, payload []byte) error {
	h := frameHeader{
		fin:    fin,
		opcode: opcode,
		length: int64(len(payload)),
	}
	if err := writeFrameHeader(w, h); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

// writeRawFrameRSV writes a raw frame with RSV bits set.
func writeRawFrameRSV(w io.Writer, fin bool, rsv1, rsv2, rsv3 bool, opcode int, payload []byte) error {
	h := frameHeader{
		fin:    fin,
		rsv1:   rsv1,
		rsv2:   rsv2,
		rsv3:   rsv3,
		opcode: opcode,
		length: int64(len(payload)),
	}
	if err := writeFrameHeader(w, h); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

// writeRawCloseFrame writes a close frame with the given code.
func writeRawCloseFrame(w io.Writer, code int) error {
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, uint16(code))
	return writeRawFrame(w, true, opClose, payload)
}

// --- Bug #97 Tests: ReadLimit per-message enforcement ---

func TestReadLimit_FragmentedMessage(t *testing.T) {
	// A message fragmented into frames each smaller than readLimit,
	// but whose total exceeds readLimit, should return ErrReadLimit.
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)
	server.SetReadLimit(10) // limit is 10 bytes total

	// Suppress close handler writing back (pipe might be closed).
	server.SetCloseHandler(func(code int, text string) error {
		server.readErr = &CloseError{Code: code, Text: text}
		return server.readErr
	})

	// Writer goroutine: send a message as 3 fragments of 5 bytes each (total 15 > 10).
	go func() {
		chunk := []byte("AAAAA") // 5 bytes each
		// First frame: opcode=text, fin=false
		writeRawFrame(c, false, opText, chunk)
		// Second frame: continuation, fin=false
		writeRawFrame(c, false, opContinuation, chunk)
		// Third frame: continuation, fin=true
		writeRawFrame(c, true, opContinuation, chunk)
	}()

	_, _, err := server.ReadMessage()
	if err != ErrReadLimit {
		t.Fatalf("got %v, want ErrReadLimit", err)
	}
}

func TestReadLimit_FragmentedMessage_ExactLimit(t *testing.T) {
	// A fragmented message whose total equals readLimit should succeed.
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)
	server.SetReadLimit(10) // limit is 10 bytes total

	// Writer goroutine: send 2 fragments of 5 bytes each (total 10 == limit).
	go func() {
		chunk := []byte("AAAAA") // 5 bytes each
		writeRawFrame(c, false, opText, chunk)
		writeRawFrame(c, true, opContinuation, chunk)
	}()

	_, p, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(p) != 10 {
		t.Fatalf("expected 10 bytes, got %d", len(p))
	}
}

func TestReadLimit_SingleFrameExceedsLimit(t *testing.T) {
	// A single frame exceeding readLimit should still fail (regression check).
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)
	server.SetReadLimit(5)

	go func() {
		writeRawFrame(c, true, opText, []byte("too long!!"))
	}()

	_, _, err := server.ReadMessage()
	if err != ErrReadLimit {
		t.Fatalf("got %v, want ErrReadLimit", err)
	}
}

// --- Bug #98 Tests: RSV bit validation and close code validation ---

func TestRSV1_RejectedWithoutCompression(t *testing.T) {
	// RSV1=1 without compression negotiated should be rejected.
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)
	// compressionNegotiated defaults to false

	go func() {
		writeRawFrameRSV(c, true, true, false, false, opText, []byte("hello"))
	}()

	_, _, err := server.ReadMessage()
	if err == nil {
		t.Fatal("expected error for RSV1 set without compression")
	}
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Code != CloseProtocolError {
		t.Fatalf("got %v, want CloseProtocolError", err)
	}
}

func TestRSV1_AllowedWithCompression(t *testing.T) {
	// RSV1=1 should be accepted when compression is negotiated.
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)
	server.compressionNegotiated = true

	go func() {
		writeRawFrameRSV(c, true, true, false, false, opText, []byte("hello"))
	}()

	msgType, p, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgType != TextMessage {
		t.Fatalf("type: got %d, want %d", msgType, TextMessage)
	}
	if string(p) != "hello" {
		t.Fatalf("payload: got %q, want %q", string(p), "hello")
	}
}

func TestRSV2_Rejected(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)

	go func() {
		writeRawFrameRSV(c, true, false, true, false, opText, []byte("hello"))
	}()

	_, _, err := server.ReadMessage()
	if err == nil {
		t.Fatal("expected error for RSV2 set")
	}
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Code != CloseProtocolError {
		t.Fatalf("got %v, want CloseProtocolError", err)
	}
}

func TestRSV3_Rejected(t *testing.T) {
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)

	go func() {
		writeRawFrameRSV(c, true, false, false, true, opText, []byte("hello"))
	}()

	_, _, err := server.ReadMessage()
	if err == nil {
		t.Fatal("expected error for RSV3 set")
	}
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Code != CloseProtocolError {
		t.Fatalf("got %v, want CloseProtocolError", err)
	}
}

func TestCloseCode_InvalidCodes(t *testing.T) {
	invalidCodes := []int{
		0,    // < 1000
		999,  // < 1000
		1004, // reserved
		1005, // must not appear in close frame
		1006, // must not appear in close frame
		1015, // must not appear in close frame
		1016, // unassigned
		2999, // unassigned
		5000, // out of range
		9999, // out of range
	}

	for _, code := range invalidCodes {
		t.Run(
			"code_"+string(rune('0'+code/1000))+string(rune('0'+(code/100)%10))+string(rune('0'+(code/10)%10))+string(rune('0'+code%10)),
			func(t *testing.T) {
				s, c := net.Pipe()
				defer s.Close()
				defer c.Close()

				server := newConn(s, true)
				// Suppress default close handler writing back.
				server.SetCloseHandler(func(code int, text string) error {
					return &CloseError{Code: code, Text: text}
				})

				go func() {
					writeRawCloseFrame(c, code)
				}()

				_, _, err := server.ReadMessage()
				if err == nil {
					t.Fatalf("expected error for invalid close code %d", code)
				}
				var ce *CloseError
				if !errors.As(err, &ce) || ce.Code != CloseProtocolError {
					t.Fatalf("code %d: got %v, want CloseProtocolError", code, err)
				}
			},
		)
	}
}

func TestCloseCode_ValidCodes(t *testing.T) {
	validCodes := []int{
		1000, // normal closure
		1001, // going away
		1002, // protocol error
		1003, // unsupported data
		1007, // invalid frame payload data
		1008, // policy violation
		1009, // message too big
		1010, // mandatory extension
		1011, // internal server error
		1012, // service restart
		1013, // try again later
		3000, // registered: libraries
		3999, // registered: libraries
		4000, // private use
		4999, // private use
	}

	for _, code := range validCodes {
		t.Run(
			"code_"+string(rune('0'+code/1000))+string(rune('0'+(code/100)%10))+string(rune('0'+(code/10)%10))+string(rune('0'+code%10)),
			func(t *testing.T) {
				s, c := net.Pipe()
				defer s.Close()
				defer c.Close()

				server := newConn(s, true)
				receivedCode := make(chan int, 1)
				server.SetCloseHandler(func(code int, text string) error {
					receivedCode <- code
					return &CloseError{Code: code, Text: text}
				})

				go func() {
					writeRawCloseFrame(c, code)
				}()

				_, _, err := server.ReadMessage()
				// We expect a CloseError but with the code we sent (not protocol error).
				var ce *CloseError
				if !errors.As(err, &ce) {
					t.Fatalf("code %d: expected CloseError, got %v", code, err)
				}
				if ce.Code != code {
					t.Fatalf("code %d: got code %d", code, ce.Code)
				}

				select {
				case got := <-receivedCode:
					if got != code {
						t.Fatalf("handler got code %d, want %d", got, code)
					}
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for close handler")
				}
			},
		)
	}
}

func TestCloseCode_SingleBytePayload(t *testing.T) {
	// A close frame with exactly 1 byte payload is invalid per RFC 6455.
	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()

	server := newConn(s, true)
	server.SetCloseHandler(func(code int, text string) error {
		return &CloseError{Code: code, Text: text}
	})

	go func() {
		// Manually write a close frame with 1-byte payload.
		writeRawFrame(c, true, opClose, []byte{0x42})
	}()

	_, _, err := server.ReadMessage()
	if err == nil {
		t.Fatal("expected error for 1-byte close payload")
	}
	var ce *CloseError
	if !errors.As(err, &ce) || ce.Code != CloseProtocolError {
		t.Fatalf("got %v, want CloseProtocolError", err)
	}
}
