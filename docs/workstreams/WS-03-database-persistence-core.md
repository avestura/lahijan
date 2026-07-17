# WS-03 · Database & Persistence Core

```
Status: pending
Phase: 0
Depends on: WS-02
Unblocks: WS-04, WS-05, WS-06, WS-08, WS-09 (everything that touches the DB)
```

## Goal

Stand up the database layer that every domain module will sit on: a real
Postgres in dev compose, sqlc configured and generating code, golang-migrate
skeleton with the first batch of base tables, the `tenant_id` discipline
enforced at the repository layer, and test fixtures that every later WS can
reuse.

## Scope

**In scope:**
- `deployments/docker-compose.dev.yml` with Postgres 16 (one container, three
  logical DBs: `lahijan`, `pdns`, `seaweed` — per ADR-0007; this WS creates
  only `lahijan`; WS-12 and WS-13 add the others when they land).
- `deployments/.env.example` with `POSTGRES_*`, `LAHIJAN_DATABASE_*`, etc.
- Postgres init script (mounted into `/docker-entrypoint-initdb.d/`) that
  creates the three databases and three users.
- `internal/app/lahijan/database/` package:
  - `sqlc.yaml` (sqlc v2 config; one query file per area)
  - `migrations/` (golang-migrate; paired up + down)
  - `queries/` (sqlc query files; one per area)
  - `gen/` (sqlc-generated code; gitignored-or-not per team preference)
  - `pool.go` (pgx pool setup)
  - `<area>_repo.go` (typed wrappers)
- Base schema migrations (0001..NNNN):
  - `tenants` (global)
  - `users` (global; `password_hash` nullable for OAuth-only users)
  - `memberships` (user ↔ tenant; role)
  - `roles`, `permissions`, `role_permissions` (global)
  - `audit_log` (tenant_id nullable; append-only; trigger-enforced)
  - `refresh_tokens`, `personal_access_tokens` (global)
- `database/testutil/`:
  - testcontainers-go harness (spin PG per test package)
  - factories (`NewTenant`, `NewUser`, etc.)
- Tenant-scoping enforcement: every tenant-scoped repo extracts `tenant_id`
  from context and bakes it into queries.

**Out of scope:**
- Domain-specific tables (instances, zones, buckets, ledger) — those land in
  their respective Phase 3/4 WSs.
- PowerDNS's `pdns` DB and SeaweedFS's `seaweed` DB tables — those are owned
  by WS-12 and WS-13.
- RBAC policy enforcement code (WS-08).
- Audit event emission from services (WS-08).

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `docs/adr/0002-tenancy-model.md`
- `docs/adr/0003-db-tooling.md`
- `docs/adr/0007-shared-postgres.md`
- `docs/architecture/conventions.md#database`
- `.opencode/skills/database-conventions/SKILL.md`
- `.opencode/skills/testing-conventions/SKILL.md`
- `deployments/AGENTS.md`

## Deliverables

- A working `make dev-up` that starts Postgres.
- A working `make db-up` / `make db-down` that applies and rolls back
  migrations.
- A working `make sqlc` that regenerates code.
- Base tables with constraints and indexes per the conventions skill.
- A `database/pool.go` that returns a `*pgxpool.Pool` configured from `conf`.
- An `audit_log` table with `INSTEAD OF` triggers preventing UPDATE/DELETE.
- A testutil harness that integration tests in later WSs can import.

## Definition of Done

- [ ] migrations up + down both apply cleanly (CI's `migrations.yml` green)
- [ ] `make sqlc` produces generated code; no diff after re-running
- [ ] every tenant-scoped query filters by `tenant_id`
- [ ] `audit_log` rejects UPDATE/DELETE (integration test)
- [ ] every base table has `created_at` + `updated_at` + index on `tenant_id`
      where applicable
- [ ] testcontainers harness works in CI
- [ ] `make lint test` green

## Open questions

- Soft-delete pattern: do we use `deleted_at TIMESTAMPTZ` everywhere, or only
  where explicitly needed? (Default: only where needed; tables that need it
  add it explicitly.)
- Do we want a `slug` column on `tenants` for URL-friendly identifiers?
  (Default: yes.)

## Notes

- The migrations `migrations.yml` workflow (added in WS-01) will start running
  real checks once the first migration lands here.
- pgxpool configuration (max conns, statement timeout) should be configurable
  via `conf`.
