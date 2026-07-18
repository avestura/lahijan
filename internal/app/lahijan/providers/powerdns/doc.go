// Package powerdns is the Lahijan driver that translates Lahijan's DNS domain
// into PowerDNS Authoritative HTTP API calls. It implements the
// providers.Provider interface and exposes every PowerDNS surface Lahijan's
// DNS module needs (zones, RRsets, cryptokeys, metadata).
//
// Per ADR-0026 this driver is a thin internal REST client over the PowerDNS
// HTTP API; PowerDNS does not ship a first-class Go SDK, and a thin client
// matches the WS-11 (Incus) pattern, keeps the dependency tree lean, and
// makes httptest fakes trivial.
//
// # Layout
//
//   - client.go — single HTTP entry point. Every area file calls through it.
//   - types.go  — request/response types mirroring the PowerDNS REST shapes.
//   - tracing.go — package-local OpenTelemetry tracer (per ADR-0016).
//   - errors.go — sentinel errors + REST error envelope decoder.
//   - zones.go — zone CRUD + AXFR toggle.
//   - records.go — RRset CRUD with canonical-name validation + reverse-zone helpers.
//   - cryptokeys.go — DNSSEC key management (per zone; enable/disable).
//   - metadata.go — zone metadata (SOA-EDIT, etc.).
//   - events.go — PDNS does not emit push events; we synthesize them on every
//     change so the WASM event bus sees a uniform stream.
//   - provider.go — providers.Provider impl (Name / Ping / Capabilities).
//   - fake/server.go — httptest PDNS server for tests (and WS-15 unit tests).
//
// # Concurrency
//
// All methods on *Provider are safe for concurrent use. The underlying
// http.Client is goroutine-safe; per-call state lives only on the call stack.
//
// # Tracing
//
// Every public method opens a span via the package tracer. Span names follow
// the convention "powerdns.<area>.<verb>" (e.g. "powerdns.zone.create"). The
// canonical zone id (when present) is recorded as the "powerdns.zone"
// attribute so a slow PDNS-side operation can be cross-referenced with the
// PDNS daemon's own logs.
//
// # Tenant mapping
//
// Per ADR-0010 and pillar 2, every Lahijan tenant maps to one or more
// canonical zones in PowerDNS. The driver itself never carries a tenant_id;
// the service layer (WS-15) consults the dns_zones table (migration 0026)
// to translate a tenant context into the canonical zone id before calling
// the driver. Per pillar 1 the Name() string "powerdns" is internal-only;
// end users see "dns".
package powerdns
