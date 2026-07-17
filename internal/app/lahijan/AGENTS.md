# AGENTS.md — Backend (`internal/app/lahijan/`)

> Load this **in addition to** the root `/AGENTS.md` whenever you touch Go code.

## Package map

| Path | Purpose | Status |
|------|---------|--------|
| `cmd/lahijan/` | Binary entrypoint. Stays thin: parse flags, call `program.Start()`. | ✅ done |
| `program/` | Process bootstrap. Wires Fiber, middleware, config; calls `app.Listen`. | ✅ minimal |
| `conf/` | Viper-based config; auto-generates pflags from the embedded default YAML. | ✅ done |
| `conf/computeddefault/` | Registry for Go-computed defaults (YAML sentinel `$go-computed`). | ✅ done |
| `version/` | Build-time version stamp via `-ldflags`. | ✅ done |
| `database/` | sqlc queries, golang-migrate migrations, repository types. | ✅ WS-03 |
| `api/` | HTTP handlers, Fiber routers, middleware, error envelope, OpenAPI. | ✅ WS-05 |
| `domain/<area>/` | Per-area domain logic (compute, dns, storage, billing, plugins, ...). | WS-14..17 |
| `auth/` | Sessions, tokens, OAuth/OIDC/SAML, MFA, RBAC, audit. | WS-06..08 |
| `providers/incus/`, `providers/powerdns/`, `providers/seaweedfs/` | Backend drivers. | WS-11..13 |
| `jobs/` | River integration; job registry; worker supervisor. | WS-09 |
| `wasm/` | wazero runtime, permission manifests, host functions, plugin loading. | WS-10a..c |
| `i18n/` | go-i18n message bundles (`en.json`, `fa.json`). | WS-06 (init), all user-facing WSs |
| `observability/` | slog handlers, OTel setup, redact middleware. | WS-04 |

## Layered architecture (target)

```
cmd → program → api (handlers) → domain (services) → database (repos) → pg
                  ↑                                  ↑
                  └─ providers/* (Incus, PDNS, S3)   └─ sqlc generated code
                  └─ jobs (River), wasm runtime
```

- **Handlers** in `api/` are thin: parse request, call service, render response.
  No business logic. No direct DB access.
- **Services** in `domain/<area>/` contain business logic and orchestrate
  providers + repos. **All privileged actions call `RequirePerm` and emit
  audit events.**
- **Repositories** in `database/` are sqlc-generated types plus thin wrappers.
  They enforce `tenant_id` scoping. **No service should ever bypass this layer.**
- **Providers** in `providers/` are pure drivers that translate between
  Lahijan's domain types and the backend APIs. They never touch our DB.

## The Provider interface (added in WS-11)

Every infrastructure backend implements:

```go
type Provider interface {
    Name() string                                  // "incus" / "powerdns" / "seaweedfs"
    Ping(ctx context.Context) error                // health probe
    Capabilities() Capabilities                    // feature flags
}
```

Each concrete provider (Incus, PowerDNS, SeaweedFS) extends with area-specific
methods. **Provider calls never carry `tenant_id`** — that mapping happens in
the service layer.

## Required reading before adding to the backend

- `/AGENTS.md` — project heart
- [`docs/architecture/conventions.md`](../../docs/architecture/conventions.md) — Go + DB + HTTP + security rules
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`
- `.opencode/skills/testing-conventions/SKILL.md`
- The relevant ADRs in `docs/adr/`

## Local dev

```bash
make dev-up      # start Postgres via docker-compose.dev.yml
make db-up       # apply migrations
make sqlc        # regenerate query types
make run         # build + run with --debug
make lint test   # always before pushing
```

## Hard rules

- Every new table has `tenant_id UUID NOT NULL` (exceptions: `tenants`, `users`,
  `roles`, `permissions`, `audit_log`, `schema_migrations`, River's `river_*`). See glossary.
- Every new HTTP handler ships under `/api/v1/` and uses the error envelope.
- Every new privileged action calls `RequirePerm` and emits an audit event.
- Every new file uses `gofumpt` formatting and passes `golangci-lint` v2 strict.
- Every new external API the backend talks to lands as a `providers/*` driver,
  never inline in a service.

## Database layer (WS-03)

```
database/
  sqlc.yaml            # sqlc v2 config; schema glob is migrations/*.up.sql
  migrations/          # golang-migrate, paired up + down, reversible
  queries/<area>.sql   # one query file per area
  gen/                 # sqlc output; single package; NEVER hand-edit
  pool.go              # *pgxpool.Pool from conf (database.*)
  tenant.go            # WithTenant / TenantFromContext (the scoping seam)
  <area>_repo.go       # typed repository wrappers; the ONLY service entry point
  testutil/            # testcontainers harness + factories (build tag: integration)
```

- **Services reach Postgres ONLY through `*database.Repos`** (built by
  `database.NewRepos(pool)`). Never import `gen` outside this package.
- **Tenant scoping is enforced at the repository seam.** Tenant-scoped repos
  (e.g. `MembershipsRepository`) take a `context.Context` and pull the tenant id
  via `database.TenantFromContext`; callers cannot pass a tenant id directly, so
  a missing tenant context fails closed with `ErrNoTenantInContext`.
- **`gen/` is one package**, not per-area. Per-area organization lives in the
  `queries/` source files and the `<area>_repo.go` wrappers. Regenerate with
  `make sqlc`; the committed output must not change on a re-run.
- **`program.Start` does not open the pool yet** — no handler needs the DB
  until WS-04/WS-05. Wire `database.NewPool(ctx)` + `database.NewRepos(pool)`
  into bootstrap once the first consumer lands; do not block `make run` on
  Postgres before then.
- **Integration tests** (`//go:build integration`) share one testcontainers
  Postgres per package via `testutil.Setup(m)` in `TestMain`.
