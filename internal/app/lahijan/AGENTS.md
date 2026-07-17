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
| `database/` | sqlc queries, golang-migrate migrations, repository types. | WS-03 |
| `api/` | HTTP handlers, Fiber routers, middleware, error envelope, OpenAPI. | WS-05 |
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
  `audit_log`, `schema_migrations`, River's `river_*`). See glossary.
- Every new HTTP handler ships under `/api/v1/` and uses the error envelope.
- Every new privileged action calls `RequirePerm` and emits an audit event.
- Every new file uses `gofumpt` formatting and passes `golangci-lint` v2 strict.
- Every new external API the backend talks to lands as a `providers/*` driver,
  never inline in a service.
