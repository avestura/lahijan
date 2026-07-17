# ADR-0018: WS-06 auth dependency choices

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

WS-06 (Core Auth) introduces the first user-facing strings (ADR-0017) and the
first outbound email path (verify-email, password-reset, email-change). It also
needs argon2id password hashing. Each of these needs a library, and
`/AGENTS.md` says: "Don't introduce a new dependency without checking it's
permissively licensed and matches our patterns. If unsure, write an ADR."

Options considered, per concern:

- **Password hashing (argon2id):**
  - `golang.org/x/crypto/argon2` — BSD-3-Clause, maintained by the Go team, the
    de-facto Go implementation; already an indirect dep via pgx/testcontainers.
  - a vendored copy — no benefit, drift risk.
  - ADR-0004 already mandates argon2id; the only question is which package.
- **Backend i18n (ADR-0017):**
  - `github.com/nicksnyder/go-i18n/v2` — MIT, the standard Go i18n library,
    supports message bundles, plurals, interpolation, and locale loading; the
    ADR-0017 compliance section explicitly names it.
  - hand-rolled map lookup — loses plural/interpolation support and would
    diverge from ADR-0017's stated tooling.
- **In-process test SMTP server:**
  - `github.com/emersion/go-smtp` — MIT, small, purpose-built server with a
    pluggable backend; ideal for asserting the email subsystem in unit tests
    without an external container.
  - a real MailHog container in CI — already used for manual dev, but coupling
    every unit test to a container is slow and brittle.
  - hand-rolled raw SMTP parser — error-prone and not worth it.

## Decision

Lahijan adds three direct dependencies for WS-06, all permissively licensed:

1. `golang.org/x/crypto/argon2` (promoted from indirect) — argon2id hashing.
2. `github.com/nicksnyder/go-i18n/v2` — backend message bundles (`en.json`,
   `fa.json`) and the `T(ctx, key, args)` helper.
3. `github.com/emersion/go-smtp` — used **only in tests** as an in-process SMTP
   capture server for the email subsystem.

`go-smtp` stays a direct dependency rather than a test-only indirect because it
is imported by test helpers that ship in the same module.

## Consequences

- **Positive:** battle-tested implementations of three security/i18n-sensitive
  concerns; no NIH code in the hot path.
- **Positive:** ADR-0017's mandated tooling (go-i18n) is now wired in.
- **Negative:** three more modules to keep current; security advisories on
  `x/crypto` must be watched (already covered by WS-01 dependency hygiene).
- **Neutral:** `go-smtp` is test-scope today; if WS-23 ever ships an SMTP
  *server* feature, the same dep is reused.

## Compliance

- `internal/app/lahijan/auth/password/` uses `golang.org/x/crypto/argon2` and
  no other hashing library.
- `internal/app/lahijan/i18n/` exposes a `T` helper backed by
  `go-i18n/v2/i18n.Localizer`; locale JSON files live under
  `internal/app/lahijan/i18n/locales/`.
- The test SMTP server lives under `internal/app/lahijan/notify/email/testsmtp/`
  and imports `github.com/emersion/go-smtp`.
- `go.mod` lists all three as direct requires; `go mod tidy` keeps them tidy.

## References

- ADR-0004 (auth methods — mandates argon2id)
- ADR-0017 (i18n from day one — names go-i18n)
- [golang.org/x/crypto/argon2](https://pkg.go.dev/golang.org/x/crypto/argon2)
- [go-i18n](https://github.com/nicksnyder/go-i18n)
- [go-smtp](https://github.com/emersion/go-smtp)
- WS-06 (Core Auth)
