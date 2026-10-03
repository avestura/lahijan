# ADR-0046: Directory connections (LDAP / SAML), platform user management, enforced PAT scopes

- **Status:** Accepted
- **Date:** 2026-10-03
- **Deciders:** maintainer
- **Related:** ADR-0004 (auth), ADR-0002 (tenancy), WS-06, WS-07b, WS-08

## Context

Platform administrators had no UI or API to see or manage users, and no way to
connect an external directory. SAML providers could only be declared in the
config file (restart required), and LDAP was not supported at all. Separately,
personal access tokens stored `scopes` but nothing enforced them, so a token
issued with a narrow scope carried all of its owner's permissions.

## Decision

- **User management.** New platform-admin API and dashboard pages
  (`/api/v1/admin/users`, `/admin/users`): list (paginated, searchable), create,
  edit (display name, sign-in on/off), delete (soft), change the role in a
  tenant. Guarded by two new `platform.*` permissions, `platform.user.list`
  (read) and `platform.user.manage` (write), which stay platform-admin only. An
  admin cannot disable or delete themselves. A user created this way has a
  verified email and gets the same signup provisioning as self-registration; a
  missing password means the user signs in through SSO, a directory or a
  password reset.
- **Directory connections.** A platform-level (global, no `tenant_id`) table
  `directory_connections` plus `directory_groups`, `directory_group_members` and
  `directory_user_links` (migration 0050), guarded by
  `platform.directory.manage`:
  - **LDAP**: connection settings + a bind password sealed with the existing
    AES-GCM envelope. _Test_ binds and runs the user / group searches. _Sync_
    imports users (matched by email, never overwritten; new users have no
    password and a verified email) and groups with their members. Sync is
    non-destructive: users that disappear from the directory are left as they
    are, and groups no longer present are removed.
  - **SAML**: IdP metadata (URL or inline) and attribute names. _Test_ fetches
    and parses the metadata. An enabled connection is registered in the sign-in
    registry **at runtime** (no restart); the registry became concurrency-safe
    (`Set` / `Remove`). The connection name is the provider key in the sign-in
    URL, and may not shadow a provider defined in the config file. After each
    SAML sign-in, the user's source and the groups carried in the configured
    groups attribute are recorded.
  - SP signing credentials remain process-wide and come from config; they are
    loaded lazily and shared so config-defined and admin-defined providers
    publish the same SP metadata. If they are missing, the connection saves but
    reports why it is not active.
- **New dependency:** `github.com/go-ldap/ldap/v3` (MIT) for LDAP, with its
  transitive `go-asn1-ber/asn1-ber` and `Azure/go-ntlmssp` (both MIT). Pure
  Go, no CGO, matches the "no CGO" stack constraint. Adding it also bumped
  `golang.org/x/{crypto,sys,text,sync}` to the minimums it requires.
- **PAT scopes are enforced.** `RequirePerm` now rejects a request authenticated
  by a personal access token whose scope list is non-empty and does not contain
  the required permission slug. A token with **no** scopes keeps its owner's
  permissions (unchanged behaviour). The dashboard offers a checklist built from
  a new `GET /api/v1/permissions` catalogue.
- **Tokens die with their owner.** `pat.Service.Authenticate` can now vet the
  owner (`WithUserChecker`); the bootstrap wires it so a disabled or deleted
  user's tokens stop working. Disabling or deleting a user from the admin API
  also revokes their sessions.
- **LDAP sign-in.** While an LDAP connection is enabled, its users sign in with
  their directory email and password. `session.Service` gained an optional
  `ExternalAuthenticator` hook: a correct local password still wins without any
  network call; otherwise the directory is tried (search for the one entry with
  that email as the connection's service account, then bind as that entry on a
  separate connection). The linked account is used, or one is created on first
  sign-in unless the connection turns that off (`createUsersOnLogin`); empty
  passwords are never sent (an empty bind password would succeed anonymously)
  and a directory password never overwrites a local one. New directory accounts
  get the same personal-tenant provisioning as self-registration.
- **Registration can be turned off.** `auth.signup.enabled`
  (`LAHIJAN_AUTH_SIGNUP_ENABLED`, default true) is the default; an
  administrator's choice in **Administration > Settings** is stored in the new
  `platform_settings` table (migration 0051) and overrides it until reset. It
  gates the register endpoint and the creation of new accounts from OAuth, OIDC
  and SAML sign-ins (`403 registration_disabled`); accounts created by an
  administrator or imported from LDAP are never blocked. Guarded by
  `platform.settings.manage`.
- **Provider connection test** for the agent's BYOK model providers
  (`POST /api/v1/agent/providers/test`): lists models, falls back to a one-token
  completion, and never returns the upstream body.

## Consequences

- Existing scoped tokens now actually lose access outside their scopes. This is
  the intended meaning of "scoped" but is a behaviour change worth a release
  note.
- LDAP sign-in matches by email address and trusts the directory's email
  attribute: whoever authenticates there with an email can sign in to the
  account with that email. Connect only directories you control.
- Group data is informational: it is not yet mapped to roles. Mapping groups to
  tenant roles is a follow-up.
- A SAML connection fetches IdP metadata over HTTP when it is saved or
  activated; at boot this happens in the background so a slow IdP cannot delay
  startup.
- Audit events are emitted after the action with its outcome, matching the
  agent module, rather than before it.

## Alternatives considered

- **Config-file only for SAML / LDAP.** Rejected: needs a restart and shell
  access for every change; the point is self-service for administrators.
- **Per-tenant connections.** Rejected for now: users are global in Lahijan
  (ADR-0002), so a directory that creates users is a platform concern.
- **Disabling users that vanish from LDAP on sync.** Rejected as a default:
  a transient directory problem would lock people out. It can be added as an
  explicit option.
