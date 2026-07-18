# ADR-0022: River migrations bundled into golang-migrate

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

River (ADR-0008) ships its own schema migrations under
`riverpgxv5/migration/main/001..007.sql`. River's own `rivermigrate`
framework applies each upstream migration in its own transaction, which
lets a freshly-added enum value (`ALTER TYPE ... ADD VALUE 'pending'`)
land before any subsequent CHECK-constraint change tries to use it.

Lahijan uses **golang-migrate** (ADR-0003) with paired `NNNN_*.up.sql` +
`NNNN_*.down.sql` files applied inside its own transactions. golang-migrate
does not have a built-in way to split a single migration file across
multiple transactions.

The naive approach — copy all seven River up migrations into ONE bundled
`NNNN_river_schema.up.sql` — fails on a fresh Postgres database with:

```
pq: unsafe use of new value "pending" of enum type river_job_state
```

because the `ALTER TYPE ... ADD VALUE` and the subsequent
`ALTER TABLE ... ADD CONSTRAINT` (which references the `state` column
that now has the new value) end up in the same transaction.

Options considered:

- **Option A — bundle and pray.** Concatenate all seven River up
  migrations into one file. Fails on Postgres 12+ as above.
- **Option B — one Lahijan migration per River upstream migration.**
  Copy each of the seven River files verbatim as `0018_river_001` ...
  `0024_river_007`. Pro: clean 1:1 mapping. Con: 7 new migration files in
  our dir for what is logically one concern; the count will grow with
  every River upgrade.
- **Option C — bundle, but split at the enum boundary.** Concatenate
  River's 001..003 + the non-enum-affecting parts of 004 + the
  `ALTER TYPE ADD VALUE` (as the LAST statement) into ONE migration,
  then put the constraint change + 005/006/007 + the
  `INSERT INTO river_migration` markers into a SECOND migration. Each
  migration is its own transaction, so the enum value commits before the
  constraint change uses it.
- **Option D — use River's `rivermigrate` at bootstrap.** Skip
  golang-migrate for River's tables entirely; let `rivermigrate.Migrator`
  apply River's migrations from its embedded FS. Pro: no SQL duplication.
  Con: violates "golang-migrate is the only migration path" (ADR-0003),
  complicates the reversibility test, and forces every test that boots
  the DB to also run the River migrator.

## Decision

Adopt **Option C**: bundle River's upstream migrations into TWO Lahijan
migrations, split at the enum boundary.

- `0018_river_schema_base.{up,down}.sql` — upstream 001, 002, 003, the
  non-enum parts of 004, and `ALTER TYPE river_job_state ADD VALUE
  IF NOT EXISTS 'pending'` as the LAST statement.
- `0019_river_schema_rest.{up,down}.sql` — the constraint change from
  004, upstream 005 (rebuilt `river_migration` + `unique_key`), 006
  (bulk unique), 007 (notification outbox + cleanup), and the marker
  `INSERT INTO river_migration` rows that mark every upstream version
  applied so a future `river migrate-up` from the CLI is idempotent.

Both migrations are reversible; the down for 0018 drops the river_job_state
TYPE entirely (the 'pending' value cannot be removed individually per
Postgres) along with every table that references it.

## Consequences

- **Positive:** two new files instead of seven; the Lahijan migration
  dir stays readable.
- **Positive:** golang-migrate remains the sole migration path; no
  rivermigrate dependency at bootstrap.
- **Positive:** The bundled approach scales — upgrading River (>= v0.41)
  means adding a small delta migration (`0023_river_schema_008` or
  similar) that applies only the new upstream SQL and INSERTs the new
  version row.
- **Negative:** The split point is non-obvious; a reader who joins the
  project may not immediately see why `ALTER TYPE ADD VALUE` lives at
  the bottom of 0018. The file's header comment explains it.
- **Negative:** We copy River's SQL verbatim, so a typo risk exists.
  Acceptable because the bundled migrations run against a fresh
  Postgres in every integration test (`migrations_integration_test.go`)
  and any divergence from River's expected schema shows up immediately.

## Compliance

- `internal/app/lahijan/database/migrations/0018_river_schema_base.up.sql`
  exists and ends with `ALTER TYPE river_job_state ADD VALUE IF NOT
  EXISTS 'pending' AFTER 'discarded';`.
- `internal/app/lahijan/database/migrations/0019_river_schema_rest.up.sql`
  exists and includes the
  `INSERT INTO river_migration (line, version) VALUES ('main', 1..7)
  ON CONFLICT DO NOTHING;` block.
- `internal/app/lahijan/database/migrations_integration_test.go` asserts
  the migration version is at least 19 (so future River upgrades must
  bump this floor).
- A future River upgrade (>= v0.41) MUST add a new Lahijan migration
  (NOT modify 0018 or 0019) that applies the delta.

## References

- ADR-0003 (sqlc + golang-migrate)
- ADR-0008 (River as the job queue)
- [River: bring your own migrations](https://riverqueue.com/docs/migrations)
- WS-09 (job system)
