package loom

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestServer creates an httptest server that upgrades to WebSocket
// and optionally echoes messages.
func newTestServer(t *testing.T, subprotocols []string, echo bool) *httptest.Server {
	t.Helper()
	upgrader := Upgrader{
		CheckOrigin:  func(r *http.Request) bool { return true },
		Subprotocols: subprotocols,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if echo {
			for {
				mt, p, err := c.ReadMessage()
				if err != nil {
					return
				}
				if err := c.WriteMessage(mt, p); err != nil {
					return
				}
			}
		}
	}))
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestDial_Basic(t *testing.T) {
	srv := newTestServer(t, nil, true)
	defer srv.Close()

	d := Dialer{}
	conn, resp, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}

	want := "hello from dialer"
	if err := conn.WriteMessage(TextMessage, []byte(want)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	mt, p, err := conn.ReadMessage()
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

func TestDialContext_Cancel(t *testing.T) {
	// Start a listener that accepts but never responds.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold connection open, never respond.
			defer c.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	d := Dialer{}
	_, _, err = d.DialContext(ctx, "ws://"+ln.Addr().String(), nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestDial_TLS(t *testing.T) {
	upgrader := Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		mt, p, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(mt, p)
	}))
	defer srv.Close()

	d := Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	u := "wss" + strings.TrimPrefix(srv.URL, "https")
	conn, resp, err := d.Dial(u, nil)
	if err != nil {
		t.Fatalf("Dial TLS: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}

	want := "tls test"
	conn.WriteMessage(TextMessage, []byte(want))
	mt, p, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if mt != TextMessage || string(p) != want {
		t.Fatalf("echo mismatch: got %d %q", mt, string(p))
	}
}

func TestDial_Subprotocol(t *testing.T) {
	srv := newTestServer(t, []string{"chat", "binary"}, false)
	defer srv.Close()

	d := Dialer{
		Subprotocols: []string{"chat"},
	}
	conn, _, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if conn.Subprotocol() != "chat" {
		t.Fatalf("subprotocol: got %q, want %q", conn.Subprotocol(), "chat")
	}
}

func TestDial_HandshakeTimeout(t *testing.T) {
	// Start a listener that accepts but never responds.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()

	d := Dialer{
		HandshakeTimeout: 100 * time.Millisecond,
	}
	_, _, err = d.Dial("ws://"+ln.Addr().String(), nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDial_BadAcceptKey(t *testing.T) {
	// Server that responds with wrong accept key.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		brw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
		brw.WriteString("Upgrade: websocket\r\n")
		brw.WriteString("Connection: Upgrade\r\n")
		brw.WriteString("Sec-WebSocket-Accept: WRONGKEY\r\n")
		brw.WriteString("\r\n")
		brw.Flush()
	}))
	defer srv.Close()

	d := Dialer{}
	_, _, err := d.Dial(wsURL(srv), nil)
	if err != ErrBadHandshake {
		t.Fatalf("got %v, want ErrBadHandshake", err)
	}
}

func TestDial_NonUpgradeResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a websocket"))
	}))
	defer srv.Close()

	d := Dialer{}
	_, resp, err := d.Dial(wsURL(srv), nil)
	if err != ErrBadHandshake {
		t.Fatalf("got %v, want ErrBadHandshake", err)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestDial_CustomHeaders(t *testing.T) {
	var gotHeader string
	upgrader := Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.Close()
	}))
	defer srv.Close()

	d := Dialer{}
	h := http.Header{}
	h.Set("X-Custom", "test-value")
	conn, _, err := d.Dial(wsURL(srv), h)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()

	if gotHeader != "test-value" {
		t.Fatalf("custom header: got %q, want %q", gotHeader, "test-value")
	}
}

func TestDial_BadScheme(t *testing.T) {
	d := Dialer{}
	_, _, err := d.Dial("ftp://example.com", nil)
	if err == nil {
		t.Fatal("expected error for bad scheme")
	}
}

func TestDefaultDialer(t *testing.T) {
	if DefaultDialer == nil {
		t.Fatal("DefaultDialer should not be nil")
	}
	if DefaultDialer.HandshakeTimeout != 45*time.Second {
		t.Fatalf("HandshakeTimeout: got %v, want 45s", DefaultDialer.HandshakeTimeout)
	}
	if DefaultDialer.Proxy == nil {
		t.Fatal("DefaultDialer.Proxy should not be nil")
	}
}

func TestDial_ProxyHTTPConnect(t *testing.T) {
	// Start a WebSocket echo server.
	srv := newTestServer(t, nil, true)
	defer srv.Close()

	// Start a simple HTTP CONNECT proxy.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			// Establish a TCP connection to the target.
			targetConn, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			// Hijack the client connection.
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijack not supported", http.StatusInternalServerError)
				targetConn.Close()
				return
			}
			clientConn, clientBuf, err := hj.Hijack()
			if err != nil {
				targetConn.Close()
				return
			}
			// Send 200 OK to the client.
			clientBuf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			clientBuf.Flush()
			// Bidirectional copy.
			go func() {
				defer targetConn.Close()
				defer clientConn.Close()
				buf := make([]byte, 4096)
				for {
					n, err := clientConn.Read(buf)
					if n > 0 {
						targetConn.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}()
			buf := make([]byte, 4096)
			for {
				n, err := targetConn.Read(buf)
				if n > 0 {
					clientConn.Write(buf[:n])
				}
				if err != nil {
					return
				}
			}
		} else {
			http.Error(w, "only CONNECT supported", http.StatusMethodNotAllowed)
		}
	}))
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	d := Dialer{
		Proxy:            http.ProxyURL(proxyURL),
		HandshakeTimeout: 5 * time.Second,
	}

	// Use ws:// (not wss://), but force CONNECT by making the target use
	// a different port than 80. Actually, for non-TLS the proxy just
	// forwards. Let's test the CONNECT path by noting ws:// goes through
	// the proxy path regardless.
	conn, resp, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("Dial through proxy: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}

	// Echo test to verify the proxied connection works end-to-end.
	want := "proxied hello"
	if err := conn.WriteMessage(TextMessage, []byte(want)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	mt, p, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if mt != TextMessage || string(p) != want {
		t.Fatalf("echo mismatch: got %d %q, want %d %q", mt, string(p), TextMessage, want)
	}
}

func TestDial_ProxyFuncCalled(t *testing.T) {
	// Verify that the Proxy function is actually invoked during dial.
	srv := newTestServer(t, nil, false)
	defer srv.Close()

	var proxyCalled bool
	d := Dialer{
		Proxy: func(r *http.Request) (*url.URL, error) {
			proxyCalled = true
			// Return nil to indicate no proxy — direct connection.
			return nil, nil
		},
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()

	if !proxyCalled {
		t.Fatal("Proxy function was not called during Dial")
	}
}

func TestDial_ProxyNil(t *testing.T) {
	// When Proxy is nil, dial should work normally (direct connection).
	srv := newTestServer(t, nil, false)
	defer srv.Close()

	d := Dialer{
		Proxy: nil,
	}
	conn, resp, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != 101 {
		t.Fatalf("status: got %d, want 101", resp.StatusCode)
	}
}

func TestDial_ProxyError(t *testing.T) {
	// When the Proxy function returns an error, dial should fail.
	d := Dialer{
		Proxy: func(r *http.Request) (*url.URL, error) {
			return nil, errors.New("proxy lookup failed")
		},
		HandshakeTimeout: 1 * time.Second,
	}
	_, _, err := d.Dial("ws://127.0.0.1:1234", nil)
	if err == nil {
		t.Fatal("expected error when Proxy function returns error")
	}
	if !strings.Contains(err.Error(), "proxy lookup failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDial_ProxyCustomDialOverridesProxy(t *testing.T) {
	// When NetDialContext is set, the Proxy function should NOT be called.
	srv := newTestServer(t, nil, false)
	defer srv.Close()

	var proxyCalled bool
	d := Dialer{
		Proxy: func(r *http.Request) (*url.URL, error) {
			proxyCalled = true
			return nil, nil
		},
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := d.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	conn.Close()

	if proxyCalled {
		t.Fatal("Proxy function should not be called when NetDialContext is set")
	}
}
