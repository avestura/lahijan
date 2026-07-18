# ADR-0020: SAML library choice — `crewjam/saml`

- **Status:** Accepted
- **Date:** 2026-07-18
- **Deciders:** maintainer

## Context

WS-07b (External IdPs — SAML 2.0) requires a Go library that implements the
SAML 2.0 service-provider surface: signing `AuthnRequest`s, publishing SP
metadata, and verifying signed assertions (signature, audience, recipient,
conditions, replay via `InResponseTo`). SAML is XML- and XML-DSig-heavy; writing
a hand-rolled implementation would be a multi-thousand-line security-critical
surface with no upside over an existing audited implementation.

`/AGENTS.md` mandates: "Don't introduce a new dependency without checking it's
permissively licensed and matches our patterns. If unsure, write an ADR."

Options considered:

- **`github.com/crewjam/saml`** (BSD-2-Clause) — the de-facto Go SAML library,
  used by Hashicorp, Grafana, and many others. Maintained, audited, implements
  the full SP + IdP surface, including XML-DSig via
  `github.com/russellhaering/goxmldsig`. Ships a `ServiceProvider` type with
  `Metadata()`, `MakeRedirectAuthenticationRequest()`, and `ParseResponse()`,
  which map 1:1 onto the WS-07b handler surface. Pros: proven, audited, matches
  the WS-07b shape. Cons: pulls in `etree`, `goxmldsig`, and
  `xml-roundtrip-validator` as transitive deps.
- **`github.com/ubogdan/network-saml-client`** — a smaller fork; less audited,
  sporadic maintenance. Rejected on security grounds.
- **Hand-rolled** — would require implementing XML-DSig (canonicalization,
  transforms, X.509 cert handling) ourselves; thousands of lines of
  security-critical code with no reviewer base. Rejected.

WS-07b's "Open questions" section explicitly records the default choice as
"`crewjam/saml` is the de-facto Go SAML library. License: BSD-2. (Default: use
it; write an ADR if needed.)"

## Decision

Lahijan adds `github.com/crewjam/saml@v0.5.x` as a direct dependency for the
WS-07b SAML service-provider implementation. Transitive deps
(`beevik/etree`, `russellhaering/goxmldsig`, `mattermost/xml-roundtrip-validator`,
`jonboulle/clockwork`) are accepted; all are permissively licensed (BSD-2/MIT).

The library is imported only from `internal/app/lahijan/auth/saml/` (the
service-provider package) and `internal/app/lahijan/auth/saml/fake/` (the
in-process IdP used by integration tests). No other package imports `crewjam/saml`
directly, so the WS-07b surface stays replaceable if a future ADR supersedes
this one.

## Consequences

- **Positive:** audited SAML implementation; the SP surface (`ServiceProvider`,
  `EntityDescriptor`, `Assertion`) maps cleanly onto Lahijan's `Provider`
  interface from WS-07a, so the `auth/idp` account-linking service can consume
  SAML through the same adapter shape OAuth and OIDC use.
- **Positive:** library ships an `IdentityProvider` type that the in-process
  test fake uses to sign real XML-DSig assertions — the WS-07b DoD rows
  "signed assertions verified; unsigned rejected" and "replay attack rejected"
  are exercised end to end without an external container.
- **Negative:** adds ~5 transitive modules, including the XML-DSig stack.
  Bumps the module graph and binary size slightly (≈2 MB).
- **Negative:** the library's `ParseResponse` runs the full XML-DSig
  canonicalization, which is CPU-heavy relative to OAuth/JWT; per-request cost
  is in the millisecond range and acceptable for login.

## Compliance

- `go.mod` lists `github.com/crewjam/saml` as a direct require.
- `internal/app/lahijan/auth/saml/saml.go` imports `github.com/crewjam/saml` and
  wraps `saml.ServiceProvider` behind the package's own `Provider` interface.
- `internal/app/lahijan/auth/saml/fake/server.go` imports
  `github.com/crewjam/saml` and is the only consumer of the `saml.IdentityProvider`
  type.
- No other Go file in the repo imports `github.com/crewjam/saml` directly.

## References

- WS-07b (External IdPs — SAML 2.0)
- ADR-0004 (auth methods — mandates SAML 2.0 in MVP)
- [crewjam/saml](https://github.com/crewjam/saml)
- [goxmldsig](https://github.com/russellhaering/goxmldsig)
