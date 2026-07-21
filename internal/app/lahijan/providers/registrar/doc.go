// Package registrar is Lahijan's driver layer for third-party domain
// registrars (WS-28). It exposes a Provider interface that translates
// Lahijan's domain lifecycle (search, register, renew, transfer,
// DNSSEC-at-parent) into a concrete registrar's HTTP API.
//
// Per ADR-0035 the package follows the same shape as the Phase-3
// providers (Incus / PowerDNS / SeaweedFS): a thin internal REST client,
// no upstream Go SDK dependency, an httptest fake under fake/ for
// tests. The package imports only stdlib + go.opentelemetry.io/otel.
//
// # Layout
//
//   - provider.go — Provider interface + Capabilities.
//   - types.go    — request/response shapes used by the interface.
//   - errors.go   — sentinel errors + REST error envelope decoder.
//   - tracing.go  — package-local OpenTelemetry tracer (per ADR-0016).
//   - opensrs.go  — OpenSRS / Tucows Reseller API implementation.
//   - fake/server.go — httptest registrar for tests (WS-28 service tests).
//
// # Tenant mapping
//
// Per pillar 2 every Lahijan tenant can register domains through the
// registrar; the driver itself never carries a tenant_id. The service
// layer (internal/app/lahijan/registrar/) consults the dns_domains table
// (migration 0046) to translate a tenant context into the order id
// before calling the driver.
//
// # Transparency
//
// Per pillar 1 the registrar's brand name NEVER surfaces to end users.
// The Provider implementations expose Name() strings that are
// internal-only ("opensrs", "resellerclub", ...); the user-facing API
// + UI say "register domain" or "transfer domain".
package registrar
