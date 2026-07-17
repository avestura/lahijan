# ADR-0003: sqlc + golang-migrate on PostgreSQL

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

We need to choose a database access layer and migration tool for PostgreSQL.

Options considered:

- **GORM + auto-migrate** — fast to write; hides SQL; schema drift; weak
  control over indexes, partial indexes, JSONB constraints.
- **ent + atlas** — modern; schema-as-code; steep learning curve; new to most
  contributors.
- **pgx + squirrel + golang-migrate** — full control; lots of boilerplate;
  stringly-typed queries.
- **sqlc + golang-migrate** — write SQL; sqlc generates type-safe Go; migrations
  are explicit, reversible SQL files; idiomatic Go; small surface area.

## Decision

Use **sqlc** for query codegen and **golang-migrate** for migrations, on top
of **pgx v5** as the underlying driver. Hand-write all SQL migrations.

No ORM. No auto-migration. No `db.Query` outside the repository layer.

## Consequences

- **Positive:** SQL is the source of truth — easy to review, easy to optimize.
- **Positive:** type-safe Go generated from SQL; IDE autocomplete works.
- **Positive:** migrations are explicit and reversible; CI verifies up + down.
- **Positive:** schema changes are visible in PRs (no magic).
- **Negative:** every schema change requires a migration file + sqlc regen.
- **Negative:** dynamic queries (e.g. user-controlled ORDER BY) need careful
  handling; sqlc doesn't generate them.

## Compliance

- `internal/app/lahijan/database/sqlc.yaml` exists and configures codegen.
- `internal/app/lahijan/database/migrations/` contains all migrations, paired
  up + down.
- `internal/app/lahijan/database/queries/<area>.sql` contains all sqlc queries.
- `internal/app/lahijan/database/gen/<area>/` holds generated code; never hand-edited.
- `make sqlc` and `make db-up` / `make db-down` work.

## References

- [sqlc documentation](https://docs.sqlc.dev/)
- [golang-migrate documentation](https://github.com/golang-migrate/migrate)
- `.opencode/skills/database-conventions/SKILL.md`
- ADR-0002 (tenancy — drives the `tenant_id` rule)
