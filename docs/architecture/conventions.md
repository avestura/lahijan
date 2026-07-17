# Conventions

These rules are non-negotiable. They're enforced by lint, CI, and review.

## Go

### Formatting & lint

- **`gofumpt`** formatting. Run `make fmt`.
- **`golangci-lint v2` strict mode**. Run `make lint`. No file-level
  suppressions without a comment explaining why.
- **Max line length 150.**
- **No `init()` for non-trivial logic.** Use explicit `Setup()` functions
  called from `program.Start()`. The existing `program.init()` registers
  computed defaults only — that's the limit.

### Package layout

- One package per directory; directory name = package name.
- Avoid `util`, `common`, `helpers`. Name packages by what they do.
- Internal area packages live under `internal/app/lahijan/<area>/`.

### Error handling

- Wrap at boundaries with context: `fmt.Errorf("compute.instance.create: %w", err)`.
- Never discard with `_ =`. If you must, comment why.
- Sentinels live in `domain/<area>/errors.go` as `errors.Is`-able vars.
- Return `(T, error)` from services/repos; handlers translate to HTTP.

### Logging

- `log/slog` only. No `fmt.Println` in production code.
- The handler is set up once in `program/` with the redact middleware.
- Always carry `request_id` via `slog.With`.
- Never log secrets, tokens, full PII. The redact handler catches common names
  but think before you log.

### Concurrency

- `t.Parallel()` by default in tests.
- No goroutine without a clear lifecycle. Use `context.Context` + `errgroup`.
- No shared mutable state across requests (except startup-time registries).

## Database

See `.opencode/skills/database-conventions/SKILL.md` for the full guide.

- **PostgreSQL 16+.**
- **Every tenant-scoped table has** `id UUID PK`, `tenant_id UUID NOT NULL`,
  `created_at`, `updated_at`.
- **Global tables (no tenant_id):** `tenants`, `users`, `roles`, `permissions`,
  `audit_log` (nullable), `schema_migrations`, `river_*`.
- **sqlc** for queries. **golang-migrate** for migrations. **pgx v5** driver.
- **Migrations paired up + down, reversible.** CI verifies.
- **Never edit a merged migration.** New migration = new file.
- **No ORM.** No auto-migrate. No `db.Query` outside repos.

## HTTP / API

- All public routes under `/api/v1/`.
- **OpenAPI 3.1 spec** (`api/openapi.yaml`) is the source of truth. Clients
  are generated.
- **Error envelope** for every error response:
  ```json
  { "error": { "code": "string", "message": "string", "details": {...} } }
  ```
- **Handlers thin.** Parse → call service → render. No business logic.
- **Middleware order:** `requestid → recover → cors → logger → tenant → auth →
  audit → rbac → handler`.
- **Per-request state** via `c.Locals`: `request_id`, `user_id`, `tenant_id`.

## Security

- **Never log secrets, tokens, full PII.**
- **Every privileged action calls `RequirePerm("scope.action")`.**
- **Every state-changing privileged action emits an audit event** before the
  side effect; updates it after.
- **Tenant scoping enforced at the repository layer.** Middleware sets the
  tenant context; queries filter automatically.
- **Secrets at rest:** AES-GCM encrypted DB columns for MVP; external Vault is
  a future option.

## Frontend

See `.opencode/skills/frontend-foundations/SKILL.md` for the full guide.

- **TypeScript strict.** `strict: true`, `noUncheckedIndexedAccess: true`.
- **All user-facing strings through `t()`** from `react-i18next`. ESLint rule
  blocks string literals in JSX children.
- **shadcn/ui + Tailwind.** Don't hand-edit generated primitives.
- **TanStack Query for server state.** Zustand for UI-only state.
- **react-hook-form + zod** for every form.
- **Generated API client** in `lib/api`. No raw `fetch`.
- **Logical Tailwind properties** (`ms-*`, `me-*`, `ps-*`, `pe-*`) — RTL-safe.
- **`usePerm("scope.action")` gates every privileged UI trigger.**

## Testing

See `.opencode/skills/testing-conventions/SKILL.md` for the full guide.

- **Tests next to source.** `foo.go` → `foo_test.go`.
- **`t.Parallel()` by default.**
- **Table-driven** for ≥3 cases.
- **testcontainers-go** for real-PG integration tests; provider backends via
  httptest fakes.
- **No `time.Sleep`.** Use `require.Eventually` or channels.
- **E2E** via Playwright (WS-22).

## Commits

- **Conventional Commits** enforced by git hook: `type(scope): description`.
- Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`,
  `ci`, `chore`, `revert`.
- Scopes: `compute`, `dns`, `s3`, `auth`, `rbac`, `billing`, `plugins`, `api`,
  `web`, `website`, `docs`, `ci`, `build`, `conf`.

## Frontend of the docs

- Docusaurus 3 source in `docs-site/`; markdown content in `docs/`.
- Use MDX, admonitions, tabs.
- Don't duplicate content; Docusaurus renders markdown as-is.

## What NOT to do

- Don't introduce a new dependency without checking license + pattern fit. If
  unsure, write an ADR.
- Don't add microservices. Don't add Redis. Don't add a message queue beyond
  River. Don't add k8s manifests (yet).
- Don't expose Incus/PowerDNS/SeaweedFS by name in user-facing API or UI.
- Don't auto-migrate the DB. Don't skip migrations in CI.
- Don't suppress lint rules file-by-file without a comment.
- Don't write English-only strings in the frontend or in user-facing backend
  messages.
- Don't bypass the repository layer to query the DB directly.
- Don't put business logic in handlers.
- Don't catch panics silently in handlers; let `recover` middleware handle it.
