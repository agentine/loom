package loom

import (
	"net/http"
	"strings"
)

// Subprotocols returns the subprotocols requested by the client in the
// Sec-WebSocket-Protocol header.
func Subprotocols(r *http.Request) []string {
	var protocols []string
	for _, v := range r.Header["Sec-Websocket-Protocol"] {
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				protocols = append(protocols, s)
			}
		}
	}
	return protocols
}

// IsWebSocketUpgrade returns true if the request is a WebSocket upgrade request.
func IsWebSocketUpgrade(r *http.Request) bool {
	return headerContains(r.Header, "Connection", "upgrade") &&
		headerContains(r.Header, "Upgrade", "websocket")
}

// headerContains returns true if the header with the given key contains
// the given value (case-insensitive token match).
func headerContains(h http.Header, key, value string) bool {
	for _, v := range h[http.CanonicalHeaderKey(key)] {
		for _, s := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(s), value) {
				return true
			}
		}
	}
	return false
}
