# ADR-0021: MFA library choices — `go-webauthn/webauthn` + `pquerna/otp`

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-07c (Multi-Factor Authentication) requires Go libraries for two
security-critical concerns:

1. **TOTP (RFC 6238):** the algorithm shared by every authenticator app
   (Google Authenticator, Authy, 1Password). The surface is small:
   generate a random base32 secret, render an `otpauth://` provisioning
   URI, compute a 6-digit code over the current 30-second step, and
   validate it within a ±1 step window to absorb clock skew.
2. **WebAuthn / passkeys (W3C WebAuthn Level 3):** the registration
   (attestation) and authentication (assertion) ceremonies for platform
   authenticators (Touch ID, Windows Hello) and roaming authenticators
   (YubiKey). The surface is *huge*: CBOR decoding, attestation format
   verification ("packed", "tpm", "android-key", "android-safetynet",
   "fido-u2f", "apple", "none"), signature verification across multiple
   COSE algorithms (ES256, RS256, EdDSA), challenge generation + replay
   protection, and the relying-party / origin / top-level-origin checks.

`/AGENTS.md` mandates: "Don't introduce a new dependency without checking
it's permissively licensed and matches our patterns. If unsure, write an
ADR."

Options considered, per concern:

### TOTP

- **`github.com/pquerna/otp`** (Apache-2.0) — widely-used, mature,
  implements RFC 6238 (TOTP) + RFC 4226 (HOTP). Exposes clean `Generate`
  / `Validate` functions with `otp.ValidateOpts` for period / digits /
  algorithm / secret length. Maintained by Peter Querna (former Mozilla
  / Couchbase). Pros: well-known, focused, Apache-2.0, matches our
  direct-dep patterns.
- **`github.com/xlzd/gotp`** (MIT) — smaller; less widely used; the API
  mixes string/byte secret types awkwardly.
- **Hand-rolled** — RFC 6238 is a 30-line HOTP-then-truncate algorithm.
  Pros: zero dependencies. Cons: security-critical code with no reviewer
  base; we'd have to re-implement base32 secret generation, HMAC-SHA1
  truncation, and the ±window validation ourselves. Rejected.

### WebAuthn

- **`github.com/go-webauthn/webauthn`** (Apache-2.0) — the de-facto Go
  WebAuthn library; the official successor to `miracl/webauthn`. Used by
  Hashicorp, Grafana, Authelia, and many others. Maintained, audited,
  implements the full W3C WebAuthn Level 3 surface (attestation,
  assertion, CBOR, COSE, top-level origin). Ships a `WebAuthn` type with
  `BeginRegistration` / `FinishRegistration` / `BeginLogin` /
  `FinishLogin` that map 1:1 onto Lahijan's ceremony handlers.
  Pros: proven, audited, matches the WS-07c handler shape. Cons: pulls
  in `fxamacker/cbor`, `go-webauthn/x` (CBOR + COSE deps), and
  `mitchellh/mapstructure` (already an indirect dep via viper).
- **`github.com/miracl/webauthn`** — deprecated upstream in favor of
  `go-webauthn/webauthn`. Rejected.
- **Hand-rolled** — CBOR + COSE + attestation verification is several
  thousand lines of security-critical, format-heavy code. Rejected.

WS-07c's "Open questions" section explicitly records the default choice
for WebAuthn as "`go-webauthn/webauthn` is the standard. License:
Apache-2. (Default: use it.)"

## Decision

Lahijan adds two direct dependencies for WS-07c, both permissively
licensed:

1. `github.com/go-webauthn/webauthn@v0.11.x` — WebAuthn relying-party
   implementation (registration + authentication ceremonies, CBOR/COSE,
   attestation verification).
2. `github.com/pquerna/otp@v1.5.x` — RFC 6238 TOTP secret generation,
   provisioning URI, and ±window validation.

Transitive deps from `go-webauthn/webauthn`
(`fxamacker/cbor`, `go-webauthn/x`, `mitchellh/mapstructure`,
`google/uuid` (already direct)) are accepted; all are MIT or Apache-2.
`pquerna/otp` has no non-stdlib transitive deps.

The libraries are imported only from:

- `internal/app/lahijan/auth/mfa/webauthn/` (the WebAuthn relying-party
  package)
- `internal/app/lahijan/auth/mfa/webauthn/fake/` (the in-process test
  authenticator that signs real WebAuthn assertions)
- `internal/app/lahijan/auth/mfa/totp/` (the TOTP package)

No other package imports either library directly, so the WS-07c surface
stays replaceable if a future ADR supersedes this one.

## Consequences

- **Positive:** audited WebAuthn implementation; the relying-party surface
  (`WebAuthn`, `User`, `Credential`) maps cleanly onto Lahijan's existing
  user / identity model, so the auth/mfa orchestrator can consume
  WebAuthn through a small adapter (same shape as the OAuth/OIDC/SAML
  adapters in WS-07a/b).
- **Positive:** `pquerna/otp` gives us the full RFC 6238 surface
  (algorithm, period, digits, secret length, ±window) without dragging
  in unrelated concerns (HOTP, ocra, custom QR rendering).
- **Positive:** `go-webauthn/webauthn` ships an in-process test
  authenticator pattern (its own tests use it) that we can copy for the
  fake — the WS-07c DoD row "WebAuthn register → login works end-to-end
  (synthetic authenticator in tests)" is exercised without an external
  browser.
- **Negative:** adds ~3 transitive modules (CBOR + COSE stack). Bumps
  the module graph and binary size slightly (<1 MB).
- **Negative:** the WebAuthn relying-party requires persistent state
  between the `Begin*` and `Finish*` calls (the challenge + the user's
  listed credentials). That state lives in a short-lived DB row
  (`mfa_pending_sessions`) so the multi-node-ready pillar (ADR-0009) is
  preserved — any Lahijan node can serve the `Finish*` callback.

## Compliance

- `go.mod` lists `github.com/go-webauthn/webauthn` and
  `github.com/pquerna/otp` as direct requires.
- `internal/app/lahijan/auth/mfa/webauthn/webauthn.go` imports
  `github.com/go-webauthn/webauthn` and wraps `webauthn.WebAuthn` behind
  the package's own `RelyingParty` interface.
- `internal/app/lahijan/auth/mfa/webauthn/fake/authenticator.go` is the
  only consumer of the synthetic-authator codepath; it generates
  real attestation + assertion objects so the integration tests cover
  the full W3C ceremony.
- `internal/app/lahijan/auth/mfa/totp/totp.go` imports
  `github.com/pquerna/otp/totp` and uses it for both secret generation
  and validation. No other TOTP library is imported anywhere.
- TOTP secret + WebAuthn credential data are AES-GCM-encrypted at rest
  via the existing `auth/secrets.Crypto` envelope (WS-07a).

## References

- WS-07c (Multi-Factor Authentication)
- ADR-0004 (auth methods — mandates TOTP + WebAuthn + recovery codes in MVP)
- ADR-0018 (WS-06 auth dependency choices — set the AES-GCM envelope pattern
  this WS reuses for TOTP secret encryption)
- [W3C WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/)
- [RFC 6238 (TOTP)](https://www.rfc-editor.org/rfc/rfc6238)
- [RFC 4226 (HOTP)](https://www.rfc-editor.org/rfc/rfc4226)
- [go-webauthn/webauthn](https://github.com/go-webauthn/webauthn)
- [pquerna/otp](https://github.com/pquerna/otp)
