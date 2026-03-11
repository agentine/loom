package loom

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeUpgradeRequest creates a valid WebSocket upgrade request targeting the given URL.
func makeUpgradeRequest(url string) *http.Request {
	r, _ := http.NewRequest("GET", url, nil)
	r.Header.Set("Connection", "upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-Websocket-Version", "13")
	r.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return r
}

// dialAndUpgrade sends a raw HTTP upgrade request and returns the response.
func dialAndUpgrade(t *testing.T, url string, extraHeaders map[string]string) (net.Conn, *http.Response) {
	t.Helper()

	// Strip scheme.
	addr := url
	if strings.HasPrefix(addr, "http://") {
		addr = addr[7:]
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	reqLines := []string{
		"GET / HTTP/1.1",
		"Host: " + addr,
		"Connection: Upgrade",
		"Upgrade: websocket",
		"Sec-WebSocket-Version: 13",
		"Sec-WebSocket-Key: " + key,
	}
	for k, v := range extraHeaders {
		reqLines = append(reqLines, k+": "+v)
	}
	reqLines = append(reqLines, "", "")
	req := strings.Join(reqLines, "\r\n")

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatalf("write request: %v", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		t.Fatalf("read response: %v", err)
	}

	return conn, resp
}

func TestUpgrade_Basic(t *testing.T) {
	upgrader := Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Echo one message.
		mt, p, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(mt, p)
	}))
	defer srv.Close()

	conn, resp := dialAndUpgrade(t, srv.URL, nil)
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}
	if resp.Header.Get("Upgrade") != "websocket" {
		t.Fatalf("Upgrade header: got %q, want %q", resp.Header.Get("Upgrade"), "websocket")
	}

	// Verify accept key.
	wantAccept := computeAcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	if resp.Header.Get("Sec-Websocket-Accept") != wantAccept {
		t.Fatalf("accept key: got %q, want %q", resp.Header.Get("Sec-Websocket-Accept"), wantAccept)
	}
}

func TestUpgrade_RejectNonGET(t *testing.T) {
	upgrader := Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
	}))
	defer srv.Close()

	// Send POST instead of GET.
	addr := srv.URL[7:] // strip http://
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := "POST / HTTP/1.1\r\nHost: " + addr + "\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestUpgrade_RejectWrongVersion(t *testing.T) {
	upgrader := Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
	}))
	defer srv.Close()

	addr := srv.URL[7:]
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := "GET / HTTP/1.1\r\nHost: " + addr + "\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 8\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n"
	conn.Write([]byte(req))

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestUpgrade_RejectCrossOrigin(t *testing.T) {
	upgrader := Upgrader{} // default CheckOrigin = checkSameOrigin
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
	}))
	defer srv.Close()

	conn, resp := dialAndUpgrade(t, srv.URL, map[string]string{
		"Origin": "http://evil.example.com",
	})
	defer conn.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestUpgrade_AcceptSameOrigin(t *testing.T) {
	upgrader := Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.Close()
	}))
	defer srv.Close()

	addr := srv.URL[7:] // host:port
	conn, resp := dialAndUpgrade(t, srv.URL, map[string]string{
		"Origin": "http://" + addr,
	})
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}
}

func TestUpgrade_CustomCheckOriginAllowAll(t *testing.T) {
	upgrader := Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.Close()
	}))
	defer srv.Close()

	conn, resp := dialAndUpgrade(t, srv.URL, map[string]string{
		"Origin": "http://evil.example.com",
	})
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101 (custom CheckOrigin should allow all)", resp.StatusCode)
	}
}

func TestUpgrade_SubprotocolNegotiation(t *testing.T) {
	upgrader := Upgrader{
		Subprotocols: []string{"chat", "binary"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.Close()
	}))
	defer srv.Close()

	conn, resp := dialAndUpgrade(t, srv.URL, map[string]string{
		"Sec-WebSocket-Protocol": "chat",
	})
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}
	if resp.Header.Get("Sec-Websocket-Protocol") != "chat" {
		t.Fatalf("subprotocol: got %q, want %q", resp.Header.Get("Sec-Websocket-Protocol"), "chat")
	}
}

func TestUpgrade_CustomResponseHeaders(t *testing.T) {
	upgrader := Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := http.Header{}
		h.Set("X-Custom", "hello")
		c, err := upgrader.Upgrade(w, r, h)
		if err != nil {
			return
		}
		c.Close()
	}))
	defer srv.Close()

	conn, resp := dialAndUpgrade(t, srv.URL, nil)
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}
	if resp.Header.Get("X-Custom") != "hello" {
		t.Fatalf("X-Custom: got %q, want %q", resp.Header.Get("X-Custom"), "hello")
	}
}

func TestComputeAcceptKey(t *testing.T) {
	// RFC 6455 §4.2.2 example.
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(websocketGUID))
	want := base64.StdEncoding.EncodeToString(h.Sum(nil))

	got := computeAcceptKey(key)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// Known expected value from RFC.
	if got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("got %q, want %q", got, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=")
	}
}

func TestUpgrade_EchoRoundTrip(t *testing.T) {
	upgrader := Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			mt, p, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(mt, p); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	// Connect and do the handshake manually, then wrap in a Conn.
	addr := srv.URL[7:]
	netConn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer netConn.Close()

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	req := "GET / HTTP/1.1\r\nHost: " + addr + "\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n\r\n"
	netConn.Write([]byte(req))

	br := bufio.NewReader(netConn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}

	// Wrap the raw connection as a client-side Conn (reusing the buffered reader).
	client := newConnFromUpgrade(netConn, bufio.NewReadWriter(br, bufio.NewWriter(netConn)), false)
	defer client.Close()

	want := "hello from loom"
	if err := client.WriteMessage(TextMessage, []byte(want)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	mt, p, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if mt != TextMessage {
		t.Fatalf("type: got %d, want %d", mt, TextMessage)
	}
	if string(p) != want {
		t.Fatalf("payload: got %q, want %q", string(p), want)
	}
}

func TestUpgrade_HandshakeTimeout(t *testing.T) {
	upgrader := Upgrader{
		HandshakeTimeout: 50 * time.Millisecond,
		CheckOrigin:      func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.Close()
	}))
	defer srv.Close()

	// Just verify the upgrade succeeds — the timeout is for slow clients;
	// a fast client should complete fine.
	conn, resp := dialAndUpgrade(t, srv.URL, nil)
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}
}
