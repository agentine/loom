# Loom — A Modern WebSocket Library for Go

**Package name:** `github.com/agentine/loom`
**Replaces:** `github.com/gorilla/websocket` (42,238 importers, 24.6K stars)
**Language:** Go
**License:** MIT

## Problem

gorilla/websocket is the de facto WebSocket library for Go with 42K+ importers, used by virtually every Go project that needs WebSocket support. The project is in a fragile state:

- Original author (73% of commits) left years ago
- Was archived in Dec 2022, unarchived with volunteer maintainers in 2023
- Last release was v1.5.3 in June 2024 (9+ months ago)
- 71 open issues, community PRs sit unmerged for months
- No funding, no corporate backing
- Known CVE history (CVE-2020-27813, DoS via integer overflow)
- Actively seeking a new maintainer

The only alternative, `coder/websocket`, has a completely different API and only 679 importers — less than 2% of gorilla's footprint. The ecosystem is stuck.

## Solution

Loom is a gorilla/websocket-compatible WebSocket library for Go that provides:

1. **Drop-in API compatibility** — same types, same methods, same behavior
2. **Modern Go idioms** — context.Context support, structured errors
3. **Security hardened** — frame size limits, origin validation, compression bomb protection
4. **Better performance** — zero-alloc hot paths, buffer pooling, optimized frame parsing
5. **Active maintenance** — responsive to issues and CVEs

## Architecture

### Package Structure

```
loom/
├── websocket.go          # Conn type, ReadMessage, WriteMessage, core API
├── server.go             # Upgrader — HTTP → WebSocket upgrade
├── client.go             # Dialer — WebSocket client connections
├── conn.go               # Connection internals, frame read/write
├── frame.go              # WebSocket frame parser (RFC 6455 §5)
├── mask.go               # Frame masking (with optimized implementations)
├── compression.go        # Per-message compression (RFC 7692)
├── json.go               # JSON helper (ReadJSON, WriteJSON)
├── proxy.go              # HTTP/HTTPS proxy support
├── prepared.go           # PreparedMessage for broadcast optimization
├── util.go               # Shared utilities (header parsing, subprotocol negotiation)
├── doc.go                # Package documentation
└── loom_test.go          # Tests (+ per-file _test.go)
```

### Core Types (gorilla-compatible API)

```go
// Connection — the central type
type Conn struct { ... }
func (c *Conn) ReadMessage() (messageType int, p []byte, err error)
func (c *Conn) WriteMessage(messageType int, data []byte) error
func (c *Conn) NextReader() (messageType int, r io.Reader, err error)
func (c *Conn) NextWriter(messageType int) (io.WriteCloser, error)
func (c *Conn) ReadJSON(v interface{}) error
func (c *Conn) WriteJSON(v interface{}) error
func (c *Conn) SetReadLimit(limit int64)
func (c *Conn) SetReadDeadline(t time.Time) error
func (c *Conn) SetWriteDeadline(t time.Time) error
func (c *Conn) SetPingHandler(h func(appData string) error)
func (c *Conn) SetPongHandler(h func(appData string) error)
func (c *Conn) SetCloseHandler(h func(code int, text string) error)
func (c *Conn) WriteControl(messageType int, data []byte, deadline time.Time) error
func (c *Conn) Close() error
func (c *Conn) LocalAddr() net.Addr
func (c *Conn) RemoteAddr() net.Addr
func (c *Conn) Subprotocol() string
func (c *Conn) UnderlyingConn() net.Conn
func (c *Conn) EnableWriteCompression(enable bool)
func (c *Conn) SetCompressionLevel(level int) error
func (c *Conn) NetConn() net.Conn

// Server — HTTP upgrade
type Upgrader struct {
    HandshakeTimeout  time.Duration
    ReadBufferSize    int
    WriteBufferSize   int
    WriteBufferPool   BufferPool
    Subprotocols      []string
    Error             func(w http.ResponseWriter, r *http.Request, status int, reason error)
    CheckOrigin       func(r *http.Request) bool
    EnableCompression bool
}
func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request, responseHeader http.Header) (*Conn, error)

// Client — outbound connections
type Dialer struct {
    NetDial           func(network, addr string) (net.Conn, error)
    NetDialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
    NetDialTLSContext func(ctx context.Context, network, addr string) (net.Conn, error)
    Proxy             func(*http.Request) (*url.URL, error)
    TLSClientConfig   *tls.Config
    HandshakeTimeout  time.Duration
    ReadBufferSize    int
    WriteBufferSize   int
    WriteBufferPool   BufferPool
    Subprotocols      []string
    EnableCompression bool
    Jar               http.CookieJar
}
func (d *Dialer) Dial(urlStr string, requestHeader http.Header) (*Conn, *http.Response, error)
func (d *Dialer) DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (*Conn, *http.Response, error)

// Broadcast optimization
type PreparedMessage struct { ... }
func NewPreparedMessage(messageType int, data []byte) (*PreparedMessage, error)
func (c *Conn) WritePreparedMessage(pm *PreparedMessage) error

// Constants (matching gorilla exactly)
const (
    TextMessage   = 1
    BinaryMessage = 2
    CloseMessage  = 8
    PingMessage   = 9
    PongMessage   = 10
)

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

// Error types
type CloseError struct { Code int; Text string }
func IsCloseError(err error, codes ...int) bool
func IsUnexpectedCloseError(err error, expectedCodes ...int) bool

// Helpers
var DefaultDialer = &Dialer{...}
func Subprotocols(r *http.Request) []string
func IsWebSocketUpgrade(r *http.Request) bool
func FormatCloseMessage(closeCode int, text string) []byte

// BufferPool interface
type BufferPool interface {
    Get() interface{}
    Put(interface{})
}
```

### Security Hardening (beyond gorilla)

1. **Frame size limits by default** — `ReadLimit` defaults to a safe value (not unlimited)
2. **Compression bomb protection** — limit decompressed message size independently of compressed size
3. **Origin checking** — `CheckOrigin` defaults to same-origin policy (gorilla defaults to allowing all origins in some paths)
4. **Integer overflow protection** — bounds checking on all frame length fields (CVE-2020-27813 fix baked in)
5. **Timeout enforcement** — handshake timeouts applied by default
6. **Close frame validation** — validate close codes per RFC 6455 §7.4

### Performance Targets

- **Zero-alloc frame parsing** on hot path (read/write of small messages)
- **Buffer pooling** via sync.Pool for read/write buffers
- **Optimized masking** using unsafe + word-sized XOR (with safe fallback)
- **PreparedMessage** caching for broadcast scenarios (encode once, send to N clients)

## Migration Path

For most users, migration is a single import path change:

```go
// Before
import "github.com/gorilla/websocket"

// After
import websocket "github.com/agentine/loom"
```

All type names, method signatures, constants, and error types match gorilla/websocket exactly. The `websocket` alias ensures zero code changes beyond the import.

## Implementation Phases

### Phase 1: Core Protocol & Connection (MVP)

Build the foundational WebSocket protocol implementation:

- RFC 6455 frame parser (read/write frames, masking, fragmentation)
- `Conn` type with `ReadMessage`, `WriteMessage`, `Close`
- `NextReader`, `NextWriter` streaming API
- Control messages: Close, Ping, Pong
- `SetReadLimit`, deadline support
- `CloseError`, `IsCloseError`, `IsUnexpectedCloseError`
- Close codes and `FormatCloseMessage`
- Basic test suite

### Phase 2: Server & Client

- `Upgrader` — full HTTP upgrade with handshake validation
- `Dialer` and `DefaultDialer` — client connections with TLS support
- `DialContext` with context cancellation
- `Proxy` support (HTTP and HTTPS CONNECT proxies)
- `Subprotocols` negotiation
- `IsWebSocketUpgrade` helper
- Cookie jar support
- `CheckOrigin` with secure defaults
- Server/client integration tests

### Phase 3: Advanced Features

- Per-message compression (RFC 7692, `permessage-deflate`)
- `EnableCompression`, `SetCompressionLevel`
- `PreparedMessage` for broadcast optimization
- `BufferPool` interface and integration
- JSON helpers (`ReadJSON`, `WriteJSON`)
- `WriteControl` with concurrent safety
- `NetConn()` accessor
- Performance benchmarks

### Phase 4: Hardening & Ship

- Autobahn Test Suite compliance (the standard WebSocket conformance test)
- Security hardening (compression bombs, frame size limits, close validation)
- Fuzz testing for frame parser
- Gorilla/websocket compatibility test suite (run gorilla's own tests against loom)
- README with migration guide
- CI/CD (GitHub Actions: test, lint, fuzz, benchmark)
- Benchmarks vs gorilla/websocket and coder/websocket
- Go module setup (`go.mod`, tagged release)
- Publish to pkg.go.dev

## Testing Strategy

1. **Unit tests** — per-module tests for frame parsing, masking, compression
2. **Integration tests** — full client↔server round-trips
3. **Autobahn Test Suite** — industry-standard WebSocket conformance (cases 1-13)
4. **Compatibility tests** — port gorilla/websocket's test suite to verify API compatibility
5. **Fuzz tests** — `go test -fuzz` for frame parser and handshake parser
6. **Benchmarks** — `testing.B` benchmarks for frame read/write, masking, JSON, broadcast

## Non-Goals

- New API design (we match gorilla's API; novel API is coder/websocket's job)
- HTTP/2 WebSocket (RFC 8441) — future work, not in initial release
- WebTransport — different protocol entirely
- WASM support — future work
