# ADR-0004: All auth methods (password + OAuth + OIDC + SAML + MFA)

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

Lahijan is an open-source cloud platform. Different deployers have different
auth requirements: a homelab user wants email/password; a small business wants
Google/GitHub OAuth; an enterprise needs OIDC/SAML SSO; security-conscious
deployers want MFA.

Options considered:

- **Email/password + API tokens only** — minimum viable; but locks out users
  who need SSO.
- **Email/password + OAuth (Google/GitHub)** — friendly for OSS/self-host; no
  enterprise story.
- **+ OIDC/SAML SSO** — enterprise-ready; significant scope (SAML is heavy).
- **All of the above** — maximum breadth; biggest scope.

## Decision

Lahijan MVP ships **all of the above**:

- Email/password (argon2id) + sessions + refresh tokens + personal access tokens
- OAuth social login (Google, GitHub, with a generic provider abstraction)
- OIDC (generic, any compliant IdP)
- SAML 2.0 (generic, any compliant IdP)
- MFA: TOTP (RFC 6238), WebAuthn / passkeys, recovery codes

This is broken into sub-workstreams to keep PRs reviewable:

- **WS-06** Core Auth (password, tokens, sessions, reset, verification)
- **WS-07a** External IdPs: OAuth + OIDC
- **WS-07b** External IdPs: SAML 2.0
- **WS-07c** MFA (TOTP, WebAuthn, recovery codes)

## Consequences

- **Positive:** single deployer can serve homelab, small biz, and enterprise.
- **Positive:** account linking lets one user log in via multiple methods.
- **Negative:** large surface area; security-critical code; must be audited carefully.
- **Negative:** SAML in particular is XML-heavy and adds a non-trivial dependency.

## Compliance

- `internal/app/lahijan/auth/` has subpackages for `password`, `oauth`,
  `oidc`, `saml`, `mfa`.
- Every external IdP is configured via `conf` (`auth.oauth.*`, `auth.oidc.*`,
  `auth.saml.*`).
- The user table has a `password_hash` (nullable), and there are separate
  identity-linking tables (`user_oauth_identities`, `user_saml_identities`,
  `user_webauthn_credentials`, `user_totp_secrets`).
- Every successful login emits an audit event.

## References

- WS-06, WS-07a, WS-07b, WS-07c
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
- ADR-0013 (Ledger billing — uses auth for balance ownership)
