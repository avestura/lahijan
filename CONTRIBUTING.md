# Contributing to Lahijan

Thanks for your interest in contributing! This document is the short tour; for
the full architecture, conventions, and design records see [`docs/`](./docs).

## Project shape at a glance

- **Backend**: Go 1.23+ monolith under [`internal/app/lahijan`](./internal/app/lahijan).
  Framework: GoFiber v2. Config: Viper. Database: PostgreSQL via sqlc + golang-migrate.
- **Frontend**: React + TypeScript via Vite, split into [`web/`](./web) (dashboard)
  and [`website/`](./website) (marketing). Both are SPAs.
- **Docs**: Markdown under [`docs/`](./docs), rendered with Docusaurus in
  [`docs-site/`](./docs-site).
- **Deploy**: Docker + Compose; everything ships as a single docker-compose stack.

The **heart** of the project — what we're building and why — is in
[`AGENTS.md`](./AGENTS.md). Read that first if you're new here.

## The workstream model

Work is broken into numbered **workstreams** (WS-01 .. WS-30). Each WS lives
in `docs/workstreams/WS-XX-<name>.md` and has:

- A goal paragraph (the WHY)
- An explicit in-scope / out-of-scope list
- A "Required reading" list (ADRs + skills the implementer must load first)
- A Definition of Done checklist

Pick a WS, read its doc top-to-bottom, then check the
[workstream index](./docs/workstreams/README.md) for what's in progress.

## Before you write code

1. **Read `AGENTS.md`.** It captures the non-negotiable pillars.
2. **Read the relevant workstream doc + its "Required reading" list.**
3. **Read the relevant ADRs** in [`docs/adr/`](./docs/adr). If your change
   contradicts an ADR, you need a new ADR first.
4. **Check conventions**: [`docs/architecture/conventions.md`](./docs/architecture/conventions.md).

## Local setup

```bash
# 1. Install Go 1.23+, Node 20+, Docker, golangci-lint v2, sqlc, golang-migrate.
# 2. Fork & clone.
make hooks         # install git hooks (conventional-commit enforcement)
make dev-up        # start Postgres etc. via docker-compose
make tidy
make lint
make test
make run           # build + run with --debug
```

The `Makefile` has a target for almost everything; run `make help` for the list.

## Commit messages

Conventional Commits are **enforced** by a git hook:

```
feat(compute): add instance start endpoint
fix(dns): handle missing zone gracefully
docs(adr): record decision on tenancy model
chore(ci): bump golangci-lint to v2.0.2
```

Scopes match the top-level area: `compute`, `dns`, `s3`, `auth`, `rbac`,
`billing`, `plugins`, `api`, `web`, `website`, `docs`, `ci`, `build`, `conf`.

## Pull requests

- One concern per PR. If you find yourself writing "and also", split it.
- Tick every box in the PR template or mark it N/A with a reason.
- CI runs: golangci-lint, go test, go mod tidy check, gofumpt check, docs build.
- New code needs tests. New DB queries need sqlc coverage. New endpoints need
  OpenAPI spec updates + regenerated clients.
- If you make an architectural decision (even a small one), drop a new ADR
  under `docs/adr/NNNN-*.md`. The template is `docs/adr/0000-template.md`.

## Code style

- Go: `gofumpt` formatting, golangci-lint v2 strict mode. See
  [`.golangci.yml`](./.golangci.yml). Line length max 150.
- Frontend: Prettier + ESLint flat config. 2-space indent.
- SQL: lowercase keywords, snake_case identifiers, one clause per line.
- Markdown: 80-100 char wrap where practical; tables are fine wider.

## Testing expectations

- **Unit tests**: pure-Go logic, in-memory, fast (`go test -short`).
- **Integration tests**: real Postgres via testcontainers-go; mock the
  Incus/PDNS/SeaweedFS APIs with httptest stubs.
- **E2E**: Playwright against a docker-compose stack (added in WS-22).

Every PR must keep `make test` green. New endpoints or services must ship with
at least one happy-path + one failure-path test.

## License

By contributing you agree your contributions are licensed under the
[MIT license](./LICENSE.md).

## Code of Conduct

Participation in this project is governed by the
[Code of Conduct](./CODE_OF_CONDUCT.md). Please be excellent to each other.
