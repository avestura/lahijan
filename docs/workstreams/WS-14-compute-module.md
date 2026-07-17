# WS-14 · Compute Module

```
Status: pending
Phase: 4
Depends on: WS-11, WS-08, WS-09
Unblocks: WS-20 (compute UI), WS-22 (e2e needs compute), WS-24, WS-25
```

## Goal

Expose compute (Incus) through Lahijan's user-facing API and UI shell. Users
create instances from images, manage their lifecycle, exec into them via
xterm.js, and every action is permission-checked, audited, and metered.

## Scope

**In scope:**
- `internal/app/lahijan/compute/`:
  - `service.go` — orchestrates Incus provider calls + billing checks + audit
  - `instances.go` — instance CRUD + lifecycle (start/stop/restart/freeze/etc.)
  - `images.go` — image catalog (alias → image; curated list + custom uploads)
  - `profiles.go` — profile CRUD per tenant
  - `networks.go` — per-tenant networks + ACLs
  - `storage.go` — per-tenant storage volumes
  - `console.go` — exec websocket proxy (uses WS-11's `exec.go`)
- API endpoints under `/api/v1/compute/*`:
  - `GET/POST /instances`, `GET/PATCH/DELETE /instances/{id}`
  - `POST /instances/{id}/{start|stop|restart|freeze|unfreeze}`
  - `POST /instances/{id}/exec` (websocket upgrade)
  - `GET/POST /instances/{id}/snapshots`, `DELETE .../snapshots/{name}`
  - `GET/POST /images`, `GET/POST /profiles`, `GET/POST /networks`,
    `GET/POST /storage`
- DB tables (tenant-scoped):
  - `compute_instances` (id, tenant_id, name, project_name, status,
    image_alias, profile, config_json, created_at, updated_at, deleted_at)
  - `compute_images` (id, tenant_id, alias, source, fingerprint, ...)
  - `compute_profiles`, `compute_networks`, `compute_storage_volumes`
- Quota enforcement (per-tenant caps on CPU/RAM/disk/instance-count)
- Billing integration:
  - on create: check balance + reserve initial deposit
  - while running: meter CPU/RAM/disk via a periodic River job (defined here;
    billing logic in WS-17)
  - on stop: release reservation
- WASM plugin hooks: every lifecycle event emits into the event bus
- Audit: every privileged action emits an audit event before + after

**Out of scope:**
- noVNC console for VMs (WS-24).
- Scheduled snapshots (WS-25).
- Public IP assignment (WS-30).
- Multi-node cluster scheduling (WS-26).
- The compute UI dashboard (WS-20 — but this WS ships the API the UI uses).

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `docs/adr/0010-full-incus-surface.md`
- `docs/adr/0009-multi-node-ready.md`
- `docs/adr/0013-ledger-billing.md`
- WS-08 doc (RBAC + audit)
- WS-09 doc (River — metering + lifecycle events)
- WS-11 doc (Incus provider)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- Full compute API surface
- Tenant-scoped compute tables
- Instance lifecycle with audit + metering hooks
- xterm.js exec websocket proxy
- Quota enforcement
- WASM event emission on every lifecycle transition
- Integration tests: create → start → exec → stop → delete, with quota + audit
  assertions

## Definition of Done

- [ ] every endpoint under `/api/v1/compute/*` uses the error envelope
- [ ] every privileged action calls `RequirePerm`
- [ ] every privileged action emits audit pre + post
- [ ] quota exceeded → 422 with clear message
- [ ] insufficient balance → 402 (per ADR-0013) when balance would hit zero
- [ ] exec websocket round-trips input/output (integration test)
- [ ] instance state changes emit events into the WASM event bus
- [ ] multi-tenant isolation tested (tenant A cannot see/manage tenant B)
- [ ] every user-facing string i18n'd; en + fa in sync
- [ ] `make lint test` green

## Open questions

- Quota defaults per tenant? (Default: 4 vCPU, 8 GiB RAM, 80 GiB disk, 10
  instances; admin-configurable.)
- Default image list (curated)? (Default: yes; ubuntu/24.04, debian/12,
  alpine/3.20, fedora/40 — matches WS-11.)
- Should `exec` be available on stopped instances? (Default: no — instance
  must be running.)

## Notes

- This WS is the first "full vertical slice" of Lahijan — it touches every
  foundational layer (auth, audit, billing, plugins, providers, jobs).
- Use it to shake out integration bugs in earlier layers; document them in
  the WS doc's notes.
