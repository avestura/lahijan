# WS-01 · Repo & Tooling Hygiene

```
Status: done
Phase: 0
Depends on: —
Unblocks: WS-02, all subsequent (CI used everywhere)
```

## Goal

Make the repo safe to build on: a CI pipeline that catches regressions, a
Makefile that's the canonical entry point for every dev task, contribution and
security docs that scale beyond solo work, and a clean Go module that
`go build` accepts without surprises on a fresh Linux machine (CI).

## Scope

**In scope:**
- `Makefile` with `build`, `run`, `test`, `test-short`, `bench`, `cover`,
  `vet`, `fmt`, `lint`, `tidy`, `web-*` (no-op stubs), `db-*`, `sqlc`,
  `docker-build`, `dev-up`, `dev-down`, `dev-logs`, `hooks`, `ci-check`,
  `clean`, `docs-install`, `docs`, `help`.
- GitHub Actions workflows: `ci.yml` (lint+test+build+tidy+fmt), `docs.yml`
  (Docusaurus build, skipped until `docs-site/` exists), `migrations.yml`
  (up/down pairing check, skipped until migrations exist).
- Issue templates: `bug_report.yml`, `feature_request.yml`.
- `PULL_REQUEST_TEMPLATE.md` with the workstream-aware checklist.
- `SECURITY.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`.
- `.gitignore` updated for the future frontend and secrets.
- `go.mod` cleaned up (`go mod tidy`); direct deps marked correctly.
- Fix the pre-existing case-sensitivity bug in
  `internal/app/lahijan/conf/computedDefault` import path (would break Linux CI).

**Out of scope:**
- Anything under `docs/` beyond what `WS-02` owns.
- Actual Postgres/Incus/PDNS/SeaweedFS wiring (those are WS-03 / WS-11..13).
- Frontend initialization (WS-18).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0001-module-path.md`

## Deliverables

- [x] `Makefile` with the targets listed above
- [x] `.github/workflows/ci.yml`, `docs.yml`, `migrations.yml`
- [x] `.github/ISSUE_TEMPLATE/{bug_report,feature_request}.yml`
- [x] `.github/PULL_REQUEST_TEMPLATE.md`
- [x] `.github/SECURITY.md`
- [x] `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`
- [x] Updated `.gitignore`
- [x] Clean `go.mod` (direct vs indirect correctly marked)
- [x] Case-sensitivity bug fixed in `program.go` and `conf/pflag.go`

## Definition of Done

- [x] `make build` works on Windows + Linux
- [x] `make test` is green
- [x] `make lint` would be green with `golangci-lint v2` (CI uses the action)
- [x] CI workflow validates `go mod tidy`, `gofumpt`, lint, test, build
- [x] PR/issue templates reference the workstream model
- [x] No `// indirect` on actually-imported packages

## Open questions

- (none)

## Notes

- WS-01 does not introduce Postgres or any other service into compose. The
  `db-*` targets exist so the Makefile is stable, but they're no-ops until
  WS-03 fills in `deployments/docker-compose.dev.yml` and migrations.
- The `docs.yml` and `migrations.yml` workflows self-skip when their target
  directory doesn't exist, so they're safe to land now.
