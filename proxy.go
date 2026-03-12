package loom

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
)

// proxyDialer dials through an HTTP proxy using the CONNECT method to
// establish a tunnel to the target. CONNECT is used for both ws:// and
// wss:// because WebSocket upgrades are hop-by-hop protocol switches
// that HTTP forward proxies cannot transparently handle.
func proxyDialer(ctx context.Context, proxyURL *url.URL, targetHostPort string) (net.Conn, error) {
	// Connect to the proxy itself.
	proxyHP := proxyHostPort(proxyURL)
	nd := &net.Dialer{}
	proxyConn, err := nd.DialContext(ctx, "tcp", proxyHP)
	if err != nil {
		return nil, err
	}

	// Establish an HTTP CONNECT tunnel to the target.
	connectReq := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: targetHostPort},
		Host:   targetHostPort,
		Header: make(http.Header),
	}

	// Proxy authentication (Basic).
	if proxyURL.User != nil {
		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()
		cred := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		connectReq.Header.Set("Proxy-Authorization", "Basic "+cred)
	}

	if err := connectReq.Write(proxyConn); err != nil {
		proxyConn.Close()
		return nil, err
	}

	br := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		proxyConn.Close()
		return nil, err
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		proxyConn.Close()
		return nil, errors.New("websocket: proxy CONNECT failed: " + resp.Status)
	}

	return proxyConn, nil
}

// proxyHostPort returns the host:port for the proxy URL.
func proxyHostPort(u *url.URL) string {
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
