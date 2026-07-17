# WS-12 · PowerDNS Provider

```
Status: pending
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

- [ ] `make dev-up` starts PDNS reachable at the configured URL
- [ ] create a zone through the client → it's queryable via `dig` against PDNS
- [ ] create an A record → resolves
- [ ] delete a record → gone
- [ ] DNSSEC enable/disable works
- [ ] every privileged action emits an audit event
- [ ] every change emits an event into the WASM event bus
- [ ] tenant isolation: tenant A cannot list/modify tenant B's zones
- [ ] `make lint test` green

## Open questions

- Default DNSSEC policy: on or off for new zones? (Default: off; admin enables
  per zone.)
- Allow AXFR by default? (Default: no — Lahijan is the only NS by default.)
- API version: pin to PDNS auth 4.x or 5.x? (Default: latest stable at deploy
  time; pin in compose.)

## Notes

- The PDNS API key is the one sensitive that must be set in env, never in
  checked-in config.
- PDNS' `schema.pgsql.sql` is shipped upstream; copy it into
  `deployments/powerdns/` so we control the version.
