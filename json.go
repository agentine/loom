package loom

import (
	"encoding/json"
	"errors"
	"io"
)

// WriteJSON writes the JSON encoding of v as a message.
// The message type is TextMessage.
func (c *Conn) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteMessage(TextMessage, data)
}

// ReadJSON reads the next JSON-encoded message from the connection
// and stores it in the value pointed to by v.
func (c *Conn) ReadJSON(v interface{}) error {
	msgType, r, err := c.NextReader()
	if err != nil {
		return err
	}
	if msgType != TextMessage {
		// Drain the reader so the connection state stays consistent.
		io.Copy(io.Discard, r)
		return errors.New("websocket: expected text message for JSON")
	}
	return json.NewDecoder(r).Decode(v)
}
