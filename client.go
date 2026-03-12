package loom

import (
	"bufio"
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrBadHandshake is returned when the server response to the opening
// handshake is invalid.
var ErrBadHandshake = errors.New("websocket: bad handshake")

// Dialer provides a means to establish WebSocket connections.
type Dialer struct {
	// NetDial specifies the dial function for creating TCP connections.
	// If NetDialContext is set, NetDial is ignored.
	NetDial func(network, addr string) (net.Conn, error)

	// NetDialContext specifies the dial function for creating TCP connections.
	NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// NetDialTLSContext specifies the dial function for creating TLS connections.
	// If set, TLSClientConfig is ignored for the TLS handshake.
	NetDialTLSContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// Proxy specifies a function to return a proxy for a given Request.
	Proxy func(*http.Request) (*url.URL, error)

	// TLSClientConfig specifies the TLS configuration to use with tls.Dial.
	TLSClientConfig *tls.Config

	// HandshakeTimeout specifies the duration for the handshake to complete.
	HandshakeTimeout time.Duration

	// ReadBufferSize and WriteBufferSize specify I/O buffer sizes in bytes.
	ReadBufferSize  int
	WriteBufferSize int

	// WriteBufferPool is a pool of buffers for write operations.
	WriteBufferPool BufferPool

	// Subprotocols specifies the subprotocols to request from the server.
	Subprotocols []string

	// EnableCompression is a placeholder for permessage-deflate support.
	EnableCompression bool

	// Jar specifies the cookie jar. If set, cookies are sent with the
	// handshake request and updated from the response.
	Jar http.CookieJar
}

// DefaultDialer is a dialer with all fields set to the default values.
var DefaultDialer = &Dialer{
	Proxy:            http.ProxyFromEnvironment,
	HandshakeTimeout: 45 * time.Second,
}

// Dial creates a new client connection by calling DialContext with a
// background context.
func (d *Dialer) Dial(urlStr string, requestHeader http.Header) (*Conn, *http.Response, error) {
	return d.DialContext(context.Background(), urlStr, requestHeader)
}

// DialContext creates a new client connection. Use the response to
// inspect the server's response or to get cookies.
func (d *Dialer) DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (*Conn, *http.Response, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, nil, err
	}

	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
		// allow
	default:
		return nil, nil, errors.New("websocket: bad scheme: " + u.Scheme)
	}

	hostPort := hostPort(u)

	// Apply timeout.
	if d.HandshakeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.HandshakeTimeout)
		defer cancel()
	}

	// Dial the connection.
	netConn, err := d.dial(ctx, u, hostPort)
	if err != nil {
		return nil, nil, err
	}

	// Generate challenge key.
	challengeKey, err := generateChallengeKey()
	if err != nil {
		netConn.Close()
		return nil, nil, err
	}

	// Build the HTTP request.
	reqHeader := make(http.Header)
	reqHeader.Set("Upgrade", "websocket")
	reqHeader.Set("Connection", "Upgrade")
	reqHeader.Set("Sec-WebSocket-Version", "13")
	reqHeader.Set("Sec-WebSocket-Key", challengeKey)
	if len(d.Subprotocols) > 0 {
		reqHeader.Set("Sec-WebSocket-Protocol", strings.Join(d.Subprotocols, ", "))
	}
	if d.EnableCompression {
		reqHeader.Set("Sec-WebSocket-Extensions", "permessage-deflate; server_no_context_takeover; client_no_context_takeover")
	}

	// Copy caller's headers (don't overwrite handshake headers).
	for k, vs := range requestHeader {
		if _, reserved := reqHeader[k]; !reserved {
			for _, v := range vs {
				reqHeader.Add(k, v)
			}
		}
	}

	// Cookie jar.
	if d.Jar != nil {
		for _, cookie := range d.Jar.Cookies(u) {
			reqHeader.Add("Cookie", cookie.String())
		}
	}

	req := &http.Request{
		Method:     "GET",
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     reqHeader,
		Host:       u.Host,
	}

	// Set deadline on the underlying connection for the handshake.
	if deadline, ok := ctx.Deadline(); ok {
		netConn.SetDeadline(deadline)
		defer netConn.SetDeadline(time.Time{})
	}

	// Write the request.
	if err := req.Write(netConn); err != nil {
		netConn.Close()
		return nil, nil, err
	}

	// Read the response.
	br := bufio.NewReader(netConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		netConn.Close()
		return nil, nil, err
	}

	// Store cookies.
	if d.Jar != nil {
		if rc := resp.Cookies(); len(rc) > 0 {
			d.Jar.SetCookies(u, rc)
		}
	}

	// Validate the handshake response.
	if resp.StatusCode != 101 {
		resp.Body.Close()
		netConn.Close()
		return nil, resp, ErrBadHandshake
	}

	if !headerContains(resp.Header, "Connection", "upgrade") ||
		!headerContains(resp.Header, "Upgrade", "websocket") {
		resp.Body.Close()
		netConn.Close()
		return nil, resp, ErrBadHandshake
	}

	acceptKey := computeAcceptKey(challengeKey)
	if resp.Header.Get("Sec-Websocket-Accept") != acceptKey {
		resp.Body.Close()
		netConn.Close()
		return nil, resp, ErrBadHandshake
	}

	// Build the Conn.
	c := newConnFromUpgrade(netConn, bufio.NewReadWriter(br, bufio.NewWriter(netConn)), false)
	c.subprotocol = resp.Header.Get("Sec-Websocket-Protocol")

	// Check if server accepted permessage-deflate.
	if d.EnableCompression {
		for _, ext := range parseExtensions(resp.Header) {
			if ext == "permessage-deflate" {
				c.compressionNegotiated = true
				c.writeCompress = true
				c.compressionLevel = flate.DefaultCompression
				break
			}
		}
	}

	return c, resp, nil
}

// dial establishes the TCP (or TLS) connection, optionally through an
// HTTP proxy when d.Proxy is configured.
func (d *Dialer) dial(ctx context.Context, u *url.URL, hostPort string) (net.Conn, error) {
	useTLS := u.Scheme == "https"

	if useTLS && d.NetDialTLSContext != nil {
		return d.NetDialTLSContext(ctx, "tcp", hostPort)
	}

	var netConn net.Conn
	var err error

	// When a custom dial function is provided, it takes precedence over
	// proxy configuration (the caller controls the transport).
	if d.NetDialContext != nil {
		netConn, err = d.NetDialContext(ctx, "tcp", hostPort)
	} else if d.NetDial != nil {
		netConn, err = d.NetDial("tcp", hostPort)
	} else if proxyURL, pErr := d.proxyURL(u); pErr != nil {
		return nil, pErr
	} else if proxyURL != nil {
		// Dial through the HTTP proxy.
		netConn, err = proxyDialer(ctx, proxyURL, hostPort)
	} else {
		netDialer := &net.Dialer{}
		netConn, err = netDialer.DialContext(ctx, "tcp", hostPort)
	}

	if err != nil {
		return nil, err
	}

	if useTLS {
		cfg := cloneTLSConfig(d.TLSClientConfig)
		if cfg.ServerName == "" {
			host, _, _ := net.SplitHostPort(hostPort)
			cfg.ServerName = host
		}
		tlsConn := tls.Client(netConn, cfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			netConn.Close()
			return nil, err
		}
		netConn = tlsConn
	}

	return netConn, nil
}

// proxyURL returns the proxy URL for the given target URL, or nil if no
// proxy is configured.
func (d *Dialer) proxyURL(u *url.URL) (*url.URL, error) {
	if d.Proxy == nil {
		return nil, nil
	}
	// Build a minimal request to pass to the Proxy function.
	req := &http.Request{URL: u, Host: u.Host}
	return d.Proxy(req)
}

func hostPort(u *url.URL) string {
	host := u.Host
	if u.Port() == "" {
		switch u.Scheme {
		case "https":
			host = net.JoinHostPort(u.Hostname(), "443")
		default:
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	return host
}

func generateChallengeKey() (string, error) {
	p := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, p); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(p), nil
}

func cloneTLSConfig(cfg *tls.Config) *tls.Config {
	if cfg == nil {
		return &tls.Config{}
	}
	return cfg.Clone()
}
