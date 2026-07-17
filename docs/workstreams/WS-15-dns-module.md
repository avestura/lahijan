# WS-15 · DNS Module

```
Status: pending
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

- [ ] every endpoint under `/api/v1/dns/*` uses the error envelope
- [ ] every privileged action calls `RequirePerm` + emits audit
- [ ] invalid record content rejected with a clear message (per type)
- [ ] DNSSEC enable/disable propagates to PowerDNS
- [ ] records are queryable via `dig` against the PDNS endpoint after create
- [ ] tenant isolation tested
- [ ] templates apply correctly
- [ ] every user-facing string i18n'd; en + fa in sync
- [ ] `make lint test` green

## Open questions

- Allow user to set custom TTL? (Default: yes, within `[300, 86400]`.)
- CNAME-to-apex allowed? (Default: no — strict RFC.)
- Zone name uniqueness: per-tenant or global? (Default: per-tenant — two
  tenants can both have `example.com.` if their NS points to us; operator's
  responsibility to avoid conflict at the registry level.)

## Notes

- The record-content validator should match PDNS' own validation so we don't
  submit something PDNS rejects.
- DNS records carry no PII; logging is straightforward.
