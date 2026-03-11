package loom

import (
	"net/http"
	"testing"
)

func TestSubprotocols(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Sec-Websocket-Protocol", "chat, binary")
	got := Subprotocols(r)
	if len(got) != 2 || got[0] != "chat" || got[1] != "binary" {
		t.Fatalf("got %v, want [chat binary]", got)
	}
}

func TestSubprotocols_Multiple(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Add("Sec-Websocket-Protocol", "chat")
	r.Header.Add("Sec-Websocket-Protocol", "binary")
	got := Subprotocols(r)
	if len(got) != 2 || got[0] != "chat" || got[1] != "binary" {
		t.Fatalf("got %v, want [chat binary]", got)
	}
}

func TestSubprotocols_Empty(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	got := Subprotocols(r)
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	if !IsWebSocketUpgrade(r) {
		t.Fatal("should be upgrade")
	}
}

func TestIsWebSocketUpgrade_False(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	if IsWebSocketUpgrade(r) {
		t.Fatal("should not be upgrade")
	}
}

func TestIsWebSocketUpgrade_CaseInsensitive(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Connection", "UPGRADE")
	r.Header.Set("Upgrade", "WEBSOCKET")
	if !IsWebSocketUpgrade(r) {
		t.Fatal("should be upgrade (case insensitive)")
	}
}
