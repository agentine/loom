# loom

A RFC 6455 WebSocket library for Go — drop-in replacement for [gorilla/websocket](https://github.com/gorilla/websocket).

```
go get github.com/agentine/loom
```

---

## Why loom?

gorilla/websocket is the de facto WebSocket library for Go with 42,000+ importers. It is also in a fragile state: the original author left years ago, the project was archived in December 2022 and unarchived by volunteers in 2023, its last release was June 2024, and it carries a CVE history including [CVE-2020-27813](https://github.com/advisories/GHSA-jf24-p9p9-4rjh) (DoS via integer overflow in frame length parsing).

Loom exists to give the Go ecosystem a maintained, security-hardened replacement that requires zero migration effort for existing gorilla/websocket users.

**What loom improves over gorilla/websocket:**

| Area | gorilla/websocket | loom |
|------|------------------|------|
| Read limit default | Unlimited | 32 MB (configurable) |
| Origin checking | Allows all origins by default in some paths | Defaults to same-origin policy |
| Frame length overflow | CVE-2020-27813 (fixed post-2020) | Integer overflow check baked in from the start |
| RSV bit validation | Validated | Validated, returns `CloseProtocolError` |
| Close code validation | Partial | Full RFC 6455 §7.4 validation |
| Compression bomb protection | None | `ReadLimit` enforced on decompressed size |
| Response header injection | None | CR/LF rejection in `Upgrader.Upgrade` |
| Maintenance | Volunteer-run, slow PRs | Active |

---

## Quick start

### Server

```go
import (
    "net/http"
    websocket "github.com/agentine/loom"
)

var upgrader = websocket.Upgrader{
    // Allow all origins for development. See "Security notes" for production guidance.
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
        if err := conn.WriteMessage(msgType, msg); err != nil {
            break
        }
    }
}

func main() {
    http.HandleFunc("/ws", handler)
    http.ListenAndServe(":8080", nil)
}
```

### Client

```go
import (
    "fmt"
    "log"
    websocket "github.com/agentine/loom"
)

conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/ws", nil)
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

conn.WriteMessage(websocket.TextMessage, []byte("hello"))
_, msg, _ := conn.ReadMessage()
fmt.Println(string(msg))
```

### Client with context

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

conn, resp, err := websocket.DefaultDialer.DialContext(ctx, "wss://example.com/ws", nil)
```

---

## API reference

### Constants

```go
// Message types
const (
    TextMessage   = 1
    BinaryMessage = 2
    CloseMessage  = 8
    PingMessage   = 9
    PongMessage   = 10
)
```

```go
// Close codes (RFC 6455 §7.4.1)
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
```

---

### Upgrader

`Upgrader` upgrades an HTTP connection to WebSocket on the server side.

```go
type Upgrader struct {
    // HandshakeTimeout is the maximum duration of the upgrade handshake.
    // Zero means no timeout.
    HandshakeTimeout time.Duration

    // ReadBufferSize and WriteBufferSize are I/O buffer sizes in bytes.
    // Zero means 4096.
    ReadBufferSize  int
    WriteBufferSize int

    // WriteBufferPool is a pool of write buffers. When set, a buffer is
    // borrowed from the pool per write and returned after the flush.
    WriteBufferPool BufferPool

    // Subprotocols lists server-supported subprotocols in preference order.
    // The first match with the client's requested protocols is selected.
    Subprotocols []string

    // Error generates the HTTP error response when the upgrade fails.
    // Defaults to http.Error.
    Error func(w http.ResponseWriter, r *http.Request, status int, reason error)

    // CheckOrigin returns true if the request Origin is acceptable.
    // Defaults to a same-origin check: the Origin host must match the
    // Host header. Set to func(r *http.Request) bool { return true }
    // to allow all origins (only appropriate in development or when you
    // enforce origin policy at a different layer).
    CheckOrigin func(r *http.Request) bool

    // EnableCompression negotiates permessage-deflate (RFC 7692) with
    // clients that advertise support.
    EnableCompression bool
}

func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*Conn, error)
```

`Upgrade` validates the HTTP request (method, headers, WebSocket version, origin), hijacks the connection, sends the 101 response, and returns a `*Conn`. Any headers in `responseHeader` are included in the 101 response; CR/LF characters in keys or values are rejected to prevent HTTP response splitting.

---

### Dialer

`Dialer` creates outbound WebSocket connections.

```go
type Dialer struct {
    // NetDial dials a TCP connection. Ignored when NetDialContext is set.
    NetDial func(network, addr string) (net.Conn, error)

    // NetDialContext dials a TCP connection with context support.
    NetDialContext func(ctx context.Context, network, addr string) (net.Conn, error)

    // NetDialTLSContext dials a TLS connection. When set, TLSClientConfig
    // is ignored for the TLS handshake.
    NetDialTLSContext func(ctx context.Context, network, addr string) (net.Conn, error)

    // Proxy returns the proxy URL for a given request.
    // Defaults to http.ProxyFromEnvironment in DefaultDialer.
    Proxy func(*http.Request) (*url.URL, error)

    // TLSClientConfig is the TLS configuration for wss:// connections.
    TLSClientConfig *tls.Config

    // HandshakeTimeout is the maximum duration of the opening handshake.
    HandshakeTimeout time.Duration

    // ReadBufferSize and WriteBufferSize are I/O buffer sizes in bytes.
    ReadBufferSize  int
    WriteBufferSize int

    // WriteBufferPool is a pool of write buffers.
    WriteBufferPool BufferPool

    // Subprotocols lists subprotocols to advertise in the handshake.
    Subprotocols []string

    // EnableCompression requests permessage-deflate from the server.
    EnableCompression bool

    // Jar stores and sends cookies on the handshake request.
    Jar http.CookieJar
}

// DefaultDialer uses http.ProxyFromEnvironment and a 45-second handshake timeout.
var DefaultDialer = &Dialer{
    Proxy:            http.ProxyFromEnvironment,
    HandshakeTimeout: 45 * time.Second,
}

func (d *Dialer) Dial(urlStr string, requestHeader http.Header) (*Conn, *http.Response, error)
func (d *Dialer) DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (*Conn, *http.Response, error)
```

Both `ws://` and `wss://` URL schemes are accepted, as well as `http://` and `https://`. The returned `*http.Response` contains the server's 101 response, useful for reading cookies or custom headers.

`ErrBadHandshake` is returned when the server's response does not conform to RFC 6455 (non-101 status, missing headers, or wrong `Sec-WebSocket-Accept`).

---

### Conn

`Conn` is the central type representing an established WebSocket connection. It is safe to call read methods and write methods concurrently, but it is not safe to make concurrent calls to the same class of operation (e.g., two simultaneous `ReadMessage` calls).

#### Reading

```go
// ReadMessage reads a complete message. Returns the message type and payload.
// Compressed messages (when permessage-deflate is negotiated) are decompressed
// transparently. ReadLimit is enforced on the decompressed size.
func (c *Conn) ReadMessage() (messageType int, p []byte, err error)

// NextReader returns a streaming reader for the next message. The caller must
// read the reader to EOF before calling NextReader again.
func (c *Conn) NextReader() (messageType int, r io.Reader, err error)
```

#### Writing

```go
// WriteMessage writes data as a single WebSocket frame.
func (c *Conn) WriteMessage(messageType int, data []byte) error

// NextWriter returns a streaming writer for a message. The writer must be
// closed when done; Close sends the final frame.
func (c *Conn) NextWriter(messageType int) (io.WriteCloser, error)
```

#### Control messages

```go
// WriteControl sends a control frame (CloseMessage, PingMessage, PongMessage)
// with an optional write deadline. Pass a zero time.Time for no deadline.
func (c *Conn) WriteControl(messageType int, data []byte, deadline time.Time) error

// Close sends a CloseNormalClosure close frame and closes the underlying
// connection. The close frame is best-effort and will not block if the
// peer is unresponsive.
func (c *Conn) Close() error
```

#### Handlers for incoming control frames

```go
// SetPingHandler sets the handler called when a ping is received.
// The default handler sends a pong with the same application data.
func (c *Conn) SetPingHandler(h func(appData string) error)

// SetPongHandler sets the handler called when a pong is received.
// The default handler does nothing.
func (c *Conn) SetPongHandler(h func(appData string) error)

// SetCloseHandler sets the handler called when a close frame is received.
// The default handler sends a corresponding close frame back and returns
// a *CloseError, which terminates the read loop.
func (c *Conn) SetCloseHandler(h func(code int, text string) error)
```

#### Limits and deadlines

```go
// SetReadLimit sets the maximum message size in bytes. If a message exceeds
// the limit, the connection returns ErrReadLimit. The default is 32 MB.
// For compressed messages the limit applies to the decompressed size.
// Pass -1 to disable the limit (not recommended on untrusted connections).
func (c *Conn) SetReadLimit(limit int64)

// SetReadDeadline sets the deadline for all future reads.
// A zero value clears the deadline.
func (c *Conn) SetReadDeadline(t time.Time) error

// SetWriteDeadline sets the deadline for all future writes.
// A zero value clears the deadline.
func (c *Conn) SetWriteDeadline(t time.Time) error
```

#### Connection metadata

```go
func (c *Conn) LocalAddr() net.Addr
func (c *Conn) RemoteAddr() net.Addr
func (c *Conn) Subprotocol() string        // negotiated subprotocol, or ""
func (c *Conn) UnderlyingConn() net.Conn   // underlying net.Conn
func (c *Conn) NetConn() net.Conn          // alias for UnderlyingConn
```

---

### JSON helpers

```go
// WriteJSON marshals v to JSON and sends it as a TextMessage.
func (c *Conn) WriteJSON(v interface{}) error

// ReadJSON reads the next TextMessage and unmarshals it into v.
// Returns an error if the next message is not a TextMessage.
func (c *Conn) ReadJSON(v interface{}) error
```

Example:

```go
type ChatMessage struct {
    User string `json:"user"`
    Body string `json:"body"`
}

// Send
conn.WriteJSON(ChatMessage{User: "alice", Body: "hi"})

// Receive
var msg ChatMessage
if err := conn.ReadJSON(&msg); err != nil {
    // handle error
}
```

---

### PreparedMessage

`PreparedMessage` encodes a message once and lets you send it to many connections without re-encoding. This is the recommended pattern for broadcasting.

```go
// NewPreparedMessage creates a message that is pre-encoded for both
// uncompressed and compressed (permessage-deflate) connections.
func NewPreparedMessage(messageType int, data []byte) (*PreparedMessage, error)

// WritePreparedMessage sends the prepared message to the connection.
// For server connections, pre-encoded frame bytes are written directly.
// For client connections, a fresh masking key is applied per RFC 6455 §5.1.
func (c *Conn) WritePreparedMessage(pm *PreparedMessage) error
```

Example broadcast:

```go
pm, err := websocket.NewPreparedMessage(websocket.TextMessage, payload)
if err != nil {
    return err
}

for _, conn := range subscribers {
    conn.WritePreparedMessage(pm)
}
```

---

### Compression

Per-message compression uses the `permessage-deflate` extension (RFC 7692). It must be enabled on both sides of the connection during the handshake.

```go
// On the server
upgrader := websocket.Upgrader{
    EnableCompression: true,
}

// On the client
dialer := websocket.Dialer{
    EnableCompression: true,
}
```

After the handshake, you can toggle compression per-connection and control the compression level:

```go
// Enable or disable write compression for this connection.
// Has no effect if permessage-deflate was not negotiated.
func (c *Conn) EnableWriteCompression(enable bool)

// Set the flate compression level. Valid values:
//   flate.BestSpeed (1) through flate.BestCompression (9)
//   flate.DefaultCompression (-1)
//   flate.HuffmanOnly (-2)
func (c *Conn) SetCompressionLevel(level int) error
```

Compression is applied to `TextMessage` and `BinaryMessage` frames only; control frames are never compressed. The `ReadLimit` is enforced on the decompressed message size, providing protection against compression bomb attacks.

---

### BufferPool

`BufferPool` is an interface for reusing write buffers across connections. Use it to reduce allocations in high-throughput servers.

```go
type BufferPool interface {
    Get() interface{}
    Put(interface{})
}
```

A `sync.Pool`-backed implementation:

```go
var writePool = &sync.Pool{
    New: func() interface{} {
        return make([]byte, 4096)
    },
}

upgrader := websocket.Upgrader{
    WriteBufferPool: writePool,
}
```

---

### Error types and helpers

```go
// CloseError is returned by read operations when a close frame is received.
type CloseError struct {
    Code int    // RFC 6455 §7.4.1 close code
    Text string // optional close reason
}
func (e *CloseError) Error() string

// IsCloseError returns true if err is a *CloseError with one of the given codes.
func IsCloseError(err error, codes ...int) bool

// IsUnexpectedCloseError returns true if err is a *CloseError whose code
// is not in the expected codes. Useful to suppress log noise.
func IsUnexpectedCloseError(err error, expectedCodes ...int) bool

// FormatCloseMessage encodes a close code and optional text into a close
// frame payload suitable for WriteControl or WriteMessage(CloseMessage, ...).
func FormatCloseMessage(closeCode int, text string) []byte

// Sentinel errors
var ErrCloseSent  = errors.New("websocket: close sent")   // write after close
var ErrReadLimit  = errors.New("websocket: read limit exceeded")
var ErrBadHandshake = errors.New("websocket: bad handshake") // client only
```

Typical read loop with proper error handling:

```go
for {
    _, msg, err := conn.ReadMessage()
    if err != nil {
        if websocket.IsUnexpectedCloseError(err,
            websocket.CloseGoingAway,
            websocket.CloseNormalClosure,
        ) {
            log.Printf("unexpected close: %v", err)
        }
        return
    }
    // process msg
}
```

---

### Utility functions

```go
// Subprotocols returns the subprotocols requested by the client in the
// Sec-WebSocket-Protocol header.
func Subprotocols(r *http.Request) []string

// IsWebSocketUpgrade returns true if the request is a WebSocket upgrade.
func IsWebSocketUpgrade(r *http.Request) bool
```

---

## Configuration reference

### Upgrader fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `HandshakeTimeout` | `time.Duration` | 0 (none) | Max duration of the HTTP upgrade handshake |
| `ReadBufferSize` | `int` | 4096 | Read I/O buffer size in bytes |
| `WriteBufferSize` | `int` | 4096 | Write I/O buffer size in bytes |
| `WriteBufferPool` | `BufferPool` | nil | Pool for write buffers |
| `Subprotocols` | `[]string` | nil | Server-supported subprotocols in preference order |
| `Error` | `func(...)` | `http.Error` | HTTP error response generator |
| `CheckOrigin` | `func(*http.Request) bool` | same-origin | Origin validator |
| `EnableCompression` | `bool` | false | Negotiate permessage-deflate |

### Dialer fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `NetDial` | `func(network, addr string) (net.Conn, error)` | nil | Custom TCP dialer |
| `NetDialContext` | `func(ctx, network, addr) (net.Conn, error)` | nil | Custom context-aware TCP dialer |
| `NetDialTLSContext` | `func(ctx, network, addr) (net.Conn, error)` | nil | Custom TLS dialer |
| `Proxy` | `func(*http.Request) (*url.URL, error)` | `http.ProxyFromEnvironment` in `DefaultDialer` | Proxy selector |
| `TLSClientConfig` | `*tls.Config` | nil | TLS configuration for wss:// |
| `HandshakeTimeout` | `time.Duration` | 0 (none) | Max duration of the opening handshake |
| `ReadBufferSize` | `int` | 4096 | Read I/O buffer size in bytes |
| `WriteBufferSize` | `int` | 4096 | Write I/O buffer size in bytes |
| `WriteBufferPool` | `BufferPool` | nil | Pool for write buffers |
| `Subprotocols` | `[]string` | nil | Subprotocols to advertise to the server |
| `EnableCompression` | `bool` | false | Request permessage-deflate from server |
| `Jar` | `http.CookieJar` | nil | Cookie jar for handshake request/response |

### Conn.SetReadLimit

The default read limit is **32 MB** per message. This protects servers from clients sending oversized messages to exhaust memory. For binary file upload use cases, raise the limit explicitly:

```go
conn.SetReadLimit(512 << 20) // 512 MB
```

For compressed messages, the limit applies to the **decompressed** size, so a 1 MB compressed message that expands to 33 MB is rejected.

---

## Migration from gorilla/websocket

For the vast majority of codebases, migration is a single line change:

```diff
-import "github.com/gorilla/websocket"
+import websocket "github.com/agentine/loom"
```

All public types, constants, functions, and method signatures match gorilla/websocket exactly. The `websocket` alias ensures no other code changes are needed.

### Full symbol compatibility table

| Category | Symbols |
|----------|---------|
| Types | `Conn`, `Dialer`, `Upgrader`, `CloseError`, `PreparedMessage`, `BufferPool` |
| Message type constants | `TextMessage`, `BinaryMessage`, `CloseMessage`, `PingMessage`, `PongMessage` |
| Close code constants | `CloseNormalClosure`, `CloseGoingAway`, `CloseProtocolError`, `CloseUnsupportedData`, `CloseNoStatusReceived`, `CloseAbnormalClosure`, `CloseInvalidFramePayloadData`, `ClosePolicyViolation`, `CloseMessageTooBig`, `CloseMandatoryExtension`, `CloseInternalServerErr`, `CloseServiceRestart`, `CloseTryAgainLater`, `CloseTLSHandshake` |
| Sentinel errors | `ErrCloseSent`, `ErrReadLimit`, `ErrBadHandshake` |
| Package-level functions | `IsCloseError`, `IsUnexpectedCloseError`, `FormatCloseMessage`, `Subprotocols`, `IsWebSocketUpgrade` |
| Package-level vars | `DefaultDialer` |
| `Conn` methods | `ReadMessage`, `WriteMessage`, `NextReader`, `NextWriter`, `WriteControl`, `Close`, `WriteJSON`, `ReadJSON`, `WritePreparedMessage`, `SetReadLimit`, `SetReadDeadline`, `SetWriteDeadline`, `SetPingHandler`, `SetPongHandler`, `SetCloseHandler`, `LocalAddr`, `RemoteAddr`, `Subprotocol`, `UnderlyingConn`, `NetConn`, `EnableWriteCompression`, `SetCompressionLevel` |

### Behavioural differences to be aware of

**Read limit is now enforced by default.** gorilla/websocket has no default read limit. Loom defaults to 32 MB. If you rely on reading messages larger than 32 MB, call `conn.SetReadLimit` with a larger value after upgrading/dialing.

**Origin check defaults to same-origin.** gorilla/websocket's `Upgrader` allows all origins when `CheckOrigin` is nil in some code paths. Loom always applies a same-origin check when `CheckOrigin` is nil. If your application legitimately serves cross-origin WebSocket connections, set `CheckOrigin` explicitly.

**`ReadJSON` requires a TextMessage.** If the peer sends a `BinaryMessage` containing JSON, `ReadJSON` will drain the message and return an error. This matches the intent of the JSON helper (JSON is a text format) and is compatible with gorilla/websocket's behaviour.

### Step-by-step migration

1. Replace the import path (single `sed` across your codebase):

   ```
   find . -name '*.go' | xargs sed -i 's|github.com/gorilla/websocket|github.com/agentine/loom|g'
   ```

2. Add the import alias in files that use the package without an alias. Or add the alias to all files consistently:

   ```go
   import websocket "github.com/agentine/loom"
   ```

3. Remove the gorilla/websocket dependency from `go.mod`:

   ```
   go mod tidy
   ```

4. Run your tests:

   ```
   go test -race ./...
   ```

5. Review your `Upgrader.CheckOrigin` usage. If it was previously nil and you were relying on permissive origin behaviour, add an explicit check.

6. If you read messages larger than 32 MB, add `conn.SetReadLimit(yourLimit)` after the upgrade or dial.

---

## Security notes

### ReadLimit

Always set an appropriate `ReadLimit` for your workload. The default of 32 MB is generous for text-based protocols; for a chat application a limit of 64 KB or 1 MB is more appropriate. Never disable the limit (`-1`) on connections from untrusted clients.

### Origin validation

Cross-origin WebSocket connections bypass the same-origin policy of browsers. The default `CheckOrigin` implementation compares the `Origin` header host against the `Host` header host (case-insensitively, stripping ports). For production APIs, supply a `CheckOrigin` function that validates against your known set of allowed origins.

### RSV bit validation

Loom validates RFC 6455 §5.2: the RSV1, RSV2, and RSV3 bits of each frame header must be zero unless a corresponding extension has been negotiated. RSV1 is permitted only when `permessage-deflate` was successfully negotiated during the handshake. Receiving a frame with unexpected RSV bits set closes the connection with `CloseProtocolError`.

### Close code validation

Close codes are validated against RFC 6455 §7.4.1. Codes 1004, 1005 (no status), 1006 (abnormal closure), and 1015 (TLS handshake failure) must never appear in an actual close frame; receiving them causes a `CloseProtocolError`. Codes outside the defined ranges (below 1000, 1016–2999, 5000+) are also rejected.

### Compression bomb protection

When `permessage-deflate` is negotiated, `ReadLimit` is enforced on the **decompressed** message size. A small compressed message that would expand to more than `ReadLimit` bytes is rejected with `ErrReadLimit`. This prevents a class of denial-of-service attacks where an attacker sends a highly-compressible payload designed to exhaust server memory on decompression.

### Integer overflow protection (CVE-2020-27813)

The WebSocket frame format supports a 64-bit payload length field. Loom checks that this field does not exceed `math.MaxInt64` and rejects frames that declare an impossibly large length, preventing integer overflow on 32-bit platforms and protecting against DoS via memory exhaustion.

### IPv6 support

The same-origin `CheckOrigin` implementation uses `net.SplitHostPort` to strip port numbers before comparing hosts, which correctly handles IPv6 addresses in bracket notation (e.g., `[::1]:8080`).

### Response header injection

`Upgrader.Upgrade` rejects any custom response header that contains CR (`\r`) or LF (`\n`) characters in its key or value. This prevents HTTP response splitting attacks where a crafted header value could inject additional HTTP headers or responses.

### TLS

For `wss://` connections, the `Dialer` uses `tls.Client` with `HandshakeContext`, which respects context cancellation. The `ServerName` field is populated from the URL hostname when not explicitly set in `TLSClientConfig`. To pin certificates or enforce a minimum TLS version, supply a `*tls.Config`:

```go
dialer := websocket.Dialer{
    TLSClientConfig: &tls.Config{
        MinVersion: tls.VersionTLS12,
    },
}
```

---

## Testing

```bash
# Run all tests with race detector
go test -race -count=1 ./...

# Run benchmarks
go test -bench=. -benchmem ./...

# Fuzz the frame parser (run for at least 30 seconds)
go test -fuzz=FuzzReadFrameHeader -fuzztime=30s ./...

# Fuzz the masking implementation
go test -fuzz=FuzzMaskBytes -fuzztime=30s ./...
```

---

## License

MIT — Copyright (c) 2026 Agentine. See [LICENSE](LICENSE).
