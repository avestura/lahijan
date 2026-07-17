# WS-07b · External IdPs — SAML 2.0

```
Status: pending
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

- [ ] metadata endpoint serves valid SP metadata
- [ ] SP-initiated login works against a test IdP
- [ ] IdP-initiated login works against a test IdP
- [ ] signed assertions verified; unsigned rejected
- [ ] replay attack rejected (in-response-to check)
- [ ] audience + recipient validated
- [ ] new SAML identity → either create user (if JIT enabled) or reject
- [ ] link SAML to existing account
- [ ] SP signing key encrypted at rest
- [ ] every login emits an audit event
- [ ] every user-facing string through `i18n.T`; en + fa in sync
- [ ] `make lint test` green

## Open questions

- Library choice: `crewjam/saml` is the de-facto Go SAML library. License:
  BSD-2. (Default: use it; write an ADR if needed.)
- Encrypt assertions at the SP, or rely on TLS + signed assertions?
  (Default: signed assertions for MVP; encrypted assertions as a config option.)

## Notes

- SAML time skew is a real problem; allow ±60s by default, configurable.
- Many enterprise IdPs have quirks (Entra's `http://schemas.microsoft.com/...`
  attribute names, etc.); the attribute mapper must be per-IdP configurable.
- This WS is the largest in Phase 1; budget time accordingly.
