# loom

A RFC 6455 WebSocket implementation for Go — drop-in replacement for [gorilla/websocket](https://github.com/gorilla/websocket).

## Install

```
go get github.com/agentine/loom
```

## Quick Start

### Server

```go
import websocket "github.com/agentine/loom"

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

func handler(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    for {
        msgType, msg, err := conn.ReadMessage()
        if err != nil {
            break
        }
        conn.WriteMessage(msgType, msg)
    }
}
```

### Client

```go
import websocket "github.com/agentine/loom"

conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/ws", nil)
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

conn.WriteMessage(websocket.TextMessage, []byte("hello"))
_, msg, _ := conn.ReadMessage()
fmt.Println(string(msg))
```

## Features

- **gorilla/websocket API compatibility** — change your import path and go
- **Server** (`Upgrader`) with origin checking, subprotocol negotiation, and custom response headers
- **Client** (`Dialer`) with TLS, proxy, subprotocol, cookie jar, and timeout support
- **JSON helpers** (`WriteJSON` / `ReadJSON`)
- **PreparedMessage** for efficient broadcasting to multiple connections
- **Compression API** (`EnableWriteCompression` / `SetCompressionLevel`)
- **Full RFC 6455 frame protocol** — masking, control frames, fragmentation, close handshake
- **Security hardening** — read limits, frame length overflow protection (CVE-2020-27813), reserved opcode rejection

## Migration from gorilla/websocket

```diff
-import "github.com/gorilla/websocket"
+import websocket "github.com/agentine/loom"
```

All public types, constants, functions, and methods match the gorilla/websocket API:

| Category | Symbols |
|----------|---------|
| Types | `Conn`, `Dialer`, `Upgrader`, `CloseError`, `PreparedMessage`, `BufferPool` |
| Constants | `TextMessage`, `BinaryMessage`, `CloseMessage`, `PingMessage`, `PongMessage` |
| Close codes | `CloseNormalClosure`, `CloseGoingAway`, `CloseProtocolError`, ... |
| Functions | `IsCloseError`, `IsUnexpectedCloseError`, `FormatCloseMessage`, `Subprotocols`, `IsWebSocketUpgrade` |
| Conn methods | `ReadMessage`, `WriteMessage`, `NextReader`, `NextWriter`, `WriteControl`, `Close`, `WriteJSON`, `ReadJSON`, `WritePreparedMessage`, `SetReadLimit`, `SetReadDeadline`, `SetWriteDeadline`, `SetPingHandler`, `SetPongHandler`, `SetCloseHandler`, `LocalAddr`, `RemoteAddr`, `Subprotocol`, `UnderlyingConn`, `NetConn`, `EnableWriteCompression`, `SetCompressionLevel` |

## Testing

```bash
go test -race -count=1 ./...
```

Run benchmarks:

```bash
go test -bench=. -benchmem ./...
```

Run fuzz tests:

```bash
go test -fuzz=FuzzReadFrameHeader -fuzztime=30s ./...
go test -fuzz=FuzzMaskBytes -fuzztime=30s ./...
```

## License

MIT
