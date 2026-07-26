# WS-22e · Admin + Security E2E Flows

```
Status: pending
Phase: 6
Depends on: WS-22 (harness + WS-22b data-testids), WS-08 (RBAC + audit), WS-10c (sample .wasm), WS-17 (billing)
Unblocks: —
```

## Goal

Close the last e2e coverage gaps from WS-22: the **admin** journeys
(plugin install → grant → audit event; billing top-up → user spends)
and the **security** journeys that need extra fixtures or libraries
(full TOTP scan→verify loop; WebAuthn/passkey enrollment; personal
access token create→revoke from the UI). Today these are scaffolded
specs that skip themselves behind `LAHIJAN_E2E_RUN_*` flags. After
this WS they run end-to-end in `make test-e2e`.

This is the follow-up the WS-22 resolution notes deferred: "admin
flows need an admin-seed fixture (operator-side)" and "MFA UI challenge
needs the post-202 challenge UI + a TOTP code generator".

## Scope

**In scope:**
- An **admin fixture** the e2e harness seeds before the suite runs:
  - a `platform.admin`-role user (or a promoted registered user) the
    admin specs can log in as, without manual operator steps.
  - seeding path decided in this WS (see Open questions): either a
    `--e2e-seed` flag on the Lahijan app, a SQL fixture applied by
    `scripts/run-e2e.sh` after migrations, or an admin-promotion
    endpoint gated to non-prod environments.
- **`admin-plugins.spec.ts`** — full journey:
  - upload the sample `.wasm` from `examples/plugins/` (WS-10c)
  - review the requested permissions → grant each → assert an audit
    event appears in `/audit` for the grant
  - disable → re-enable → delete the plugin
- **`admin-billing.spec.ts`** — full journey:
  - admin tops up a freshly-registered user's balance via the UI
  - the user (separate session) sees the new balance + can spend
    against it (e.g. create a billable resource)
- **`mfa.spec.ts`** — extend to the full TOTP loop:
  - add a TOTP library to `test/e2e` deps (e.g. `@otplib/preset-default`)
  - enroll → read the secret → compute a 6-digit code → verify → assert
    enrolled state; then disable with the current password
- **`settings.spec.ts`** — extend the settings feature cards:
  - personal access token: create → reveal → copy → revoke (needs
    `data-testid` coverage on `PersonalAccessTokensCard`)
  - identities: assert the empty state + (if an IdP fake exists) link flow
- `data-testid` coverage on: `PersonalAccessTokensCard`, `TOTPCard`,
  the plugin upload/grant UI, and the admin billing top-up form.
- Update `scripts/run-e2e.sh` to set `LAHIJAN_E2E_RUN_PLUGINS`,
  `LAHIJAN_E2E_RUN_BILLING`, and (once the TOTP lib lands)
  `LAHIJAN_E2E_RUN_MFA` so `make test-e2e` exercises them.
- A sample-plugin artifact path agreed with the harness (WS-10c ships
  the `.wasm`; this WS points the spec at it).

**Out of scope:**
- WebAuthn/passkey UI e2e — browser automation of the WebAuthn
  ceremony needs the virtual authenticator API; tracked separately.
- Payment-gateway flows → WS-27 (Stripe).
- Load / soak → Phase 7.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/workstreams/WS-22-sandbox-integration-test-harness.md` (the
  harness this builds on; especially the Resolution notes + the
  "Suggested follow-up WSs" list)
- `docs/workstreams/WS-08-rbac-audit-log.md` (admin role + audit)
- `docs/workstreams/WS-10c-wasm-sample-plugins-marketplace.md`
  (sample `.wasm` artifact)
- `docs/workstreams/WS-17-billing-metering.md` (top-up + ledger)
- `.opencode/skills/testing-conventions/SKILL.md`
- `test/e2e/specs/helpers.ts`, `admin-plugins.spec.ts`,
  `admin-billing.spec.ts`, `mfa.spec.ts`, `settings.spec.ts`
- `web/AGENTS.md`

## Deliverables

- Admin-seed fixture (mechanism chosen under Open questions)
- `admin-plugins.spec.ts`, `admin-billing.spec.ts`, `mfa.spec.ts`,
  `settings.spec.ts` upgraded from scaffolds to full UI journeys
- `data-testid` coverage on the settings cards + plugin/billing admin UI
- `scripts/run-e2e.sh` enabling the matching `LAHIJAN_E2E_RUN_*` flags
- This WS's Status field + `docs/workstreams/README.md` updated

## Definition of Done

- [ ] admin-seed fixture reproducible from a clean test compose
- [ ] admin-plugins spec covers upload → grant → audit → lifecycle
- [ ] admin-billing spec covers top-up → user spend
- [ ] mfa spec covers the full enroll → verify → disable loop
- [ ] settings spec covers PAT create → reveal → revoke
- [ ] every new flow has ≥1 happy + ≥1 failure assertion
- [ ] `make test-e2e` runs all of them green in the CI budget (under 15 min)
- [ ] no new flakies (per ADR-0029 flaky-rate policy)
- [ ] `docs/workstreams/WS-22e-*.md` Status updated; README index row added

## Open questions

- **Admin-seed mechanism** — three candidates, pick one and record in an ADR:
  1. `--e2e-seed` CLI flag on the Lahijan app that creates the admin
     user + role on startup when `LAHIJAN_ENVIRONMENT=dev`. Simplest to
     drive; risk is the flag accidentally shipping to prod (guard with
     a build tag or env check).
  2. A SQL fixture (`test/e2e/fixtures/admin.sql`) applied by
     `run-e2e.sh` after migrations. No app-code change; but the
     argon2id hash for the admin password must be precomputed.
  3. A non-prod-only `POST /api/v1/admin/promote` endpoint the spec
     calls once. Most "real"; adds an API surface to gate carefully.
- **TOTP library** — `@otplib/preset-default` (MIT, popular) vs rolling
  a minimal RFC-6238 implementation. Prefer the library; pin the version.
- **Sample plugin artifact location** — confirm the path WS-10c writes
  the `.wasm` to and whether the harness needs to build it first.

## Notes

- The existing `admin-plugins.spec.ts` + `admin-billing.spec.ts`
  scaffolds (gated behind their flags) are the starting point; this WS
  replaces their bodies, not the files.
- Keep the admin user in a **separate tenant** from the regular-user
  specs so tenant-row isolation is also exercised by the billing flow.
- Audit-event assertions should poll `/api/v1/audit` (the event is
  emitted before the side effect but the row may lag by the request
  that just completed) — use `expect.poll`, never `time.Sleep`.
