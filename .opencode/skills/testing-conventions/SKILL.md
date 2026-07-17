---
name: testing-conventions
description: "Use when writing or reviewing tests for Lahijan. Triggers on *_test.go files, Playwright specs, testutil helpers, mocks of Incus/PowerDNS/SeaweedFS, testcontainers-go setup, table-driven tests, fixtures, fake servers."
---

# Testing Conventions — Lahijan

Load this whenever you write or review tests.

## Test pyramid

```
                  ▲
                  │       e2e (Playwright)              — slow, full stack
                  │     ───────────────────
                  │   integration (testcontainers-go)   — real PG + provider fakes
                  │  ──────────────────────────────
                  │ unit (go test / Vitest)              — fast, in-memory
                  ▼
```

- **Unit** tests: pure logic, in-memory, run in <1s. Use `t.Parallel()`.
- **Integration** tests: real Postgres (testcontainers-go), provider backends
  stubbed via httptest. Gated behind `//go:build integration`.
- **E2E** tests: Playwright against a docker-compose stack. Live under `test/e2e/`.

CI runs unit on every PR, integration on `main` + labeled PRs, e2e nightly.

## Go test rules

- File: `foo_test.go` next to `foo.go`.
- Function: `Test<Thing>_<Condition>` (e.g. `TestCreateInstance_ZeroBalance_Rejects`).
- Always `t.Parallel()` unless the test mutates shared state.
- Table-driven when there are ≥3 cases.
- Use `testify/require` for fatal assertions, `testify/assert` for non-fatal.
- Timeouts: every test that hits a DB or HTTP sets a `context.WithTimeout`.
- No `time.Sleep` — use `require.Eventually` or channels.

## Integration test setup

- Spin up Postgres via testcontainers-go in `TestMain`.
- Run all migrations up at the start, down at the end.
- Each test gets a fresh tenant + user via the factory in
  `database/testutil/factories.go`.
- Each test gets a fresh tx where possible (`BEGIN` → test → `ROLLBACK`).
- Provider backends are stubbed with `httptest.NewServer` + a typed fake:
  - `providers/incus/fake/server.go` → fake Incus REST API
  - `providers/powerdns/fake/server.go` → fake PDNS API
  - `providers/seaweedfs/fake/server.go` → fake S3 API

## Fixtures and factories

- Builders, not fixtures: `testutil.NewInstance(t).WithStatus("running").Build()`.
- Factories live in `database/testutil/factories.go` (DB-backed) and
  `domain/<area>/testutil/factories.go` (in-memory).
- Factory defaults must be valid; you only override what matters for the test.

## Mocking rules

- Generate mocks with `mockery` for service-to-service boundaries.
- Mock at the **interface** boundary, not the concrete type.
- Don't mock the database. Use a real one via testcontainers.
- Don't mock `time.Now`; inject a `Clock` interface.

## Frontend tests

- Vitest + React Testing Library.
- File: `Foo.tsx` → `Foo.test.tsx` (next to source).
- Mock TanStack Query via `@testing-library/react`'s `QueryClientProvider` with
  `defaultOptions: { queries: { retry: false } }`.
- Don't mock components you don't own; mock at the API client boundary.

## E2E (Playwright)

- Live in `test/e2e/<area>.spec.ts`.
- Spin up `docker-compose.test.yml` once per CI run, not per test.
- Each test creates its own tenant/user; cleans up after.
- Use `data-testid` attributes for selectors, not text (text breaks with i18n).

## Coverage

- No hard threshold for MVP (would encourage low-value tests). Reviewed per PR.
- A PR that adds user-facing logic must add at least one happy + one failure
  test for that logic.
- Coverage report: `make cover` → prints `total: (statements) X%`.

## What NOT to do

- No `t.Skip()` without a TODO and a tracking issue.
- No catching panics in tests; let them fail loudly.
- No test depends on another test's side effects (parallel-safe only).
- No real network calls in unit tests.
- No real Incus/PDNS/SeaweedFS in CI integration tests; only fakes.

## Required reading

- `/AGENTS.md`
- `docs/adr/0003-db-tooling.md`
- `docs/workstreams/WS-22-sandbox-integration-test-harness.md`
- `docs/architecture/conventions.md#testing`
