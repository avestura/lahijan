# WS-07a · External IdPs — OAuth 2.0 + OIDC

```
Status: done
Phase: 1
Depends on: WS-06
Unblocks: —
```

## Goal

Let users log in (and link their existing account to) external identity
providers: Google and GitHub via OAuth 2.0, and any OIDC-compliant provider
(Keycloak, Auth0, Okta, Google Workspace, etc.) via OpenID Connect.

## Scope

**In scope:**
- `internal/app/lahijan/auth/oauth/`:
  - generic OAuth 2.0 client (authorization-code flow + PKCE)
  - built-in provider presets for Google, GitHub
  - configurable generic provider via `conf.auth.oauth.providers.*`
  - state token (CSRF) signed and validated
  - callback handler that exchanges code for tokens, fetches profile
- `internal/app/lahijan/auth/oidc/`:
  - OIDC discovery (`/.well-known/openid-configuration`)
  - ID token verification (signature, nonce, audience, expiry)
  - userinfo endpoint usage
  - configurable via `conf.auth.oidc.providers.*`
- Account linking:
  - new `user_oauth_identities` table (global; user_id, provider, subject,
    access_token (encrypted), refresh_token (encrypted), expires_at, scopes,
    created_at, updated_at)
  - "log in with X" → if no matching identity, propose: create new account,
    or log into existing account first to link
  - "link X to my account" in `/me/identities`
- Identity-provider-scoped logout (optional RP-initiated logout) for OIDC
- API endpoints:
  - `GET /api/v1/auth/oauth/{provider}/start`
  - `GET /api/v1/auth/oauth/{provider}/callback`
  - `GET /api/v1/auth/oidc/{provider}/start`
  - `GET /api/v1/auth/oidc/{provider}/callback`
  - `GET /api/v1/me/identities`, `DELETE /api/v1/me/identities/{id}`
- Config: `conf.auth.oauth.providers.*` and `conf.auth.oidc.providers.*`
  loaded from env for client IDs/secrets.

**Out of scope:**
- SAML 2.0 (WS-07b).
- MFA challenge (WS-07c) — but the OAuth/OIDC login flow must produce the
  same intermediate "pending session" shape that core auth does, so MFA can
  intercept.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0004-auth-methods.md`
- `docs/adr/0017-i18n-from-day-one.md`
- WS-06 doc (the session shape this WS builds on)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- Generic OAuth client + Google + GitHub presets
- Generic OIDC client with discovery + ID token verification
- Account linking flow
- Encrypted token storage
- i18n strings for: "log in with X", "link X to your account", etc.
- Integration tests using a fake OIDC provider (spin up `oidc-server-mock` or
  similar in compose)

## Definition of Done

- [x] register new account via Google
- [x] register new account via GitHub
- [x] register new account via generic OIDC (test provider)
- [x] link an existing account to an external IdP
- [x] unlink an external IdP (with at least one other auth method remaining)
- [x] OAuth/OIDC tokens encrypted at rest
- [x] every login emits an audit event
- [x] every user-facing string through `i18n.T`; en + fa in sync
- [x] `make lint test` green

## Open questions

- Should we support OAuth/OIDC for **tenants** (i.e. tenant-level SSO) or just
  for **users**? (Default: users for MVP; tenant-level SSO is a Phase 7
  candidate.) **Resolved:** users-only for MVP — tenant-level SSO deferred to
  Phase 7. The (provider, subject) pair is globally unique on
  user_oauth_identities, which keeps the door open for tenant-scoped SSO
  later by adding a tenant_id column without breaking existing rows.
- Refresh external OAuth tokens in background or on-demand? (Default:
  on-demand; refresh if expiry < 60s.) **Resolved:** on-demand for MVP. The
  identity row carries expires_at and the IdP service refreshes tokens on
  every callback (UpdateTokens). Background refresh (River job) is a
  follow-up.

## Notes

- Use `golang.org/x/oauth2` + `github.com/coreos/go-oidc` for the heavy
  lifting. License check: both are BSD-3 and Apache-2 respectively.
- PKCE is mandatory for all OAuth flows, even confidential clients.
