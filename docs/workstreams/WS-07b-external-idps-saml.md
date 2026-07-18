# WS-07b · External IdPs — SAML 2.0

```
Status: done
Phase: 1
Depends on: WS-06
Unblocks: —
```

## Goal

Let users log in via SAML 2.0 service-provider flow, so Lahijan can integrate
with enterprise IdPs (Microsoft Entra / Azure AD, Okta, OneLogin, Shibboleth,
Google Workspace SAML).

## Scope

**In scope:**
- `internal/app/lahijan/auth/saml/`:
  - SAML service provider (SP) implementation
  - metadata endpoint (`/api/v1/auth/saml/metadata`) that advertises our SP
  - `GET /api/v1/auth/saml/{provider}/start` — redirects to IdP with signed
    `AuthnRequest`
  - `POST /api/v1/auth/saml/{provider}/acs` — assertion consumer service,
    verifies signed assertion, extracts `NameID` + attributes
  - SP-initiated + IdP-initiated SSO
  - optional signed SAML logout (SLO)
- IdP configuration:
  - `conf.auth.saml.providers.*` per IdP (entity ID, metadata URL or inline
    XML, sign/encrypt certs)
  - metadata refresh job (River) for IdPs that publish metadata URLs
- Account linking: same `user_saml_identities` table as OAuth pattern (user_id,
  provider, name_id, attributes_json, created_at, updated_at)
- Cert/key management: SP signing key in encrypted DB column (or external file
  with path in `conf`)
- Just-in-time (JIT) provisioning: configurable policy — auto-create user on
  first SAML login, or require admin pre-registration
- Attribute mapping: configurable mapping from SAML attribute names to
  Lahijan fields (email, full_name)

**Out of scope:**
- OAuth + OIDC (WS-07a).
- Multi-IdP tenancy (one tenant = one IdP) — Phase 7 candidate.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0004-auth-methods.md`
- WS-06 doc (session shape)
- WS-07a doc (account-linking pattern)
- `.opencode/skills/backend-foundations/SKILL.md`

## Deliverables

- SAML SP metadata endpoint
- AuthnRequest signing + redirect
- ACS endpoint with assertion verification (signature, audience, recipient,
  not-before/not-on-or-after, replay detection)
- IdP metadata loading (URL or inline)
- `user_saml_identities` table + CRUD
- Configurable attribute mapping
- i18n strings for: "log in with SSO", SAML error messages
- Integration tests using a fake SAML IdP (e.g. `simplesamlphp` container or
  `test-saml-idp`)

## Definition of Done

- [x] metadata endpoint serves valid SP metadata
- [x] SP-initiated login works against a test IdP
- [x] IdP-initiated login works against a test IdP
- [x] signed assertions verified; unsigned rejected
- [x] replay attack rejected (in-response-to check)
- [x] audience + recipient validated
- [x] new SAML identity → either create user (if JIT enabled) or reject
- [x] link SAML to existing account
- [x] SP signing key encrypted at rest
- [x] every login emits an audit event
- [x] every user-facing string through `i18n.T`; en + fa in sync
- [x] `make lint test` green

## Open questions

- Library choice: `crewjam/saml` is the de-facto Go SAML library. License:
  BSD-2. **Resolved:** use it; ADR-0020 records the choice + the XML-DSig
  transitive dep stack (`goxmldsig`, `etree`, `xml-roundtrip-validator`).
- Encrypt assertions at the SP, or rely on TLS + signed assertions?
  **Resolved:** signed assertions only for MVP. The `crewjam/saml`
  `ServiceProvider` does not enable assertion-encrypted-by-default; a
  future WS can add an `EncryptAssertions` config flag if a deployer's
  threat model demands it (e.g. interior proxies between the SP and the
  browser).

## Notes

- SAML time skew is a real problem; allow ±60s by default, configurable.
  **Addressed:** crewjam's `ParseResponse` applies a ±60s skew on
  Conditions.NotBefore / NotOnOrAfter by default; no extra config needed.
- Many enterprise IdPs have quirks (Entra's `http://schemas.microsoft.com/...`
  attribute names, etc.); the attribute mapper must be per-IdP configurable.
  **Addressed:** `ProviderConfig.AttributeMap` is per-provider and defaults
  to the standard WS-Federation claim URIs; deployers override via
  `auth.saml.providers.<key>.emailAttribute` / `.nameAttribute` for IdPs
  that use non-standard names.
- This WS is the largest in Phase 1; budget time accordingly.
