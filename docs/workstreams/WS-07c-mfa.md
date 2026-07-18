# WS-07c · Multi-Factor Authentication

```
Status: done
Phase: 1
Depends on WS-06
Unblocks: —
```

## Goal

Add a second factor to logins: TOTP (RFC 6238 authenticator apps), WebAuthn /
passkeys (platform authenticators + security keys), and recovery codes. After
this WS, users can enable MFA and the login flow challenges them appropriately.

## Scope

**In scope:**
- `internal/app/lahijan/auth/mfa/totp/`:
  - secret generation + QR code provisioning URI (`otpauth://`)
  - TOTP verification (window of ±1 step)
  - `user_totp_secrets` table (user_id, secret (encrypted), confirmed_at)
  - enrollment flow: generate → user verifies 6-digit → mark confirmed
- `internal/app/lahijan/auth/mfa/webauthn/`:
  - registration ceremony (attestation)
  - authentication ceremony (assertion)
  - `user_webauthn_credentials` table (user_id, credential_id, public_key,
    sign_count, transports, name, created_at, last_used_at)
  - platform authenticator support (Touch ID, Windows Hello) and roaming
    authenticators (YubiKey)
- `internal/app/lahijan/auth/mfa/recovery/`:
  - 10 single-use recovery codes per user, shown once, hashed at rest
- MFA policy:
  - per-user opt-in (default)
  - per-tenant enforced (admin setting: "all members must use MFA")
  - per-role enforced (e.g. admins must use MFA)
- Login flow integration:
  - if user has MFA enabled, the `POST /login` response is `202 Accepted` with
    a `pending_session_token` instead of `200 OK` with a session
  - `POST /api/v1/auth/mfa/challenge` accepts `pending_session_token` + a code
    (TOTP / recovery / WebAuthn assertion) → on success, issues the real
    session + refresh token
- API endpoints:
  - `POST /api/v1/me/mfa/totp/enroll`, `/verify`, `/disable`
  - `POST /api/v1/me/mfa/webauthn/register/begin`, `/register/finish`,
    `/login/begin`, `/login/finish`
  - `GET /api/v1/me/mfa/recovery` (regenerate), `POST /api/v1/me/mfa/recovery/use`
  - `POST /api/v1/auth/mfa/challenge` (during login)

**Out of scope:**
- SMS-based MFA (deprecated pattern; out of scope unless explicitly requested).
- Per-tenant password policy (covered by WS-08 RBAC + tenant settings).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0004-auth-methods.md`
- WS-06 doc (session + login flow this WS intercepts)
- WS-07a/07b docs (external IdPs also go through MFA challenge after success)
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- TOTP enrollment + verification
- WebAuthn registration + login ceremonies
- Recovery codes (generation, hashing, single-use)
- MFA policy enforcement in the login flow
- Login flow returns `pending_session_token` when MFA is required
- i18n strings for: "scan this QR", "enter your code", "save these recovery
  codes", etc.
- Integration tests for each ceremony

## Definition of Done

- [x] TOTP enroll → verify → disable works end-to-end
- [x] WebAuthn register → login works end-to-end (synthetic authenticator in
      tests)
- [x] recovery codes: generate → use → invalidated
- [x] login with MFA-enabled user returns `pending_session_token`
- [x] MFA challenge succeeds → real session issued
- [x] MFA challenge fails 5 times → pending token revoked
- [x] per-tenant "MFA required" policy enforced at login
- [x] every privileged MFA action emits an audit event
- [x] every user-facing string through `i18n.T`; en + fa in sync
- [x] `make lint test` green

## Open questions

- WebAuthn library choice: `go-webauthn/webauthn` is the standard. License:
  Apache-2. **Resolved:** ADR-0021 records the choice (v0.11.x).
- TOTP secret encrypted at rest with what key? **Resolved:** same AES-GCM
  master key (`conf.auth.secrets.encryptionKey`) used for OAuth/OIDC
  tokens; reused via `auth/secrets.Crypto`.
- Allow user to disable MFA without re-entering password? **Resolved:** no.
  `POST /me/mfa/totp/disable` requires `currentPassword`; the api handler
  calls `sessionSvc.VerifyCredentials` to re-authenticate before revoking
  the factor.

## Notes

- WebAuthn testing in CI is tricky; use `firefox-webauthn` or a synthetic
  authenticator library. **Resolved:** `auth/mfa/webauthn/fake/authenticator.go`
  implements a minimal ES256 synthetic authenticator that produces real
  CBOR + ASN.1 ECDSA signatures the relying-party verifies end to end
  (register → login covered by `TestCeremony_RegisterThenLogin` +
  `TestMFA_WebAuthnCeremonyViaHTTP`).
- `pending_session_token` must be short-lived (e.g. 5 min) and single-use.
  **Resolved:** default `auth.mfa.pendingTTLSeconds = 300`, single-use
  enforced by `mfa_pending_sessions.consumed_at`, brute-force lockout at
  `auth.mfa.maxAttempts = 5` (returns 429 on the 5th failed challenge).
