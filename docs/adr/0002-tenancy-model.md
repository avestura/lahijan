# ADR-0002: Multi-tenant, row-level isolation

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan is intended to be deployable both as a self-hosted single-org platform
and as a SaaS-style multi-tenant platform. The tenancy model must be chosen
before any schema is written, because retrofitting is expensive.

Options considered:

- **Single-org (one big shared space)** — simplest schema; no `tenant_id`
  needed; but forces a from-scratch migration to support multi-tenant.
- **Multi-tenant, row-level isolation** — single DB, single schema, `tenant_id`
  on every row, scoped at the repository layer. Scales well; cheap to operate.
- **Multi-tenant, schema-per-tenant** — stronger isolation; per-tenant
  migrations; high operational overhead at scale.
- **Multi-tenant, DB-per-tenant** — strongest isolation; expensive; complex
  cross-tenant queries.

## Decision

Lahijan uses **multi-tenant, row-level isolation** in a single PostgreSQL
database and schema. Every tenant-scoped table has `tenant_id UUID NOT NULL`;
the repository layer enforces scoping; the API middleware sets the tenant
context from the authenticated session.

## Consequences

- **Positive:** one set of migrations, one DB to back up, one connection pool.
- **Positive:** cross-tenant admin queries (system-wide audit, billing) are easy.
- **Positive:** SaaS, self-hosted single-org, and on-prem all use the same code path.
- **Negative:** a single bug in a query can leak data across tenants. Mitigated
  by repository-layer enforcement + integration tests that specifically try to
  cross-read.
- **Negative:** per-tenant backup/restore is harder. Acceptable for MVP.

## Compliance

- Every new table has `tenant_id UUID NOT NULL REFERENCES tenants(id)` (except
  the global tables listed in `docs/glossary.md`).
- Every sqlc query that reads tenant-scoped data filters by `tenant_id`.
- The repository layer (`database/<area>_repo.go`) extracts tenant from context
  and bakes it into every query.

## References

- ADR-0003 (DB tooling — sqlc + golang-migrate)
- `docs/glossary.md` (global vs tenant-scoped tables)
- `.opencode/skills/database-conventions/SKILL.md`
