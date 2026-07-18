# ADR-0025: Incus client library choice — thin internal REST client

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-11 ("Incus Provider") lists the choice of Go Incus client as an open question
with the default "use the official `github.com/lxc/incus/client` (Apache-2.0)."
This ADR records the decision actually taken before implementation began.

The Lahijan codebase has two non-negotiables that bear on this choice
(`/AGENTS.md`, "Non-negotiables"):

1. **No new dependencies without checking license + pattern fit. When in doubt,
   write an ADR.** The official Incus Go SDK is a *large* transitive tree
   (`github.com/lxc/incus` pulls in `gorilla/websocket`, `google.golang.org/grpc`,
   `gopkg.in/yaml.v3`, the entire `incus/shared/api` surface, plus Incus' own
   internal subpackages). Adopting it would more than double the dep graph of a
   project that has been deliberately kept lean.
2. **Tests stub the backends via httptest.** WS-11's own DoD requires the events
   listener and the `exec` websocket to round-trip via httptest fakes. The
   official SDK does its own connection bootstrap (TLS handshakes, `/1.0`
   capability probe, cluster detection) *before* the first user request, which
   makes httptest fakes painful — every test would need to script the bootstrap
   sequence. A plain HTTP client just speaks HTTP to the fake server.

ADR-0010 already commits to "wraps the Incus Go SDK" as one of its compliance
bullets. This ADR narrows that wording from "the SDK" to "a Go client over the
Incus REST API" — i.e. we still wrap a Go client, but it is ours.

Options considered:

- **Option A — Official SDK (`github.com/lxc/incus/client`).** Pros:
  idiomatic, tracks upstream changes automatically, used by `incus` CLI. Cons:
  massive transitive dep tree, painful to drive against an httptest fake
  (bootstrap handshakes), opinionated connection lifecycle that does not match
  Lahijan's layered "providers/ → pure driver" pattern.
- **Option B — Thin internal REST client over the Incus REST API.** Pros:
  trivially httptest-able, ~zero new deps, pattern-uniform with WS-12 (PowerDNS)
  and WS-13 (SeaweedFS) which also have no first-class Go SDK and will be thin
  HTTP clients, full control over retry / tracing / redaction at the boundary.
  Cons: we own the request/response types (mitigation:Incus' REST API is
  versioned and stable; types are small JSON structs).
- **Option C — Generate a client from the Incus OpenAPI spec.** Pros: typed
  end-to-end. Cons: Incus does not publish a machine-readable OpenAPI spec
  upstream; this option is not actually available.

## Decision

WS-11 implements the Incus driver as a thin internal REST client under
`internal/app/lahijan/providers/incus/`. The package:

- Talks HTTP to the Incus REST API (`GET/POST/PUT/PATCH/DELETE /1.0/...`) using
  a standard `net/http.Client` configured with a Unix-socket `DialContext` for
  production and a plain HTTP transport for tests.
- Owns its own JSON request/response types in `types.go`, kept narrow to what
  Lahijan actually uses (projects, instances, images, profiles, devices,
  networks, storage, events, exec).
- Adds **one** new runtime dependency: `github.com/gorilla/websocket` (BSD-2)
  for the events stream and `exec` websocket endpoints. `gorilla/websocket` is
  the canonical Go websocket library, BSD-2 licensed, and matches the existing
  pattern of choosing the most popular, narrowly-scoped library for each
  external protocol.
- Re-uses the in-process `eventbus` (`internal/app/lahijan/wasm/eventbus`) to
  fan Incus events into the WASM event bus, matching the pattern already in
  place from WS-10b.

The Provider interface (`Name / Ping / Capabilities`) introduced in
`internal/app/lahijan/providers/provider.go` is the shared surface that WS-12
and WS-13 will also implement.

This ADR narrows ADR-0010's compliance bullet from "wraps the Incus Go SDK" to
"wraps the Incus REST API from Go." ADR-0010's substantive content (full
Incus surface, tenant→project mapping, project-scoped isolation) is unchanged.

## Consequences

- **Positive:** zero transitive bloat — only `gorilla/websocket` is added.
- **Positive:** every WS-11 test is a pure httptest round-trip; no Incus daemon
  required for `make test`.
- **Positive:** WS-12 and WS-13 can copy the layout in `providers/<name>/`
  (client + types + per-area files + `fake/`) and ship their own drivers in
  days, not weeks.
- **Negative:** we own the JSON types. If Incus adds a field we want, we add it
  ourselves. Mitigation: the Incus REST API is documented and stable across
  minor versions; types in our driver are intentionally minimal.
- **Negative:** we re-implement the websocket `exec` protocol (operation secret
  → fds map → per-fd websocket). This is ~150 lines of Go and is reused by
  WS-14's xterm.js console and WS-24's noVNC.

## Compliance

- `internal/app/lahijan/providers/provider.go` defines the `Provider` interface
  shared by all three backends.
- `internal/app/lahijan/providers/incus/client.go` is the single HTTP entry
  point; every area file (`projects.go`, `instances.go`, ...) calls through it.
- The only new runtime import added by WS-11 is `github.com/gorilla/websocket`.
  `go.mod` should reflect that and nothing else from Incus.
- `internal/app/lahijan/providers/incus/fake/server.go` is the httptest fake
  every test in the package (and future WS-14 unit tests) reuses.
- All provider methods open an OpenTelemetry span via the package-local tracer
  in `tracing.go` (per ADR-0016 "Full OTel").

## References

- ADR-0010 (full Incus surface — narrowed by this ADR)
- ADR-0005 (single-host topology — Incus on Unix socket for MVP)
- ADR-0009 (multi-node-ready — the `PlacementDriver` interface)
- ADR-0016 (full OTel — every provider call traced)
- [Incus REST API docs](https://linuxcontainers.org/incus/docs/main/rest-api-spec/)
- WS-11 (Incus provider), WS-12 / WS-13 (PowerDNS, SeaweedFS)
