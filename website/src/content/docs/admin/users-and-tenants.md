---
title: Users and tenants
description: How users, tenants and memberships are created in Lahijan, what the platform administrator is, and how to manage members today.
---

This page is for operators. It explains how Lahijan organises people into tenants, how accounts and tenants come into existence, and what you can and cannot manage in the current release.

## The model

- A **user** is a global account: one email address, one password (optional), any number of linked sign-in identities.
- A **tenant** is an isolated space that owns resources: instances, DNS zones, buckets, balances, audit events. Every resource row carries its tenant's id, and every query is filtered by it.
- A **membership** connects one user to one tenant with exactly one **role**. A user can belong to many tenants, with a different role in each.

The role decides what the user may do in that tenant. The built-in roles are `tenant.owner`, `tenant.admin`, `tenant.member`, `tenant.viewer` and `platform.admin`; see [Roles and permissions](/docs/admin/roles-and-permissions).

In the dashboard, the tenant switcher in the header lists the tenants the signed-in user belongs to. API clients choose a tenant with the `X-Tenant-Id` or `X-Tenant-Slug` header on each request.

## The first administrator

On the first start against an empty database, Lahijan creates:

1. A tenant with the slug `default`, named "Default Tenant".
2. A user with the email in `bootstrap.adminEmail` (in the production stack, set `LAHIJAN_BOOTSTRAP_ADMIN_EMAIL` in the environment file) and the display name in `bootstrap.adminDisplayName`.
3. A membership that gives this user the `platform.admin` role in the default tenant.

If `bootstrap.adminPassword` (`LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD` in the environment file) is empty, Lahijan generates a random password of `bootstrap.generatedPasswordLength` characters (24 by default) and prints it once to the log:

```sh
docker compose -f docker-compose.prod.yml logs lahijan
```

Change that password right after your first sign-in. The bootstrap runs only when the database has no users at all, so it never runs again once any account exists. It is skipped when `bootstrap.enabled` is `false` or `bootstrap.adminEmail` is empty. See [Configuration](/docs/reference/configuration).

## Self-registration

By default anyone who can reach the server can create an account with `POST /api/v1/auth/register`. You can turn this off; see [Turn registration off](#turn-registration-off).

```sh
curl -X POST https://cloud.example.com/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "sara@example.com", "password": "a-Longer-passphrase-42", "displayName": "Sara"}'
```

With `auth.signup.personalTenant: true` (the default), each new account also gets its own tenant, with the slug `personal-<user id>` and the user's email as its name, and the user becomes its `tenant.owner`. With the setting off, a new account has no membership and cannot create anything until you add it to a tenant.

> [!WARNING]
> Registration is open by default and the API endpoint answers anyone who can reach the server. On a private installation, turn it off before you expose the server.

> [!CAUTION]
> The owner of a personal tenant holds every tenant permission, including `billing.balance.adjust` (credit any balance in that tenant) and `compute.ip_pool.manage` (edit the operator IP pools, which are shared across tenants). Read [Roles and permissions](/docs/admin/roles-and-permissions#tenant-owner) before you open registration.

Accounts created by signing in with an external identity provider (OAuth or OIDC, or SAML with just-in-time creation turned on) get no tenant at all.

### Turn registration off

Registration has a configured default and a switch in the dashboard:

- **The environment variable.** Set `LAHIJAN_AUTH_SIGNUP_ENABLED=false` in your [environment file](/docs/operations/environment) (the config key is `auth.signup.enabled`) and recreate the `lahijan` service. This is the starting value.
- **The dashboard switch.** A platform administrator opens **Administration > Settings** and clears **Allow people to register**. It takes effect at once, without a restart, and it **wins over the environment variable** until you select **Use the configured default**. The page shows both values and which one is in force.

While registration is off:

- `POST /api/v1/auth/register` answers `403` with the error code `registration_disabled`.
- Signing in with an OAuth, OIDC or SAML identity that has no account yet no longer creates one; it is refused with the same `403`. People who already have an account, or who linked the identity to it, still sign in.
- Accounts an administrator creates under **Administration > Users**, and accounts imported from [LDAP](/docs/admin/directories), are not affected.

Changing the switch needs the `platform.settings.manage` permission, which only `platform.admin` holds, and is written to the [audit log](/docs/audit/overview). The same setting is available at `GET` and `PUT /api/v1/admin/settings`.

## Platform administrator

`platform.admin` is not a global flag. It is a role on a membership, and like any role it applies only in the tenant where it is held. The bootstrap administrator therefore has full powers in the default tenant and none in other tenants unless you give them a membership there.

Within that tenant, `platform.admin` passes every permission check. It is also the only role that holds the `platform.*` permissions, which gate the background job pages, user management and directory connections.

The dashboard shows the **Administration** section of the sidebar (**Users**, **Directories**, **Settings**, **Billing**, **Plugins**, **Marketplace**, **Agent Policy**) only while the current tenant's role is `platform.admin`. The job queue's own web page is at `/admin/jobs/ui` when jobs are enabled; see [Background jobs](/docs/admin/jobs).

## Manage users in the dashboard

Platform administrators manage accounts at **Administration > Users** (`/admin/users`). The page lists every user on the platform, 15 per page, and you can search by email or display name. Each row shows the user's status, role in each tenant, where the account came from (a local account, or the name of the [directory](/docs/admin/directories) it was imported from) and when it was created.

| Action       | What it does                                                                                                                                                                                                                                           |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **New user** | Creates an account. The email address is marked verified and the user gets a personal tenant (when `auth.signup.personalTenant` is on). A password is optional; without one the user signs in through single sign-on, a directory or a password reset. |
| **Edit**     | Changes the display name, turns sign-in on or off, and changes the user's role in each tenant they belong to.                                                                                                                                          |
| **Delete**   | Removes the account (soft delete). The user is signed out at once and cannot sign in again.                                                                                                                                                            |

Disabling or deleting a user ends their open sessions and stops their [access tokens](/docs/account/access-tokens). You cannot disable or delete your own account.

The same actions are available in the API under `/api/v1/admin/users` (needs `platform.user.list` to read and `platform.user.manage` to change). Every change is written to the [audit log](/docs/audit/overview).

## What the dashboard does not cover yet

In the current release there is no API or dashboard page to:

- create or delete tenants (other than the personal tenant made at registration),
- add a user to a tenant, invite one, or remove a member (you can change the role of an existing membership),
- require a second factor for a tenant.

## Manage members in the database

For the changes the dashboard does not cover, you work directly in PostgreSQL. Open a shell on the Lahijan database (the user and database names come from `LAHIJAN_DATABASE_USER` and `LAHIJAN_DATABASE_NAME` in your [environment file](/docs/operations/environment); both are `lahijan` by default):

```sh
docker compose -f docker-compose.prod.yml exec postgres psql -U lahijan -d lahijan
```

> [!WARNING]
> Changes made in SQL are not written to the audit log and bypass Lahijan's checks. Take a [backup](/docs/operations/backups) first and keep your own record of what you changed.

Find a user and a tenant:

```sql
SELECT id, email, is_active FROM users WHERE deleted_at IS NULL ORDER BY created_at;
SELECT id, slug, name FROM tenants WHERE deleted_at IS NULL ORDER BY created_at;
```

Add a user to a tenant as a member (use `tenant.viewer`, `tenant.admin` or `tenant.owner` for other roles):

```sql
INSERT INTO memberships (tenant_id, user_id, role_id)
VALUES ('<tenant id>', '<user id>', (SELECT id FROM roles WHERE slug = 'tenant.member'));
```

Change a member's role:

```sql
UPDATE memberships
SET role_id = (SELECT id FROM roles WHERE slug = 'tenant.admin'), updated_at = now()
WHERE tenant_id = '<tenant id>' AND user_id = '<user id>' AND deleted_at IS NULL;
```

Remove a member (memberships are soft-deleted):

```sql
UPDATE memberships SET deleted_at = now()
WHERE tenant_id = '<tenant id>' AND user_id = '<user id>' AND deleted_at IS NULL;
```

Stop a user from signing in (prefer **Edit** in the dashboard, which also ends their open sessions):

```sql
UPDATE users SET is_active = false, updated_at = now() WHERE id = '<user id>';
```

An inactive user cannot sign in with a password or an external identity and their access tokens stop working, but sessions that are already open keep working until they expire or are revoked.

Require a second factor for everyone in a tenant:

```sql
UPDATE tenants SET mfa_required = true WHERE id = '<tenant id>';
```

Members without an authenticator app or passkey are then refused at sign-in. Read the second-factor limits in [Sign-in security](/docs/account/security#current-limits) before you turn this on.

Role changes take effect on the user's next request. The dashboard reads memberships at sign-in, so the user may need to reload the page to see a new tenant in the switcher.
