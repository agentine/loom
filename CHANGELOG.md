# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-03-12

Initial release of **loom** — a zero-dependency, RFC 6455-compliant WebSocket library for Go that replaces [gorilla/websocket](https://github.com/gorilla/websocket) with a safer, more secure default configuration.

### Added

- **Frame parser/writer** (`frame.go`, `mask.go`) — full RFC 6455 frame codec: variable-length payload, masking/unmasking (per-message, SIMD-friendly loop), fin/opcode/RSV bits, control frame handling.
- **Connection** (`conn.go`, `websocket.go`) — `Conn` type with gorilla-compatible API: `ReadMessage`, `WriteMessage`, `ReadJSON`, `WriteJSON`, `SetReadDeadline`, `SetWriteDeadline`, `SetPingHandler`, `SetPongHandler`, `SetCloseHandler`, `Close`, `CloseHandler`.
- **Server** (`server.go`) — `Upgrader` with same-origin check, `CheckOrigin` hook, configurable `ReadBufferSize`/`WriteBufferSize`, `HandshakeTimeout`. Origin policy defaults to **reject cross-origin** (gorilla defaults to accept).
- **Client** (`client.go`) — `Dialer` with `TLSClientConfig`, `Proxy` support, `HandshakeTimeout`, `NetDial` hook.
- **Proxy support** (`proxy.go`) — HTTP CONNECT tunnel through `http.ProxyFromEnvironment` / custom proxy function via `Dialer.Proxy`.
- **JSON helpers** (`json.go`) — `ReadJSON` / `WriteJSON` using `encoding/json`.
- **PreparedMessage** (`prepared.go`) — `NewPreparedMessage` pre-encodes wire frames (both compressed and uncompressed variants) at construction time for zero-copy broadcast to multiple connections.
- **permessage-deflate compression** (`compression.go`) — RFC 7692 `permessage-deflate` extension with pooled `flate.Writer` instances, sync-marker stripping (§7.2.1), and compression bomb protection (decompressed byte limit).
- **Utility** (`util.go`) — `hostPort`, token-list parsing.
- **Benchmarks** (`bench_test.go`) — echo throughput, ping-pong latency, broadcast overhead.
- **Fuzz tests** (`fuzz_test.go`) — `FuzzReadFrameHeader`, `FuzzMaskBytes`.
- **Gorilla-compatible API** — `ReadLimit`, `EnableCompression`, `IsCloseError`, `IsUnexpectedCloseError`, `FormatCloseMessage`, `CloseMessage`, error sentinel values.

### Fixed

- **Proxy silently ignored** — `Dialer.dial()` now uses `Proxy` field via HTTP CONNECT; previously the field was set but never consulted.
- **Header injection** (#108) — `Upgrader.Upgrade` now rejects `\r` and `\n` in `responseHeader` values, preventing header injection.
- **ReadLimit per-frame instead of per-message** (#97) — `ReadLimit` is now enforced across the full message (accumulating multi-frame payloads), not reset per frame.
- **Close() deadlock on net.Pipe** — `Close()` no longer hangs when the peer is not reading; underlying connection is closed immediately.
- **RSV bits and close code validation** (#98) — non-zero RSV bits (when no extension negotiated) and invalid close codes now result in a protocol error close frame per RFC 6455.
- **IPv6 addresses in hostPort()** (#115) — bracket notation for IPv6 host literals handled correctly.
- **PreparedMessage re-encoding on each write** (#173) — `NewPreparedMessage` pre-encodes both compressed and uncompressed frames at construction; `WritePreparedMessage` writes pre-encoded bytes directly (zero-copy broadcast).
- **Compression bomb protection** (#141) — decompressed payload capped at `ReadLimit`; previously an attacker could send a tiny compressed message that expanded to arbitrary size.
- **permessage-deflate unimplemented** (#172) — compression negotiation, `compressData`, and `decompressData` are now fully wired; was previously a stub.
- **Dead websocket close frame** (#144) — close frames sent after connection already closed are silently dropped rather than surfacing write errors.
- **Gorilla API gaps** (#145) — `IsCloseError`, `IsUnexpectedCloseError`, `FormatCloseMessage`, all standard close codes present and gorilla-compatible.
