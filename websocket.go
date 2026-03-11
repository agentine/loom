package loom

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Message types (matching gorilla/websocket exactly).
const (
	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10
)

// Close codes defined in RFC 6455 §7.4.1.
const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005
	CloseAbnormalClosure         = 1006
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
	CloseServiceRestart          = 1012
	CloseTryAgainLater           = 1013
	CloseTLSHandshake            = 1015
)

// defaultReadLimit is the default max message size (32 MB).
const defaultReadLimit = 32 << 20

// CloseError represents a WebSocket close control message.
type CloseError struct {
	Code int
	Text string
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("websocket: close %d %s", e.Code, e.Text)
}

// IsCloseError returns true if err is a *CloseError with one of the given codes.
func IsCloseError(err error, codes ...int) bool {
	var ce *CloseError
	if !errors.As(err, &ce) {
		return false
	}
	for _, code := range codes {
		if ce.Code == code {
			return true
		}
	}
	return false
}

// IsUnexpectedCloseError returns true if err is a *CloseError whose code
// is NOT in the given expected codes. Useful for logging only unexpected closes.
func IsUnexpectedCloseError(err error, expectedCodes ...int) bool {
	var ce *CloseError
	if !errors.As(err, &ce) {
		return false
	}
	for _, code := range expectedCodes {
		if ce.Code == code {
			return false
		}
	}
	return true
}

// FormatCloseMessage creates a close control frame payload from a close code
// and optional text.
func FormatCloseMessage(closeCode int, text string) []byte {
	if closeCode == CloseNoStatusReceived {
		return []byte{}
	}
	buf := make([]byte, 2+len(text))
	binary.BigEndian.PutUint16(buf, uint16(closeCode))
	copy(buf[2:], text)
	return buf
}

// ErrCloseSent is returned after a close message has been sent.
var ErrCloseSent = errors.New("websocket: close sent")

// ErrReadLimit is returned when a message exceeds the read limit.
var ErrReadLimit = errors.New("websocket: read limit exceeded")
