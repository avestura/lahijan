# WS-08 · RBAC + Audit Log

```
Status: done
Phase: 1
Depends on: WS-06
Unblocks: WS-10a, WS-14..17 (every privileged action needs RBAC + audit)
```

## Goal

Make "every privileged action calls `RequirePerm` and emits an audit event"
real. This WS lands the role/permission model, the policy enforcement
middleware, the tenant-scoping middleware, the append-only audit table with
trigger-enforced immutability, and the query/export API.

## Scope

**In scope:**

### RBAC (`internal/app/lahijan/auth/rbac/`)
- Permission model: every permission is `scope.action` (e.g.
  `compute.instance.create`, `dns.zone.delete`, `billing.balance.adjust`).
- Role model: roles bundle permissions; seeded defaults: `tenant.owner`,
  `tenant.admin`, `tenant.member`, `tenant.viewer`.
- `user_roles` table (global, with `tenant_id` for tenant-scoped roles; some
  roles like `platform.admin` are global).
- `RequirePerm("scope.action")` Fiber middleware + Go helper for services.
- Permission registry: a single Go source of truth listing all permissions
  (used to seed the DB, generate docs, and validate grants).
- Tenant context middleware: extracts `tenant_id` from the session (or
  `tenant_slug` from the URL), sets it in context + `c.Locals`.

### Audit log (`internal/app/lahijan/auth/audit/`)
- `audit_log` table (already created in WS-03).
- DB triggers preventing `UPDATE` and `DELETE` (verify + test in WS-03; this
  WS adds the **emission** pipeline).
- Audit emitter interface + DB implementation:
  - `Emit(ctx, event)` writes a row with `pending` status before the side
    effect.
  - `MarkOutcome(ctx, auditID, status, details)` updates the row.
  - Both go through the repository layer so we can swap to an outbox later.
- Event shape: `tenant_id (nullable), actor_user_id, actor_type, action,
  resource_type, resource_id, status, request_id, metadata JSONB, created_at,
  updated_at`.
- Standard library of events: `auth.login`, `auth.logout`, `auth.token.issued`,
  `compute.instance.create`, `dns.record.create`, etc. (one per privileged
  action across all WSs).
- API:
  - `GET /api/v1/audit` (paginated, filterable by tenant/actor/action/date)
  - `GET /api/v1/audit/{id}`
  - `GET /api/v1/audit/export?format=json|csv`

**Out of scope:**
- The actual privileged actions themselves (those live in WS-14..17). This WS
  lands the framework + an `auditLog` helper that all those WSs call.
- Tamper-evident audit chain (hash chaining) — Phase 7 candidate.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0002-tenancy-model.md`
- `docs/adr/0004-auth-methods.md`
- WS-06 doc
- `docs/architecture/conventions.md#security`
- `.opencode/skills/database-conventions/SKILL.md`
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- Permission registry (Go) with all seed permissions documented
- Default role seeds
- Tenant context middleware (used by every authenticated route from here on)
- `RequirePerm` middleware + Go helper
- Audit emitter (interface + DB impl)
- Audit query API with filters + export
- DB triggers immutability-tested (UPDATE/DELETE rejected)
- i18n strings for all audit action descriptions

## Definition of Done

- [x] every authenticated route carries a tenant_id in context (when applicable)
- [x] every privileged endpoint has `RequirePerm` on it
- [x] `RequirePerm` returns the standard error envelope when denied
- [x] audit table rejects UPDATE and DELETE (integration test)
- [x] audit emit + mark-outcome flow tested end-to-end
- [x] audit query API supports filters + pagination + export
- [x] audit actions are i18n-keyed; en + fa in sync
- [x] every privileged action in WS-06 (auth) is retrofitted to emit audit
- [x] `make lint test` green

## Open questions

- Hash-chained audit rows (each row includes `prev_hash`)? Strong tamper
  evidence but adds write cost. **Resolved (this WS):** deferred to Phase 7.
  The audit_log and audit_log_outcomes tables are both fully immutable
  (trigger-enforced), which is the contract this WS was scoped to land.
- Streaming export for large audit queries? **Resolved (this WS):** CSV export
  uses Fiber's `BodyWriter` so each row flushes as it's written; the
  `ExportRowCap` constant caps the in-memory cost at 10 000 rows. True
  chunked-transfer streaming (server-side cursors) is a Phase 7 candidate
  once we have a profiled workload.

## Notes

- This WS makes audit a first-class primitive that all later modules consume.
- Every Phase 3/4 WS doc references this WS in its "Required reading".
- platform.admin is currently per-tenant (a membership with that role grants
  the full catalog inside the tenant it's held in). A truly global
  platform.admin (cross-tenant override without per-tenant memberships) is
  deferred — the WS-08 doc lists it as "global" but the existing memberships
  table is NOT NULL on tenant_id. A future WS will add the cross-tenant
  bypass via either a separate `user_roles` table (NULL tenant_id for global
  roles) or an `is_platform_admin` flag on `users`.

## Notes

- This WS makes audit a first-class primitive that all later modules consume.
  Get the emitter interface right.
- Every Phase 3/4 WS doc references this WS in its "Required reading".
