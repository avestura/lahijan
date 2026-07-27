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

## Fake vs real — what the in-process fakes do NOT catch

The provider fakes (`providers/{incus,powerdns,seaweedfs}/fake/`) implement
the happy-path REST surface so unit + integration tests can exercise the
service layer without a daemon. They are essential for regression coverage
and they are **not sufficient** for first-time validation of any new
endpoint or schema-sensitive change. Real backends enforce many things
the fakes do not:

| Drift class | Example from this project | Fake behaviour | Real backend behaviour |
|---|---|---|---|
| **Schema renames between versions** | `restrict` → `restricted` in Incus 6.0; `restricted.devices.unix` → `unix-block` + `unix-char` | Fake accepts both; the schema is whatever the fake's struct allows | Real Incus rejects unknown keys with HTTP 400 |
| **Status-code quirks** | Incus returns HTTP 500 (not 409) with `"already exists"` for duplicate `INSERT`s at the DB layer | Fake returns 409 (well-behaved) | Real daemon returns 500 in some paths |
| **Async error envelopes** | Incus `POST /1.0/instances` returns 202 + op URL; the wait endpoint returns HTTP 200 with `{"type":"error","error_code":500,...}` when the op failed | Fake synchronously returns the final Operation with `Err` populated | Real daemon wraps the failure in an envelope that decoders expecting only an Operation silently swallow |
| **Body vs query parameters** | Incus routes by `?project=<name>`, NOT by the `project` field in the JSON body | Fake reads both | Real Incus reads only the query parameter; the body field is ignored |
| **Device validation** | A `root` device without a `pool` property overrides the profile's root device and is rejected | Fake stores any device map verbatim | Real Incus validates device schemas against its current config |
| **Required side-effects of bootstrap** | `EnsureProject` must seed the project's `default` profile with a root disk or every subsequent create fails | Fake pre-seeds a usable project, masking the gap | Real Incus creates a project with an empty default profile |

**Hard rule:** any new `providers/*` endpoint, any change to a request body
shape, and any change to a restricted/config key MUST be smoke-tested
against a real backend at least once before merge. The unit test against
the fake is for regression, not first-time validation. See
`.opencode/skills/incus-on-windows/SKILL.md` "Driver pitfalls" for the
concrete workflow + the cross-compiled smoke binary pattern.

When you do hit a fake-passes-real-fails gap, **extend the fake** to model
the real behaviour (so future tests catch the same drift) AND record the
gotcha in the relevant provider skill.

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
- No real Incus/PDNS/SeaweedFS in CI integration tests; only fakes. (This
  is a CI policy — it does NOT absolve you of the "smoke-test against the
  real backend once before merge" rule from the *Fake vs real* section
  above. The smoke-test happens on your dev machine, not in CI.)

## Required reading

- `/AGENTS.md`
- `docs/adr/0003-db-tooling.md`
- `docs/workstreams/WS-22-sandbox-integration-test-harness.md`
- `docs/architecture/conventions.md#testing`
