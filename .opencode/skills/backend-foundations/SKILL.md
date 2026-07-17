---
name: backend-foundations
description: "Use when writing Go code in this repo: package layout, layering (handlers→services→repos), error wrapping, slog logging, Fiber middleware patterns, config via Viper, dependency choices. Triggers on Go file edits, new packages under internal/app/lahijan, fiber handlers, repository code, service code."
---

# Backend Foundations — Go conventions for Lahijan

Load this whenever you write or review Go in `internal/app/lahijan/`.

## Layered architecture (enforced)

```
cmd/lahijan/main.go             # thin: call program.Start()
program/                        # bootstrap, fiber wiring
api/                            # handlers (HTTP) — thin
domain/<area>/                  # services (business logic)
database/                       # repositories (sqlc-generated + thin wrappers)
providers/<incus|powerdns|seaweedfs>/   # backend drivers
```

**Hard rule:** a handler must not call a repository directly. It goes through a
service. A service must not call `http.*`. A repository must not contain
business logic. A provider must not know about our DB.

## Package naming

- Short, lowercase, no underscores, no `-go` suffix.
- One package per directory; directory name = package name.
- Avoid `util`, `common`, `helpers`. Name by what the package *does*, not by
  what it *is made of*.
- `internal/app/lahijan/<area>` is the canonical home for an area's services.

## Error handling

- Wrap at boundaries with context: `fmt.Errorf("compute.instance.create: %w", err)`.
- Never swallow with `_ =`. If you genuinely must discard, comment why.
- Sentinel errors live in `domain/<area>/errors.go` as `errors.Is`-able vars.
- Return `(T, error)` from services/repos; handlers translate to HTTP via the
  error envelope helper in `api/errors.go`.
- Use `errors.Join` only when you genuinely have multiple unrelated errors.

## Logging

- Use `log/slog` only. No `fmt.Println`, no `log.Printf` in production code.
- The handler is set up once in `program/` with the redact middleware.
- Always include the request ID: `slog.With("request_id", rid)`.
- Levels: `Debug` (dev), `Info` (lifecycle), `Warn` (degraded), `Error` (failed
  operation but process continues), `Error`+return (fatal).
- **Never log secrets, tokens, passwords, full PII.** The redact handler
  catches common field names but you must also think before you log.

## Config

- Read via `conf.Get<Thing>()` helpers. **Never** call `viper.Get` directly
  outside the `conf` package.
- Add new config keys by editing the embedded `.lahijan.conf.default.yaml` —
  the pflag generator picks them up automatically.
- Go-computed defaults register via `computeddefault.Register*Default` in
  `program.init()` or an area-specific `Setup()` function.

## Fiber patterns

- Group routers per area: `api.ComputeGroup`, `api.DNSGroup`, etc.
- Every handler signature: `func(c *fiber.Ctx) error`.
- Every handler returns the standard error envelope via `api.SendError(c, ...)`.
- Middleware order matters: `requestid → recover → cors → logger → tenant →
  auth → audit → rbac`.
- Use `c.Locals` for per-request values (`tenant_id`, `user_id`, `request_id`).

## Dependency choices

Before adding any new module to `go.mod`:

1. License: must be MIT/Apache-2/BSD/ISC. GPL/AGPL is a hard no.
2. Maintenance: last commit within 6 months, or known-stable (stdlib-like).
3. Pattern fit: does it match how the rest of the codebase works? (e.g. we use
   GoFiber v2 not Echo/Chi/Gin; pgx not lib/pq; sqlc not gorm.)
4. If the dependency is large or strategic, write an ADR.

## What NOT to do

- No `init()` for non-trivial logic. Use explicit `Setup()` funcs called from
  `program.Start()`.
- No global mutable state except registries (`computeddefault`, route tables).
- No `panic()` in handlers or services. Recover only at the Fiber boundary.
- No goroutines without a clear lifecycle (use `context.Context` + errgroup).
- No `time.Sleep` in tests; use channels or `eventually`.

## Required reading

- `/AGENTS.md`
- `docs/architecture/conventions.md#go`
- `docs/adr/0001-module-path.md`
- `docs/adr/0010-monolithic-backend.md`
