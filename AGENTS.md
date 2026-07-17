# AGENTS.md — Lahijan Cloud Platform

> **Read this first.** This file is the canonical "heart" of the project. Every
> AI session that touches Lahijan loads this file. Keep it concise, current, and
> load-bearing. If something here is wrong, **everything downstream is wrong**.

## What is Lahijan?

Lahijan is an **open-source cloud platform** that gives end users a self-service
portal for **compute, DNS, and object storage** while hiding the operator
complexity behind three best-of-breed backends:

| Capability | Backend | Lahijan's role |
|------------|---------|----------------|
| **Compute** (system containers + VMs) | [Incus](https://linuxcontainers.org/incus/) | Wraps the full Incus REST API; maps tenants → Incus projects. |
| **DNS** (authoritative zone serving) | [PowerDNS Authoritative](https://github.com/PowerDNS/pdns) | Wraps PowerDNS' HTTP API; maps tenants → zones. |
| **Object storage** (S3-compatible) | [SeaweedFS](https://github.com/seaweedfs/seaweedfs) | Mints per-user S3 credentials; manages buckets; users hit SeaweedFS directly for data. |

End users provision infrastructure through Lahijan's REST API or web dashboard
without ever knowing Incus/PowerDNS/SeaweedFS exist. Operators deploy a single
Docker Compose stack and Lahijan manages the rest.

## The 12 non-negotiable pillars

These are settled decisions. Any change requires a new ADR superseding the
original (see `docs/adr/`).

1. **Transparent infrastructure** — Incus, PowerDNS, SeaweedFS are never
   exposed to end users by name. They talk to "Lahijan."
2. **Multi-tenant, row-level isolation** — one PostgreSQL database, `tenant_id`
   on every row, scoped at the repository layer. No shared data between tenants.
3. **Full Incus surface to power users** — instances, profiles, devices,
   networks, projects, storage pools are all exposed. No "dumbed-down" model.
4. **Direct S3 + Lahijan control plane** — users get their own S3 credentials
   and hit SeaweedFS directly for data; bucket & policy management goes through
   Lahijan.
5. **WASM plugin system with Android-style permissions** — plugins declare a
   manifest; admins approve each permission at install time. Plugins are
   sandboxed via wazero. **This is in MVP scope.**
6. **PAYG + prepaid billing (ledger-only for MVP)** — admin tops up user
   balances; usage is metered per-minute and debited. No payment gateway in MVP.
7. **RBAC + append-only audit log** — every privileged action emits an audit
   event. Audit rows cannot be updated or deleted.
8. **Monolithic Go backend** — one process, one binary, no microservices.
   Deploy is Docker Compose. Architecture must still admit future multi-node.
9. **React + TypeScript SPA frontend** — Vite for both the dashboard (`web/`)
   and marketing site (`website/`); shadcn/ui + Tailwind.
10. **PostgreSQL via sqlc + golang-migrate** — type-safe codegen from SQL;
    hand-written migrations. No ORM auto-migration, ever.
11. **Full OpenTelemetry observability** — structured logs (slog), metrics,
    traces. OTLP exporter to a collector in the compose stack.
12. **Full i18n from day one** — go-i18n on the backend, react-i18next on the
    frontend, RTL support. English is the default; Persian is the second locale.

## Tech stack (locked)

- **Language (backend)**: Go 1.23+
- **Language (frontend)**: TypeScript (strict)
- **HTTP framework**: GoFiber v2
- **Config**: Viper (env + YAML + CLI flags auto-generated from default YAML)
- **Database**: PostgreSQL 16+; access via **sqlc** + **pgx**; migrations via
  **golang-migrate**
- **Job queue**: **River** (PostgreSQL-native; no Redis)
- **WASM runtime**: **wazero** (pure Go, no CGO)
- **Auth**: argon2id, OAuth, OIDC, SAML 2.0, TOTP, WebAuthn
- **Observability**: log/slog + OpenTelemetry SDK + OTLP collector
- **Frontend**: Vite + React 18 + TanStack Router/Query + Zustand + shadcn/ui +
  Tailwind + react-i18next
- **Docs**: Docusaurus 3 (source: `docs/`; app: `docs-site/`)
- **Deploy**: Docker + Compose; one stack brings up Lahijan + Postgres + Incus
  client + PowerDNS + SeaweedFS + OTEL collector

## Repo layout (the parts that matter)

```
cmd/lahijan/                  # binary entrypoint (thin)
internal/app/lahijan/         # all backend code lives under here
  conf/                       # config subsystem (Viper + auto-gen pflags)
  program/                    # bootstrap, fiber wiring
  version/                    # build-time version stamp
  database/                   # (WS-03) sqlc queries, migrations, repositories
  api/                        # (WS-05) HTTP handlers, middleware, routers
  domain/                     # (WS-03+) per-area domain logic
  auth/                       # (WS-06+) auth, RBAC, audit
  providers/                  # (WS-11..13) Incus, PowerDNS, SeaweedFS drivers
  compute/, dns/, storage/,   # (WS-14..17) user-facing modules
    billing/, plugins/
  jobs/                       # (WS-09) River integration
  wasm/                       # (WS-10a..c) wazero runtime, permissions, host funcs
  i18n/                       # message bundles (en, fa)
  observability/              # OTel + slog wiring
docs/                         # all engineering markdown (ADRs, workstreams, ...)
docs-site/                    # Docusaurus app that renders docs/
web/                          # dashboard SPA (WS-18+)
website/                      # marketing site SPA (WS-19+)
deployments/                  # docker-compose files (dev, prod)
scripts/                      # operator install / migration helper scripts
test/                         # integration / e2e tests that need their own app
```

Per-area `AGENTS.md` files exist under `internal/app/lahijan/`, `web/`, `docs/`,
and `deployments/`. **Load them when working in those areas.**

## How work is organized

Work is broken into **34 workstreams** (WS-01 .. WS-30) across **8 phases**.
The full index with status is at `docs/workstreams/README.md`. Phases denote
dependency order, not strict serialization.

- **Phase 0** — Foundations (WS-01..WS-05)
- **Phase 1** — Security & Identity (WS-06, WS-07a/b/c, WS-08)
- **Phase 2** — Platform Services (WS-09, WS-10a/b/c)
- **Phase 3** — Infrastructure Providers (WS-11, WS-12, WS-13)
- **Phase 4** — User-Facing Modules (WS-14..WS-17)
- **Phase 5** — Frontend (WS-18..WS-21)
- **Phase 6** — Quality & Release (WS-22, WS-23)
- **Phase 7** — Deferred Backlog (WS-24..WS-30, docs ready; not in MVP)

## How to start working on a workstream (the AI session checklist)

When a fresh AI session is asked to implement WS-XX, the **`ws-implementer`**
subagent enforces this order:

1. Read this file (`AGENTS.md`) end to end.
2. Read `docs/workstreams/WS-XX-<name>.md` end to end.
3. Read every file under that WS doc's **"Required reading"** list.
4. Read `docs/architecture/conventions.md` — especially the section for your
   area.
5. Read the relevant `.opencode/skills/<area>/SKILL.md`.
6. Create a feature branch: `feat/ws-XX-<short>`.
7. Implement one concern at a time, with tests, before moving on.
8. Run `make lint test` after every meaningful change.
9. When done: update the WS doc's status, write any new ADRs, regenerate
   clients/specs, open a PR with the checklist ticked.

The `adding-a-workstream` skill (`/skills/adding-a-workstream/SKILL.md`) has
the full checklist and template.

## Conventions you must follow

These are non-negotiable. Details live in
[`docs/architecture/conventions.md`](./docs/architecture/conventions.md);

### Go

- `gofumpt` formatting (max 150 char line). `golangci-lint v2` strict mode.
- Errors are wrapped with `fmt.Errorf("verb: %w", err)` at boundaries; never
  discarded except via explicit `_ :=`.
- Public functions get doc comments starting with the identifier name.
- Tests live next to the code (`foo_test.go`); use `t.Parallel()` by default.
- No `init()` for non-trivial logic. Use explicit `Setup()` functions.

### Database

- **Every table has** `tenant_id UUID NOT NULL` (except global tables like
  `tenants`, `users`, `audit_log` — see glossary for exceptions).
- **Every query** goes through sqlc; never raw `db.Query` in handlers/services.
- Migrations are paired (`NNNN_name.up.sql` + `NNNN_name.down.sql`) and
  **reversible**. CI verifies this.
- No ORM. No auto-migrate. Hand-write SQL.

### HTTP / API

- All public routes under `/api/v1/`.
- Every response uses the standard error envelope (`{error: {code, message,
  details}}`).
- Handlers thin; business logic in services; persistence in repositories.
- OpenAPI 3.1 spec is the source of truth; clients (Go + TS) are generated.

### Frontend

- All UI text goes through `t()` from react-i18next. **No hardcoded English.**
- Components are shadcn/ui-based; Tailwind utility classes for layout; CSS vars
  for theme.
- Server state via TanStack Query; client state via Zustand.
- Forms via react-hook-form + zod schemas shared with the OpenAPI types.

### Security

- Never log secrets, tokens, or PII. Use the `redact` slog handler.
- Every privileged action calls `RequirePerm("scope.action")`.
- Every state-changing privileged action emits an audit event **before** the
  side effect and updates it with the result.
- Tenant scoping is enforced at the repository layer; middleware sets the
  tenant context, queries filter automatically.

### Commits

- Conventional Commits enforced by git hook: `feat(scope): desc`.
- Scopes: `compute`, `dns`, `s3`, `auth`, `rbac`, `billing`, `plugins`, `api`,
  `web`, `website`, `docs`, `ci`, `build`, `conf`.

## What NOT to do

- Don't introduce a new dependency without checking it's permissively licensed
  and matches our patterns. If unsure, write an ADR.
- Don't add microservices. Don't add a message queue beyond River. Don't add
  Redis. Don't add k8s manifests (yet).
- Don't expose Incus/PowerDNS/SeaweedFS by name in user-facing API or UI. They
  are "compute", "dns", "object storage" to users.
- Don't auto-migrate the DB. Don't skip migrations in CI.
- Don't suppress lint rules file-by-file without a comment explaining why.
- Don't write English-only strings in the frontend or in user-facing messages.

## Pointers

- **Architecture details**: `docs/architecture/overview.md`,
  `docs/architecture/conventions.md`
- **All decisions**: `docs/adr/` (start with `docs/adr/README.md`)
- **Workstream index**: `docs/workstreams/README.md`
- **Glossary**: `docs/glossary.md`
- **Project-specific skills**: `.opencode/skills/`
- **WS implementer agent**: `.opencode/agent/ws-implementer.md`
- **Config reference**: `internal/app/lahijan/conf/.lahijan.conf.default.yaml`
