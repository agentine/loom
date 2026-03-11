package loom

import (
	"testing"
)

type testPayload struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestWriteReadJSON(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	want := testPayload{Name: "hello", Value: 42}
	done := make(chan error, 1)

	go func() {
		done <- client.WriteJSON(want)
	}()

	var got testPayload
	if err := server.ReadJSON(&got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if err := <-done; err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
}

func TestReadJSON_BinaryMessageReturnsError(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- client.WriteMessage(BinaryMessage, []byte(`{"key":"val"}`))
	}()

	var v map[string]string
	err := server.ReadJSON(&v)
	if err == nil {
		t.Fatal("expected error for binary message")
	}

	if err := <-done; err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

func TestWriteJSON_InvalidValue(t *testing.T) {
	server, client := connPair()
	defer server.Close()
	defer client.Close()

	// Channels can't be marshalled to JSON.
	ch := make(chan int)
	err := client.WriteJSON(ch)
	if err == nil {
		t.Fatal("expected error for non-marshalable value")
	}
}
