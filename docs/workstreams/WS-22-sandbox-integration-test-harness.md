# WS-22 · Sandbox / Integration Test Harness

```
Status: pending
Phase: 6
Depends on: WS-14, WS-15, WS-16
Unblocks: WS-23 (release)
```

## Goal

Make the test pyramid real. Ship the integration + e2e harness that gives us
confidence to refactor and release. After this WS: a fresh `make test-e2e`
brings up a docker-compose stack with fakes for Incus/PDNS/SeaweedFS, runs
Playwright against the dashboard, and tears it down cleanly.

## Scope

**In scope:**
- `deployments/docker-compose.test.yml` — like dev, but with throwaway volumes,
  isolated network, no Lahijan binary prebuilt (built from source in CI)
- Provider fakes:
  - `internal/app/lahijan/providers/incus/fake/` — fake Incus REST API over
    httptest, simulating instances/projects/etc. in memory
  - `providers/powerdns/fake/` — fake PDNS API
  - `providers/seaweedfs/fake/` — fake S3 API (could reuse minio test container)
- testcontainers-go harness:
  - already started in WS-03; this WS extends with provider fakes + River
  - one shared `TestMain` per package brings up everything
- Playwright setup:
  - `test/e2e/` with `playwright.config.ts`
  - one spec per primary user journey:
    - register → verify email → login
    - create instance → start → exec → stop → delete
    - create zone → add A record → resolve via fake
    - create bucket → mint creds → upload via aws-cli (in test container)
    - enroll TOTP → log in with MFA
    - admin: install plugin → grant → see event
    - admin: top up user balance → user can spend
- Coverage gate:
  - Go: ≥60% statements for MVP (revisited per WS)
  - Frontend: ≥60% lines
- CI:
  - unit tests on every PR (already in WS-01)
  - integration tests on `main` + on labeled PRs (`run:integration`)
  - e2e nightly on `main` (and on labeled PRs `run:e2e`)

**Out of scope:**
- Load / soak testing (Phase 7).
- Mutation testing (Phase 7).
- Visual regression (Phase 7).

## Required reading for the AI session

- `/AGENTS.md`
- `.opencode/skills/testing-conventions/SKILL.md`
- `docs/adr-0005-mvp-topology.md`
- WS-03, WS-11, WS-12, WS-13 docs (the layers being tested)
- WS-14..17 docs (the modules being tested)
- WS-20, WS-21 docs (the UIs being tested)

## Deliverables

- `docker-compose.test.yml` + helper scripts in `scripts/`
- Three provider fakes (incus, powerdns, seaweedfs)
- Playwright config + e2e specs covering the user journeys above
- CI workflow `e2e.yml` that runs the harness
- Coverage gate enforcement

## Definition of Done

- [ ] `make test` (unit) runs in <30s
- [ ] `make test-integration` brings up Postgres + fakes, runs all
      integration tests in <5 minutes
- [ ] `make test-e2e` brings up the full stack (with fakes), runs Playwright,
      in <15 minutes
- [ ] every primary user journey has at least one e2e spec
- [ ] coverage gate enforced in CI
- [ ] a flaky test rate monitor (test retry policy)

## Open questions

- Should the fake Incus be re-used by dev for offline work? (Default: no, dev
  should use real Incus in docker.)
- Playwright browser binaries: CI-cached? (Default: yes, use the official
  Playwright GH action.)
- Coverage gate: hard fail or warn? (Default: hard fail at MVP threshold.)

## Notes

- The fake Incus should be "realistic enough to catch most bugs" but doesn't
  need to faithfully reproduce every Incus quirk.
- Coverage is a floor, not a target. Don't write useless tests just to hit
  the number.
