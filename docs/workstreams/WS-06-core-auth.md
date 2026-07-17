# WS-06 · Core Auth (Password + Tokens + Sessions)

```
Status: done
Phase: 1
Depends on: WS-05
Unblocks: WS-07a, WS-07b, WS-07c, WS-08
```

## Goal

Establish the auth subsystem that everyone else builds on: argon2id password
login, DB-backed sessions with refresh tokens, personal access tokens,
password reset, email verification, and the i18n message bundles for
auth-related emails. After this WS, a user can register, log in, and call the
API with a session cookie or a PAT.

## Scope

**In scope:**
- `internal/app/lahijan/auth/password/`:
  - argon2id hashing with sane parameters (memory, iterations, parallelism)
  - password strength validation (min entropy, breached-password check optional)
  - migration path for future hash upgrades
- `internal/app/lahijan/auth/session/`:
  - DB-backed sessions (`sessions` table; FK to `users`)
  - opaque session cookies (signed, HttpOnly, SameSite=Lax/Strict)
  - refresh tokens with rotation + reuse detection
  - logout (revoke session + token family)
- `internal/app/lahijan/auth/pat/`:
  - personal access tokens (fine-grained scopes, expiry, last-used-at)
  - hashed at rest; shown once at creation
- `internal/app/lahijan/auth/email/`:
  - email verification flow (signed token, expiring link)
  - password reset flow (signed token, expiring link, single-use)
  - email change flow
- `internal/app/lahijan/i18n/`:
  - go-i18n setup, `en.json` + `fa.json` seeded with all auth strings
  - `T(ctx, key, args)` helper
- `internal/app/lahijan/notify/email/`:
  - SMTP client wrapper (uses `conf.smtp.*`)
  - template rendering (Go `html/template` per locale)
  - the actual templates for verify-email, reset-password, email-changed
- API endpoints under `/api/v1/auth/*`:
  - `POST /register`, `POST /login`, `POST /logout`, `POST /refresh`
  - `POST /verify-email`, `POST /resend-verification`
  - `POST /password-reset/request`, `POST /password-reset/confirm`
  - `GET /me`, `PATCH /me` (change email/password)
  - `GET/POST/DELETE /personal-access-tokens`

**Out of scope:**
- OAuth / OIDC / SAML (WS-07a, WS-07b).
- MFA (WS-07c) — but the session shape must leave room for an MFA challenge
  step before issuing the session.
- RBAC policy enforcement (WS-08) — but the session carries the user identity
  used by RBAC.
- Audit event emission plumbing (WS-08) — but the auth handlers call into an
  `auditEmitter` interface stubbed here.

## Required reading for the AI session

- `/AGENTS.md`
- `internal/app/lahijan/AGENTS.md`
- `docs/adr/0004-auth-methods.md`
- `docs/adr/0017-i18n-from-day-one.md`
- `docs/architecture/conventions.md#security`
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- Working register/login/logout/refresh with rotation-aware refresh tokens
- Working email verification (signed token via email)
- Working password reset (signed token via email, single-use)
- Working PAT issuance (shown once, hashed at rest)
- `en.json` and `fa.json` in sync (CI check)
- SMTP integration tested against an in-process test SMTP server
- Integration tests for: register, login, refresh rotation, reuse detection,
  reset flow, PAT issuance + use + revocation

## Definition of Done

- [x] every auth endpoint ships under `/api/v1/auth/*` and uses the error envelope
- [x] every privileged auth action emits an audit event (interface stubbed if WS-08 not merged yet)
- [x] passwords hashed with argon2id; parameters configurable via `conf`
- [x] cookies: HttpOnly, Secure (configurable for local dev), SameSite per env
- [x] PATs: hashed at rest, shown once, scoped, expiry-enforced
- [x] every user-facing string goes through `i18n.T`
- [x] `en.json` and `fa.json` keys match (CI check)
- [x] ≥1 happy + ≥1 failure test per endpoint
- [x] `make lint test` green

## Open questions

- Email service in dev: real SMTP, MailHog container, or in-process catch-all?
  **Resolved:** MailHog added to `deployments/docker-compose.dev.yml` (SMTP on
  `:1025`, web UI on `:8025`); the app talks to it via `conf.smtp.*` when
  `smtp.enabled` is true. Tests use an in-process `emersion/go-smtp` capture
  server (`notify/email/testsmtp`) so they don't need a container.
- Password breach check (HaveIBeenPwned k-anonymity API) — opt-in via conf?
  **Resolved:** config key `auth.password.breachCheck.enabled` is wired (off by
  default for privacy); the actual network lookup is deferred to a follow-up
  since it adds an outbound HTTP dependency and is not load-bearing for MVP.

## Notes

- The session shape must support an MFA challenge phase before issuing the
  real session, so WS-07c can plug in without rewriting.
- Refresh token rotation + reuse detection is critical; get it right or the
  whole auth story is broken.
