# ADR-0026: PowerDNS client library choice — thin internal REST client

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-12 ("PowerDNS Provider") needs a Go client that talks to PowerDNS
Authoritative's HTTP API (`/api/v1/servers/localhost/zones`,
`/api/v1/servers/localhost/zones/<id>/cryptokeys`, etc.). PowerDNS does not
ship a first-class Go SDK — upstream's only client surface is the CLI (`pdns`)
plus the HTTP API itself.

The Lahijan codebase has two non-negotiables that bear on this choice
(`/AGENTS.md`, "Non-negotiables"):

1. **No new dependencies without checking license + pattern fit. When in
   doubt, write an ADR.** The popular third-party Go clients
   (`github.com/nzmprlr/powerdns-go-client`, `libgo/powerdns`) each pull in
   their own type trees and have spotty release cadences. None of them is
   canonical or recommended by PowerDNS upstream.
2. **Tests stub the backends via httptest.** WS-12's own DoD requires "every
   driver method round-trips against an httptest fake". A plain HTTP client
   just speaks HTTP to the fake server; a third-party SDK that does its own
   connection bootstrap would be painful to script against an httptest
   fake.

WS-11 (Incus) already made the same call in ADR-0025. Keeping the pattern
uniform means the WS-13 (SeaweedFS) driver will follow the same shape, so
the three Phase-3 providers look identical to a future maintainer.

Options considered:

- **Option A — Third-party SDK
  (`github.com/nzmprlr/powerdns-go-client`).** Pros: typed end-to-end,
  community-maintained. Cons: pulls a large type tree we will not use
  (TSIG, catalog zones, slab authority); release cadence lags PDNS
  upstream by months; painful to drive against an httptest fake; opinionated
  connection lifecycle that does not match Lahijan's "providers/ → pure
  driver" layering.
- **Option B — Thin internal REST client over the PowerDNS HTTP API.**
  Pros: trivially httptest-able, zero new deps, pattern-uniform with WS-11
  (Incus) and the upcoming WS-13 (SeaweedFS), full control over retry /
  tracing / redaction at the boundary. Cons: we own the request/response
  types (mitigation: PDNS' REST API is documented and stable across minor
  versions; types in our driver are intentionally minimal).
- **Option C — Generate a client from a PowerDNS OpenAPI spec.** Pros:
  typed end-to-end. Cons: PowerDNS does not publish a machine-readable
  OpenAPI spec upstream; this option is not actually available.

## Decision

WS-12 implements the PowerDNS driver as a thin internal REST client under
`internal/app/lahijan/providers/powerdns/`. The package:

- Talks HTTP to the PowerDNS REST API (`GET/POST/PATCH/PUT/DELETE
  /api/v1/servers/localhost/...`) using a standard `net/http.Client`
  configured for the daemon's HTTP origin. The API key is sent in the
  `X-API-Key` header on every request.
- Owns its own JSON request/response types in `types.go`, kept narrow to
  what Lahijan actually uses (zones, RRsets, cryptokeys, metadata).
- Adds **zero** new runtime dependencies: the package imports only stdlib
  (`net/http`, `encoding/json`) plus `go.opentelemetry.io/otel` for
  per-call tracing (already required by ADR-0016 and pulled in by WS-11).
- Synthesizes change events on every mutating call (`events.go`) and
  routes them into the same in-process `eventbus` (`internal/app/lahijan/
  wasm/eventbus`) the Incus driver uses. PDNS does not push events itself;
  this keeps the WASM bus uniform across providers.
- Re-uses the Provider interface (`Name / Ping / Capabilities`) introduced
  in `internal/app/lahijan/providers/provider.go` (WS-11). The three
  Phase-3 providers share the surface.

## Consequences

- **Positive:** zero transitive bloat — no new module appears in `go.mod`.
- **Positive:** every WS-12 test is a pure httptest round-trip; no PDNS
  daemon required for `make test`.
- **Positive:** the package layout mirrors WS-11's `providers/incus/` so a
  maintainer reading either driver sees the same shape (client + types +
  per-area files + `fake/`).
- **Negative:** we own the JSON types. If PDNS adds a field we want, we add
  it ourselves. Mitigation: PDNS' REST API is documented and stable across
  minor versions; types in our driver are intentionally minimal.
- **Negative:** we re-implement the DNSSEC enable/disable flow on top of
  the lower-level cryptokeys API. This is ~80 lines of Go and gives us
  fine-grained control over the KSK/ZSK/CSK choice Lahijan exposes at the
  service layer.

## Compliance

- `internal/app/lahijan/providers/powerdns/client.go` is the single HTTP
  entry point; every area file (`zones.go`, `records.go`, ...) calls
  through it.
- The only new runtime import added by WS-12 is the existing
  `go.opentelemetry.io/otel` (already required by ADR-0016). `go.mod`
  should not grow any new module for the PDNS driver.
- `internal/app/lahijan/providers/powerdns/fake/server.go` is the
  httptest fake every test in the package (and the upcoming WS-15 unit
  tests) reuses.
- All provider methods open an OpenTelemetry span via the package-local
  tracer in `tracing.go` (per ADR-0016 "Full OTel").
- The driver never carries `tenant_id`; the tenant → zone mapping lives in
  the `dns_zones` table (migration 0026) and is consulted by the DNS
  service (WS-15) before any driver call.

## References

- ADR-0007 (shared Postgres — the `pdns` logical DB the daemon's gpgsql
  backend reads from)
- ADR-0010 (full Incus surface — the sister decision; WS-12 is the DNS
  equivalent)
- ADR-0016 (full OTel — every provider call traced)
- ADR-0025 (Incus client library choice — the pattern this ADR mirrors)
- [PowerDNS HTTP API docs](https://doc.powerdns.com/authoritative/http-api/)
- WS-12 (PowerDNS provider), WS-13 / WS-15 (SeaweedFS provider, DNS module)
