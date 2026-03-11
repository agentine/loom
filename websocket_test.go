package loom

import (
	"testing"
)

func TestCloseError(t *testing.T) {
	e := &CloseError{Code: CloseNormalClosure, Text: "bye"}
	if e.Error() != "websocket: close 1000 bye" {
		t.Fatalf("unexpected: %s", e.Error())
	}
}

func TestIsCloseError(t *testing.T) {
	err := &CloseError{Code: CloseNormalClosure, Text: "bye"}
	if !IsCloseError(err, CloseNormalClosure) {
		t.Fatal("should match")
	}
	if IsCloseError(err, CloseGoingAway) {
		t.Fatal("should not match")
	}
	if IsCloseError(err, CloseGoingAway, CloseNormalClosure) {
		// should match because CloseNormalClosure is in the list
	}
}

func TestIsUnexpectedCloseError(t *testing.T) {
	err := &CloseError{Code: CloseGoingAway, Text: ""}
	if !IsUnexpectedCloseError(err, CloseNormalClosure) {
		t.Fatal("GoingAway is unexpected when only NormalClosure expected")
	}
	if IsUnexpectedCloseError(err, CloseGoingAway) {
		t.Fatal("GoingAway is expected")
	}
}

func TestFormatCloseMessage(t *testing.T) {
	msg := FormatCloseMessage(CloseNormalClosure, "bye")
	if len(msg) != 5 {
		t.Fatalf("len: got %d, want 5", len(msg))
	}
	if msg[0] != 0x03 || msg[1] != 0xE8 { // 1000 big-endian
		t.Fatalf("code bytes: got %x %x, want 03 e8", msg[0], msg[1])
	}
	if string(msg[2:]) != "bye" {
		t.Fatalf("text: got %q, want %q", string(msg[2:]), "bye")
	}
}

func TestFormatCloseMessage_NoStatus(t *testing.T) {
	msg := FormatCloseMessage(CloseNoStatusReceived, "")
	if len(msg) != 0 {
		t.Fatalf("expected empty for CloseNoStatusReceived, got len %d", len(msg))
	}
}

func TestConstants(t *testing.T) {
	// Verify constants match gorilla/websocket values.
	if TextMessage != 1 || BinaryMessage != 2 || CloseMessage != 8 || PingMessage != 9 || PongMessage != 10 {
		t.Fatal("message type constants mismatch")
	}
	if CloseNormalClosure != 1000 || CloseGoingAway != 1001 || CloseProtocolError != 1002 {
		t.Fatal("close code constants mismatch")
	}
}
