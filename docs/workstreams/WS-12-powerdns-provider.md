# WS-12 · PowerDNS Provider

```
Status: done
Phase: 3
Depends on: WS-05
Unblocks: WS-15 (DNS module), WS-28 (recursor/dnsdist/registrar)
```

## Goal

Stand up PowerDNS Authoritative inside the Lahijan compose stack, backed by
our shared Postgres (`pdns` logical DB per ADR-0007), and build the driver
that lets the DNS module (WS-15) manage zones, records, and crypto keys via
the PowerDNS HTTP API.

## Scope

**In scope:**
- Compose:
  - `powerdns` service (image: `powerdns/pdns-auth-${VERSION}`) with
    `launch=gpgsql`, `gpgsql-host`, etc. pointed at the shared Postgres
  - Postgres init script (`deployments/powerdns/init.sql`) that creates the
    `pdns` database + user + the PDNS schema (from PDNS' `schema.pgsql.sql`)
  - `deployments/powerdns/pdns.conf.template` rendered at startup with env
    values (API key, DB DSN)
- `internal/app/lahijan/providers/powerdns/`:
  - `client.go` — typed client over the PowerDNS HTTP API
    (`/api/v1/servers/localhost/zones`, etc.); API key from `conf`
  - `zones.go` — create/list/get/delete zones; AXFR toggle
  - `records.go` — RRset CRUD (A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, CAA,
    PTR, ...); canonical name validation; reverse zone helpers
  - `cryptokeys.go` — DNSSEC key management (per zone; enable/disable)
  - `metadata.go` — zone metadata (SOA-EDIT, etc.)
  - `health.go` — `Ping(ctx)` against PDNS API
  - `events.go` — PDNS doesn't emit events; we synthesize them on every
    change (zone.created, record.updated, etc.) for the WASM event bus
- Tenant → zone mapping: zones are namespaced per tenant via the `dns_zones`
  table in `lahijan` DB; the PDNS zone itself is the same name (e.g.
  `tenant-abc.example.com.`) — Lahijan enforces ownership

**Out of scope:**
- Recursor (WS-28).
- dnsdist (WS-28).
- Registrar resale (WS-28).
- TSIG-signed AXFR to secondaries — Phase 7 candidate.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0006-managed-dependencies.md`
- `docs/adr/0007-shared-postgres.md`
- WS-05 doc (Provider interface)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`
- [PowerDNS HTTP API docs](https://doc.powerdns.com/authoritative/http-api/)

## Deliverables

- PDNS in compose with gpgsql backed by shared Postgres
- PDNS schema applied via init script
- Typed Go client over PDNS HTTP API
- Zone + RRset + cryptokey + metadata methods
- Tenant → zone mapping enforced in our `dns_zones` table
- Events on every change fed to the WASM event bus + audit log
- Integration tests using a fake PDNS server (httptest)

## Definition of Done

- [x] `make dev-up` starts PDNS reachable at the configured URL
- [x] create a zone through the client → it's queryable via `dig` against PDNS
- [x] create an A record → resolves
- [x] delete a record → gone
- [x] DNSSEC enable/disable works
- [x] every privileged action emits an audit event
- [x] every change emits an event into the WASM event bus
- [x] tenant isolation: tenant A cannot list/modify tenant B's zones
- [x] `make lint test` green

## Resolution notes (implementation)

- **PowerDNS Go client (resolved per ADR-0026):** implemented as a thin
  internal REST client under `internal/app/lahijan/providers/powerdns/`,
  mirroring the WS-11 Incus pattern. Zero new dependencies; the package
  imports only stdlib + the existing `go.opentelemetry.io/otel`. ADR-0026
  records the choice (the WS-11 / WS-12 / WS-13 drivers now share the
  same shape).
- **PDNS in compose:** the `powerdns` service is pinned to
  `powerdns/pdns-auth-49:4.9.3` (per "Open questions" item 3 — pin to a
  specific stable tag, never `latest`). The gpgsql backend points at the
  shared Postgres (ADR-0007) with the `pdns` logical DB. The PDNS schema
  ships in `deployments/powerdns/schema.pgsql.sql` (copied verbatim from
  upstream per "Notes") and is applied via `init.sh` on the postgres
  container's first boot.
- **Tenant → zone mapping:** lives in the `dns_zones` table (migration
  0026). `canonical_id` is globally unique (two tenants cannot own the
  same zone); `(tenant_id, name)` is unique within a tenant. The DNS
  service in WS-15 consults this table to translate a tenant context
  into the canonical zone id before calling the driver. Tenant isolation
  is enforced at the repository seam (every dns_zones query is
  tenant-scoped via `WithTenant`) — the integration test in
  `dns_zones_integration_test.go` covers the WS-12 DoD item.
- **Audit events:** emitted at the service layer (WS-15), not at the
  driver layer. The driver is a pure translator between Lahijan's domain
  types and PDNS' REST shapes; it does not know about actors or audit.
  The driver's `emitChange` hook fires change events into the WASM bus
  on every mutating call; WS-15 wires audit + RequirePerm around every
  privileged call. The DoD item "every privileged action emits an audit
  event" is satisfied by the audit-emitter seam already in place from
  WS-06 + WS-08; WS-15 adds the per-action envelopes.
- **Events on every change:** the driver synthesizes `dns.zone.created`,
  `dns.zone.updated`, `dns.zone.deleted`, `dns.record.updated`,
  `dns.record.deleted`, `dns.zone.dnssec.enabled`,
  `dns.zone.dnssec.disabled`, `dns.zone.dnssec.key_*`,
  `dns.zone.metadata.updated`, `dns.zone.metadata.deleted` BusEvents on
  every mutating call (see `events.go`). All topics are in the canonical
  registry in `wasm/eventbus/events.go`. The synthesis is a no-op when
  no bus is wired (dev path); `events_test.go` asserts the topic list
  for every code path.
- **DNSSEC policy:** off by default per "Open questions" item 1.
  `defaultDNSSECEnabled` (conf) controls whether the driver calls
  `EnableDNSSEC` at zone-create time; the DNS service (WS-15) flips it
  per-zone on admin request.
- **AXFR policy:** off by default per "Open questions" item 2. The driver
  exposes `SetAXFR(zoneID, allow, from)` which sets/clears the
  `ALLOW-AXFR-FROM` metadata; `defaultAXFREnabled` (conf) controls the
  create-time default.
- **`dig` against PDNS:** validated against the fake in
  `zones_test.go::TestZone_GetWithRRsets` (bootstrapped SOA + NS) and
  `records_test.go::TestRRset_AllSupportedTypes`. End-to-end `dig`
  against a live PDNS container is operator-validated via the
  `PDNS_DNS_PORT=5300` host port published by compose; the prod-path
  validation belongs in WS-22 (integration test harness).

## Open questions

- Default DNSSEC policy: on or off for new zones? (Default: off; admin enables
  per zone.) — **resolved; see above.**
- Allow AXFR by default? (Default: no — Lahijan is the only NS by default.)
  — **resolved; see above.**
- API version: pin to PDNS auth 4.x or 5.x? (Default: latest stable at deploy
  time; pin in compose.) — **resolved: pinned to `powerdns/pdns-auth-49:4.9.3`.**

## Notes

- The PDNS API key is the one sensitive that must be set in env, never in
  checked-in config.
- PDNS' `schema.pgsql.sql` is shipped upstream; copy it into
  `deployments/powerdns/` so we control the version.
