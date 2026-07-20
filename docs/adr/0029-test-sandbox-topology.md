# ADR-0029: Test sandbox topology — fakes per provider, coverage gate at the unit-test baseline

- **Status:** Accepted
- **Date:** 2026-07-20
- **Deciders:** maintainer

## Context

WS-22 ships the integration + e2e harness. Three concrete decisions
shape the design and need to be settled before the implementation can
land:

1. **Which backends get in-process fakes vs. real containers?** The WS
   doc names three provider fakes (`providers/{incus,powerdns,seaweedfs}/fake/`)
   but also says "could reuse minio test container" for SeaweedFS — the
   choice is genuinely open.

2. **What is the coverage gate threshold?** The WS doc says ≥60% for Go
   and ≥60% for the frontend, but also that "Coverage is a floor, not a
   target." The 60% number was picked before the test architecture was
   known to push most coverage behind the `integration` build tag.

3. **What is the flaky-rate policy?** The WS doc lists "test retry
   policy" as a DoD item without saying what the policy is.

This ADR settles all three.

## Options considered

### Provider fakes vs. real containers

- **All three as real containers** (dev mirror): highest fidelity, but
  Incus cannot run in GitHub Actions without kernel features. The Incus
  daemon needs `/dev/incus` + a uid/gid shifting layer the hosted
  runners don't provide. Rejected.

- **All three as in-process fakes** (the WS doc's headline choice):
  cheapest + fastest. Works for Incus + PowerDNS because the WS-11 +
  WS-12 fakes are already wire-level httptest servers. Does NOT work
  for SeaweedFS because the WS-13 fake is operations-interface-level
  (it implements the driver's internal interfaces directly, not the
  S3 wire protocol). The dashboard's storage journey + the aws-cli
  upload test need a real S3 endpoint.

- **Hybrid (the chosen option):** Incus + PowerDNS stay in-process
  (the existing fakes are already wire-level). SeaweedFS runs as a
  real `weed mini` container from the dev compose topology; the test
  compose re-uses the same image with a throwaway volume. SeaweedFS
  is cheap to run + exercises the actual SigV4 + S3 wire path the
  dashboard + aws-cli drive.

### Coverage threshold

- **60% hard-fail today (the WS doc headline):** would block every PR.
  The test architecture deliberately puts most coverage behind the
  `integration` build tag (testcontainers + fakes); a plain `go test`
  carries only the pure-logic tests. The actual unit-test coverage
  today is ~22% on Go and ~15% on the frontend. Setting the gate at
  60% would either block every PR or force low-value tests that
  contradict the WS doc's "don't write useless tests just to hit the
  number" note.

- **No gate (the doc's "aspirational" interpretation):** would not
  satisfy the DoD item "coverage gate enforced in CI."

- **Baseline-anchored floor (the chosen option):** set the gate at
  the current level (Go 20%, web 10%) so coverage can only go up.
  Each subsequent WS that adds user-facing logic re-runs
  `make cover-check` locally before pushing; the floor rises
  monotonically. The 60% aspiration stays in the WS doc as the
  long-term target; the gate's job is to prevent regressions, not
  to coerce a number.

### Flaky-rate policy

- **Zero retries, treat any flake as a P1 bug:** clean signal, but
  wastes CI time on transient issues (network blips, container slow
  starts) that aren't real bugs.

- **Aggressive retries (5+) hides flakes:** the suite goes green but
  the flake rate keeps climbing.

- **2 retries on CI, 0 locally, retain-on-failure trace + video (the
  chosen option):** matches Playwright's recommended default. The
  HTML report marks any test that failed-then-passed as flaky; a
  non-zero flaky count is a P2 bug per the WS doc's "expectation is
  zero flakies per WS" language. Retrying only on CI keeps local dev
  fast.

## Decision

1. **Provider topology:** Incus + PowerDNS stay in-process httptest
   fakes (`providers/<name>/fake/`). SeaweedFS runs as a real
   `chrislusf/seaweedfs:3.61` container in `deployments/docker-compose.test.yml`
   with an anonymous (throwaway) volume. The SeaweedFS fake stays in
   the tree for the WS-13 driver unit tests; the e2e path uses the
   real container.

2. **Coverage gate thresholds:**
   - Go: `LAHIJAN_COVERAGE_MIN_PCT=20` (default in
     `scripts/cover-check/main.go`, overridable via env).
   - Web: vitest `coverage.thresholds` pinned at 10% across the board.
   - Both gates run on every PR via `.github/workflows/e2e.yml`
     (`cover` + `web-cover` jobs).
   - Reviewers must approve any drop; the script's `::error::` message
     makes the regression visible in the GH Actions log.
   - The 60% aspiration in the WS doc is the long-term target; raising
     the floor is a follow-up task tracked in the WS-22 resolution
     notes.

3. **Flaky-rate policy:**
   - Playwright `retries: 2` on CI, `0` locally.
   - `trace: "retain-on-failure"` + `video: "retain-on-failure"` so
     the report HTML carries the evidence for any flake.
   - A non-zero flaky count in the report is a P2 bug; a test that
     never passes on the first try for 3 consecutive runs is a P1.

## Consequences

- **Positive:** the e2e stack runs in any Linux CI environment
  (GitHub Actions ubuntu-latest). No kernel features required.
- **Positive:** the coverage gate is meaningful (it can only be
  violated by a regression, not by the baseline).
- **Positive:** the flake policy is operationalizable — the report
  surfaces the signal, the engineering response is documented.
- **Negative:** SeaweedFS in the test sandbox costs ~150 MB of image
  pull + 10s of cold-start. Acceptable for a CI job that already
  runs ~10 minutes.
- **Negative:** the coverage floor will need to be raised
  deliberately per WS. This is a feature, not a bug — it forces a
  conversation about test value instead of letting the gate
  silently inflate.
- **Negative:** the Incus + PowerDNS fakes are now used by both
  unit tests + the e2e harness, so a regression in the fake shape
  breaks both layers. Mitigated by the unit tests in WS-11 + WS-12
  that exercise the driver against the fake.

## Compliance

- The `providers/<name>/fake/NewServerStandalone()` constructors
  (added in this WS) are the only fake entry points the e2e harness
  uses; `NewServer(t testing.TB)` is preserved for unit tests.
- The test compose file (`deployments/docker-compose.test.yml`) uses
  anonymous volumes + an isolated network so a `down -v` wipes all
  state. No named volumes that survive a run.
- The cover-check tool is Go (not bash) so the same `make cover-check`
  target works on Linux CI + Windows local dev.

## References

- WS-22 doc (`docs/workstreams/WS-22-sandbox-integration-test-harness.md`)
- ADR-0005 (single-host topology — the test stack mirrors the prod
  shape with the exception of provider containers)
- ADR-0007 (shared Postgres — the test stack uses the same image +
  init scripts as dev)
- ADR-0025, ADR-0026, ADR-0027 (provider client choices — explain
  why the fakes are wire-level for Incus + PowerDNS but
  operations-level for SeaweedFS)
