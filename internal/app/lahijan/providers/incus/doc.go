// Package incus is the Lahijan driver that translates Lahijan's compute domain
// into Incus REST API calls. It implements the providers.Provider interface
// and exposes every Incus API surface (instances, images, profiles, devices,
// networks, projects, storage, events, exec) as typed Go methods.
//
// Per ADR-0025 this driver is a thin internal REST client over the Incus
// REST API; it does NOT depend on the upstream `github.com/lxc/incus/client`
// SDK. This keeps the dependency tree lean and makes httptest fakes trivial.
//
// # Layout
//
//   - client.go — single HTTP entry point. Every area file calls through it.
//   - types.go  — request/response types mirroring the Incus REST shapes.
//   - tracing.go — package-local OpenTelemetry tracer (per ADR-0016).
//   - errors.go — sentinel errors + REST error envelope decoder.
//   - projects.go — tenant → Incus project mapping + restricted defaults.
//   - instances.go, images.go, profiles.go, devices.go, networks.go,
//     storage.go — area-specific CRUD + lifecycle.
//   - events.go — websocket event stream → WASM event bus.
//   - exec.go  — websocket exec proxy (reused by WS-14 xterm.js console).
//   - provider.go — providers.Provider impl (Name / Ping / Capabilities).
//   - fake/server.go — httptest Incus daemon for tests (and WS-14 unit tests).
//
// # Concurrency
//
// All methods on *Provider are safe for concurrent use. The underlying
// http.Client is goroutine-safe; per-call state lives only on the call stack.
// The events listener runs in its own goroutine and is the only writer to the
// event bus.
//
// # Tracing
//
// Every public method opens a span via the package tracer. Span names follow
// the convention "incus.<area>.<verb>" (e.g. "incus.instance.create"). The
// Incus operation id (when present) is recorded as the "incus.operation_id"
// attribute so a slow Incus-side operation can be cross-referenced with the
// Incus daemon's own logs.
package incus
