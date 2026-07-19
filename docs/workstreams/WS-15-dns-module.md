# WS-15 · DNS Module

```
Status: done
Phase: 4
Depends on: WS-12, WS-08
Unblocks: WS-21 (DNS UI)
```

## Goal

Expose DNS (PowerDNS) through Lahijan's user-facing API. Users manage zones
and records, with validation, templates, DNSSEC toggle, and full audit.

## Scope

**In scope:**
- `internal/app/lahijan/dns/`:
  - `service.go` — orchestrates PDNS provider calls + audit + plugin hooks
  - `zones.go` — zone CRUD per tenant; zone transfer settings
  - `records.go` — RRset CRUD with record-type-specific validation (A, AAAA,
    CNAME, MX, TXT, NS, SOA, SRV, CAA, PTR)
  - `templates.go` — predefined zone templates (e.g. "mail server set",
    "verify Google Workspace"); templates apply on zone create or later
  - `dnssec.go` — DNSSEC enable/disable per zone; key rotation
- API endpoints under `/api/v1/dns/*`:
  - `GET/POST /zones`, `GET/PATCH/DELETE /zones/{id}`
  - `GET/POST /zones/{id}/records`, `GET/PATCH/DELETE /zones/{id}/records/{id}`
  - `POST /zones/{id}/dnssec/{enable|disable}`
  - `GET /templates`, `POST /zones/{id}/apply-template`
- DB tables (tenant-scoped):
  - `dns_zones` (id, tenant_id, name, kind (master/native), dnssec_enabled,
    created_at, updated_at, deleted_at)
  - `dns_records` (id, zone_id, tenant_id, name, type, content, ttl, prio,
    disabled, created_at, updated_at)
- WASM plugin hooks: every zone/record change emits events
- Audit: every privileged action emits audit pre + post
- Validation: record content matches the record type (e.g. A record must be a
  valid IPv4)

**Out of scope:**
- Recursor / dnsdist / registrar resale (WS-28).
- GeoDNS / load-balancing (Phase 7).
- Reverse DNS for Lahijan-assigned IPs (WS-30).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0006-managed-dependencies.md`
- WS-08 doc (RBAC + audit)
- WS-12 doc (PowerDNS provider)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- Full DNS API surface
- Tenant-scoped DNS tables
- Record-type validators
- Zone templates
- DNSSEC toggle
- WASM event emission on every change
- Integration tests covering every record type + DNSSEC flow

## Definition of Done

- [x] every endpoint under `/api/v1/dns/*` uses the error envelope
- [x] every privileged action calls `RequirePerm` + emits audit
- [x] invalid record content rejected with a clear message (per type)
- [x] DNSSEC enable/disable propagates to PowerDNS
- [ ] records are queryable via `dig` against the PDNS endpoint after create
- [x] tenant isolation tested
- [x] templates apply correctly
- [x] every user-facing string i18n'd; en + fa in sync
- [x] `make lint test` green

## Resolution notes (implementation)

- **Per-type validator coverage (DoD item 3):** the validators under
  `internal/app/lahijan/dns/validators.go` cover every supported type
  (A, AAAA, CAA, CNAME, DS, MX, NS, PTR, SOA, SRV, TLSA, TXT). The
  test suite (`validators_test.go` + the integration test
  `TestCreateRecord_InvalidContent`) walks both the accept and the
  reject path for every type so a regression that submits malformed
  content to PDNS is caught at the Lahijan boundary, not at the
  daemon. The validator messages surface verbatim in the 400 envelope
  so the UI can render per-type context (e.g. "A record content must
  be a valid IPv4 address") rather than PDNS' opaque "Bad Request".
- **`dig` against a live PDNS endpoint (DoD item 5):** the WS-12 doc
  explicitly defers the live-`dig` end-to-end test to WS-22
  (sandbox-integration-test-harness). The Lahijan-side coverage is
  the same shape as WS-12's: a fake PDNS server (httptest) drives the
  provider through the real REST client, and the integration tests
  assert every code path the live `dig` would also exercise (zone +
  record CRUD, DNSSEC toggle). The live-`dig` operator validation
  goes through the `PDNS_DNS_PORT=5300` host port published by
  compose; that path belongs to WS-22.
- **DNSSEC events in the WASM bus:** the canonical registry in
  `wasm/eventbus/events.go` now declares the DNSSEC enable/disable/
  rotate topics so plugins can subscribe to `dns.zone.dnssec.*`. The
  PDNS driver already emits the corresponding events on the wire (per
  WS-12 resolution notes); the WS-15 service re-emits them with actor
  context so plugin subscriptions see the user who triggered the
  change.
- **ApplyTemplate idempotence:** the WS-15 doc lists templates as in
  scope; the apply path re-uses the per-record create flow so the
  same audit + validator + bus wiring applies. Running the same
  template twice is a no-op (the unique `(zone_id, name, type,
  content)` constraint short-circuits the duplicate).
- **`%w` placeholder in templates:** the catalog exposes two
  placeholders. `%s` is replaced with the zone's canonical id
  INCLUDING the trailing dot (e.g. `example.com.`) — used for the
  Name field and for content where the canonical name is the value.
  `%w` is replaced with the zone's canonical id WITHOUT the trailing
  dot (e.g. `example.com`) — used for content where the zone name
  appears mid-string (e.g. the Microsoft 365 MX target
  `<zone>.mail.protection.outlook.com.`). The unit test
  (`TestExpandTemplatePlaceholder`) covers both shapes plus the
  multi-placeholder case.

## Open questions

- Allow user to set custom TTL? (Default: yes, within `[300, 86400]`.)
  — **resolved; see above.** `dns.ValidateTTL` enforces the bounds;
  the constant pair (`MinTTL`, `MaxTTL`) lives in `validators.go` so
  a future WS can lift it into config without touching call sites.
- CNAME-to-apex allowed? (Default: no — strict RFC.) — **resolved; see
  above.** `CreateRecord` rejects CNAME at the apex with
  `ErrCNAMEAtApex`; the integration test
  `TestCreateRecord_CNAMEAtApex` asserts.
- Zone name uniqueness: per-tenant or global? (Default: per-tenant.)
  — **resolved (this WS):** the global uniqueness enforced by the
  `dns_zones.canonical_id` index (WS-12) is the right call because
  two tenants cannot both serve the same zone from the same PDNS
  daemon — whoever got there first wins. The per-tenant view is
  preserved at the API (a tenant only sees their own zones) but the
  cross-tenant uniqueness is the platform-level invariant. The
  integration test `TestCreateZone_DuplicateCanonical` covers it.

## Notes

- The record-content validator should match PDNS' own validation so we don't
  submit something PDNS rejects.
- DNS records carry no PII; logging is straightforward.
