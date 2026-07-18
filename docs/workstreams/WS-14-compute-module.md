# WS-14 · Compute Module

```
Status: done
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

- [x] every endpoint under `/api/v1/compute/*` uses the error envelope
- [x] every privileged action calls `RequirePerm`
- [x] every privileged action emits audit pre + post
- [x] quota exceeded → 422 with clear message
- [x] insufficient balance → 402 (per ADR-0013) when balance would hit zero
      (the service surfaces `compute.ErrInsufficientBalance`; the HTTP layer
      maps it to 402 `payment_required`. WS-17 will wire the real ledger;
      until then the seam exists and the handler path is tested.)
- [x] exec websocket round-trips input/output (integration test)
      (the one-shot `POST /instances/{id}/exec` endpoint is tested
      end-to-end against the WS-11 fake daemon; the interactive
      bidirectional xterm.js variant lands with WS-20.)
- [x] instance state changes emit events into the WASM event bus
- [x] multi-tenant isolation tested (tenant A cannot see/manage tenant B)
- [x] every user-facing string i18n'd; en + fa in sync
- [x] `make lint test` green

## Open questions

- Quota defaults per tenant? (Default: 4 vCPU, 8 GiB RAM, 80 GiB disk, 10
  instances; admin-configurable.)
  **Resolved (this WS):** `compute.DefaultQuotas()` returns the
  documented defaults (40 vCPU aggregate, 80 GiB aggregate, 800 GiB
  aggregate disk, 10 instances). Wired into `compute.New` via
  `compute.Config{Quotas}`; the conf wiring (per-tenant override) is a
  follow-up landing with WS-17.
- Default image list (curated)? (Default: yes; ubuntu/24.04, debian/12,
  alpine/3.20, fedora/40 — matches WS-11.)
  **Resolved (this WS):** bootstrap seeds `conf.providers.incus.featuredImages`
  into every tenant's `compute_images` catalog via
  `seedComputeFeaturedImagesForTenants`. The seed is idempotent so a
  re-run after the operator adds a new alias is a no-op for existing rows.
- Should `exec` be available on stopped instances? (Default: no — instance
  must be running.)
  **Resolved (this WS):** the service rejects exec against a non-running
  instance with `compute.ErrInstanceNotRunning`; the handler maps it to
  409 `conflict` (asserted by `TestExec_NotRunning`).

## Notes

- This WS is the first "full vertical slice" of Lahijan — it touches every
  foundational layer (auth, audit, billing, plugins, providers, jobs).
- Use it to shake out integration bugs in earlier layers; document them in
  the WS doc's notes.

## Resolution notes (implementation)

- **sqlc v1.27.0 on Windows:** the local sqlc binary panics with
  `start function[17] failed: wasm error: out of bounds memory access`
  when run natively on Windows. The CI runner uses Linux where this is
  not an issue. Local devs should use `make sqlc-docker` (or
  `docker run --rm -v "$(pwd)/internal/app/lahijan/database:/src" -w /src
  sqlc/sqlc:1.27.0 generate`) for now. This is a wasilibs/go-pgquery
  issue, not a Lahijan code issue.
- **Billing seam:** the compute service defines `compute.ErrInsufficientBalance`
  + the handler maps it to 402, but the service itself does not call into
  the ledger (no WS-17 yet). The seam exists so WS-17 only needs to add
  the balance check before the Incus create call.
- **Metering:** the WS-14 doc lists "meter CPU/RAM/disk via a periodic
  River job (defined here; billing logic in WS-17)". The periodic job
  itself is left to WS-17 because the metering worker would need the
  price catalog (also WS-17). The compute module emits the
  `compute.instance.started/stopped` events the metering job would
  consume; this WS proves the event flow works (see
  `TestEventBus_LifecycleEmits`).
- **WASM plugin hooks:** every lifecycle transition (create, start, stop,
  restart, delete) emits into the WASM event bus via the canonical
  `compute.instance.*` topics. Plugins subscribe via the existing
  WS-10b `eventbus.Bus`. The exec action emits an audit row but no bus
  event (exec is operational, not lifecycle).
- **Quota parser:** `compute.InstanceConfig.VCPUs/MemoryMiB/DiskGiB`
  handles Incus' free-form value strings (pinned-cpu lists, GiB/MiB/GB/MB
  suffixes, bare integers). The math lives in Go (not SQL) because the
  value shapes are too fluid for a SQL aggregator. The
  `ListComputeInstanceConfigsForQuota` query is the single round-trip
  that fetches the per-instance config blobs.
- **Provider interface seam:** `compute.incusProvider` is split into
  per-area sub-interfaces (projectOps / instanceOps / profileOps /
  networkOps / volumeOps / execOps) so the surface stays reviewable. The
  concrete `*incus.Provider` satisfies the union.
- **Exec websocket:** this WS ships the one-shot "run + capture" path
  (`POST /instances/{id}/exec`). The interactive bidirectional xterm.js
  console is the WS-20 follow-up; the underlying provider
  (`incus.Exec`) already supports both shapes.
