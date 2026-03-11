package loom

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// BufferPool is an interface for getting and returning temporary buffers.
type BufferPool interface {
	Get() interface{}
	Put(interface{})
}

// Upgrader specifies parameters for upgrading an HTTP connection to a
// WebSocket connection.
type Upgrader struct {
	// HandshakeTimeout specifies the duration for the handshake to complete.
	HandshakeTimeout time.Duration

	// ReadBufferSize and WriteBufferSize specify I/O buffer sizes in bytes.
	// If a buffer size is zero, a default size of 4096 is used.
	ReadBufferSize  int
	WriteBufferSize int

	// WriteBufferPool is a pool of buffers for write operations.
	WriteBufferPool BufferPool

	// Subprotocols specifies the server's supported protocols in order of
	// preference. If this field is not nil, the Upgrade method negotiates
	// a subprotocol by selecting the first match.
	Subprotocols []string

	// Error specifies the function for generating HTTP error responses.
	// If Error is nil, http.Error is used.
	Error func(w http.ResponseWriter, r *http.Request, status int, reason error)

	// CheckOrigin returns true if the request Origin is acceptable.
	// If CheckOrigin is nil, a safe default is used: the Origin host
	// must match the request Host header.
	CheckOrigin func(r *http.Request) bool

	// EnableCompression is a placeholder for future permessage-deflate support.
	EnableCompression bool
}

// websocketGUID is the magic GUID from RFC 6455 §4.2.2.
const websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// computeAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455 §4.2.2.
func computeAcceptKey(challengeKey string) string {
	h := sha1.New()
	h.Write([]byte(challengeKey))
	h.Write([]byte(websocketGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Upgrade upgrades the HTTP server connection to the WebSocket protocol.
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*Conn, error) {
	if r.Method != http.MethodGet {
		return u.returnError(w, r, http.StatusMethodNotAllowed, "websocket: request method is not GET")
	}

	if !headerContains(r.Header, "Connection", "upgrade") {
		return u.returnError(w, r, http.StatusBadRequest, "websocket: missing Connection upgrade header")
	}

	if !headerContains(r.Header, "Upgrade", "websocket") {
		return u.returnError(w, r, http.StatusBadRequest, "websocket: missing Upgrade websocket header")
	}

	if r.Header.Get("Sec-Websocket-Version") != "13" {
		return u.returnError(w, r, http.StatusBadRequest, "websocket: unsupported version: "+r.Header.Get("Sec-Websocket-Version"))
	}

	challengeKey := r.Header.Get("Sec-Websocket-Key")
	if challengeKey == "" {
		return u.returnError(w, r, http.StatusBadRequest, "websocket: missing Sec-WebSocket-Key")
	}

	checkOrigin := u.CheckOrigin
	if checkOrigin == nil {
		checkOrigin = checkSameOrigin
	}
	if !checkOrigin(r) {
		return u.returnError(w, r, http.StatusForbidden, "websocket: request origin not allowed")
	}

	// Negotiate subprotocol.
	subprotocol := u.selectSubprotocol(r)

	// Hijack the connection.
	hj, ok := w.(http.Hijacker)
	if !ok {
		return u.returnError(w, r, http.StatusInternalServerError, "websocket: server does not support hijacking")
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		return u.returnError(w, r, http.StatusInternalServerError, "websocket: hijack failed: "+err.Error())
	}

	if u.HandshakeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(u.HandshakeTimeout))
		defer conn.SetWriteDeadline(time.Time{})
	}

	// Write the 101 response.
	var buf strings.Builder
	buf.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	buf.WriteString("Upgrade: websocket\r\n")
	buf.WriteString("Connection: Upgrade\r\n")
	buf.WriteString("Sec-WebSocket-Accept: " + computeAcceptKey(challengeKey) + "\r\n")
	if subprotocol != "" {
		buf.WriteString("Sec-WebSocket-Protocol: " + subprotocol + "\r\n")
	}
	// Include extra response headers.
	for k, vs := range responseHeader {
		for _, v := range vs {
			buf.WriteString(k + ": " + v + "\r\n")
		}
	}
	buf.WriteString("\r\n")

	if _, err := brw.WriteString(buf.String()); err != nil {
		conn.Close()
		return nil, err
	}
	if err := brw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}

	// Build the Conn.
	c := newConnFromUpgrade(conn, brw, true)
	c.subprotocol = subprotocol
	return c, nil
}

func (u *Upgrader) selectSubprotocol(r *http.Request) string {
	if u.Subprotocols == nil {
		return ""
	}
	clientProtocols := Subprotocols(r)
	for _, sp := range u.Subprotocols {
		for _, cp := range clientProtocols {
			if sp == cp {
				return sp
			}
		}
	}
	return ""
}

func (u *Upgrader) returnError(w http.ResponseWriter, r *http.Request, status int, reason string) (*Conn, error) {
	err := errors.New(reason)
	if u.Error != nil {
		u.Error(w, r, status, err)
	} else {
		http.Error(w, reason, status)
	}
	return nil, err
}

// checkSameOrigin checks that the Origin header matches the Host header.
func checkSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Strip scheme from origin to compare host.
	originHost := origin
	if i := strings.Index(origin, "://"); i >= 0 {
		originHost = origin[i+3:]
	}
	// Strip port from origin host and request host for comparison.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if h, _, err := net.SplitHostPort(originHost); err == nil {
		originHost = h
	}
	return strings.EqualFold(originHost, host)
}

// newConnFromUpgrade creates a Conn from a hijacked connection, reusing
// the existing bufio.ReadWriter from the HTTP server.
func newConnFromUpgrade(conn net.Conn, brw *bufio.ReadWriter, isServer bool) *Conn {
	c := &Conn{
		conn:      conn,
		isServer:  isServer,
		br:        brw.Reader,
		bw:        brw.Writer,
		readLimit: defaultReadLimit,
	}
	c.pingHandler = c.defaultPingHandler
	c.pongHandler = func(string) error { return nil }
	c.closeHandler = c.defaultCloseHandler
	return c
}
