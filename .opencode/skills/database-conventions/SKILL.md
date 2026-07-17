---
name: database-conventions
description: "Use when writing SQL, creating migrations, adding sqlc queries, or designing tables for Lahijan. Triggers on *.sql files, sqlc.yaml edits, migrations under internal/app/lahijan/database/migrations, repository code, tenant_id discipline, indexing decisions."
---

# Database Conventions — PostgreSQL + sqlc + golang-migrate

Load this whenever you touch SQL or repositories.

## Toolchain

- **PostgreSQL 16+.**
- **sqlc** for query type codegen (config: `internal/app/lahijan/database/sqlc.yaml`).
- **golang-migrate** for migrations (dir: `internal/app/lahijan/database/migrations`).
- **pgx v5** as the underlying driver (pool managed in `database/pool.go`).

No GORM. No sqlx. No squirrel. No `db.Query` in services.

## Migration rules

- Naming: `NNNN_short_name.up.sql` + `NNNN_short_name.down.sql`, zero-padded,
  monotonic.
- One logical change per migration.
- **Every up must have a reversible down.** CI enforces this.
- **Never edit a merged migration.** New change = new migration.
- **Always test both directions** locally: `make db-up && make db-down && make db-up`.
- Don't put app code in migrations (no PL/pgSQL triggers except for audit log).

## Schema rules

- All tables (with exceptions below) have:
  - `id UUID PRIMARY KEY DEFAULT gen_random_uuid()`
  - `tenant_id UUID NOT NULL REFERENCES tenants(id)`
  - `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
  - `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
  - `deleted_at TIMESTAMPTZ` (nullable; soft-delete if used)
- **Exceptions (global tables, no tenant_id):** `tenants`, `users`, `roles`,
  `permissions`, `audit_log` (audit has `tenant_id` but it can be NULL for
  system-level events), `schema_migrations`, River's `river_*`.
- Index every `tenant_id` column. Composite indexes for `(tenant_id, <lookup>)`
  where it's a hot path.
- Use `TEXT` not `VARCHAR(N)` — Postgres has no perf difference; enforce
  length limits in the application layer.
- Use `BIGINT` not `INT` for anything that might grow (counts, sizes, IDs
  from external systems).
- Use `JSONB` for flexible blobs; document the schema in a comment.

## Tenant scoping (hard rule)

- **Every query that reads user data must filter by `tenant_id`.**
- The repository layer enforces this via a `WithTenant(ctx)` wrapper that
  extracts the tenant from context and bakes it into the query.
- Never write a query like `SELECT * FROM instances` — always
  `SELECT * FROM instances WHERE tenant_id = $1`.
- sqlc query annotations include `$1` as the tenant ID by convention.

## sqlc conventions

- Queries live in `database/queries/<area>.sql`, one file per area.
- Each query has a leading SQL comment:
  ```sql
  -- name: GetInstance :one
  --: tenant-scoped
  SELECT * FROM instances WHERE tenant_id = $1 AND id = $2;
  ```
- Generated code goes to `database/gen/<area>/`.
- Repository wrappers go in `database/<area>_repo.go` and add typed methods on
  top of the generated code.

## Naming

- Tables: `snake_case`, plural (`instances`, `dns_zones`, `audit_log`).
- Columns: `snake_case`.
- Foreign keys: `<referenced_table_singular>_id` (`tenant_id`, `user_id`).
- Booleans: prefix with `is_` or `has_` (`is_enabled`, `has_public_ip`).
- Timestamps: `<verb>_at` (`created_at`, `deleted_at`, `started_at`).
- JSONB blobs: `<noun>_json` or `<noun>_doc` (`config_json`).

## Indexing checklist

- Every `tenant_id` → indexed.
- Every foreign key that you join on → indexed.
- Every field in a `WHERE` clause that's high-selectivity → indexed.
- Unique constraints where business rules require them (e.g.
  `(tenant_id, slug) UNIQUE`).
- Partial indexes for soft-delete patterns: `WHERE deleted_at IS NULL`.

## Test fixtures

- Use testcontainers-go to spin up a real Postgres per test package.
- Migrations are applied up before each test, rolled back after.
- Never share a DB between test packages (parallelism breaks).
- Factory functions live in `database/testutil/factories.go`.

## Audit log specifics

- Append-only. No `UPDATE`, no `DELETE`. Enforce via trigger in WS-08.
- Schema (overview): `id, tenant_id, actor_user_id, action, resource_type,
  resource_id, status, request_id, metadata JSONB, created_at`.
- Audit events are emitted from the service layer, never from repositories.

## Required reading

- `/AGENTS.md`
- `docs/adr/0002-tenancy-model.md`
- `docs/adr/0003-db-tooling.md`
- `docs/adr/0007-shared-postgres.md`
- `docs/architecture/conventions.md#database`
- `.opencode/skills/testing-conventions/SKILL.md`
