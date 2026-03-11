package loom

// PreparedMessage caches the encoded wire format of a message so it
// can be sent efficiently to multiple connections without re-encoding.
type PreparedMessage struct {
	messageType int
	data        []byte
}

// NewPreparedMessage creates a PreparedMessage from the given message
// type and payload. The prepared message can then be sent to many
// connections via Conn.WritePreparedMessage.
func NewPreparedMessage(messageType int, data []byte) (*PreparedMessage, error) {
	return &PreparedMessage{
		messageType: messageType,
		data:        data,
	}, nil
}

// WritePreparedMessage writes a PreparedMessage to the connection.
// This is equivalent to WriteMessage but avoids repeated marshalling
// when broadcasting the same message to many peers.
func (c *Conn) WritePreparedMessage(pm *PreparedMessage) error {
	return c.WriteMessage(pm.messageType, pm.data)
}
