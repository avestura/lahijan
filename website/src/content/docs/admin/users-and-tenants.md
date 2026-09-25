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

Anyone who can reach the server can create an account with `POST /api/v1/auth/register`:

```sh
curl -X POST https://cloud.example.com/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "sara@example.com", "password": "a-Longer-passphrase-42", "displayName": "Sara"}'
```

With `auth.signup.personalTenant: true` (the default), each new account also gets its own tenant, with the slug `personal-<user id>` and the user's email as its name, and the user becomes its `tenant.owner`. With the setting off, a new account has no membership and cannot create anything until you add it to a tenant.

> [!WARNING]
> There is no setting to turn registration off, and the dashboard has no sign-up page, but the API endpoint is always open. If your server is reachable from the internet, anyone can register. Restrict access at your reverse proxy if you run a private installation.

> [!CAUTION]
> The owner of a personal tenant holds every tenant permission, including `billing.balance.adjust` (credit any balance in that tenant) and `compute.ip_pool.manage` (edit the operator IP pools, which are shared across tenants). Read [Roles and permissions](/docs/admin/roles-and-permissions#tenant-owner) before you open registration.

Accounts created by signing in with an external identity provider (OAuth or OIDC, or SAML with just-in-time creation turned on) get no tenant at all.

## Platform administrator

`platform.admin` is not a global flag. It is a role on a membership, and like any role it applies only in the tenant where it is held. The bootstrap administrator therefore has full powers in the default tenant and none in other tenants unless you give them a membership there.

Within that tenant, `platform.admin` passes every permission check. It is also the only role that holds the `platform.*` permissions, which gate the background job pages.

The dashboard shows the **Administration** section of the sidebar (**Billing**, **Plugins**, **Marketplace**, **Agent Policy**) only while the current tenant's role is `platform.admin`. The job queue's own web page is at `/admin/jobs/ui` when jobs are enabled; see [Background jobs](/docs/admin/jobs).

## What the API does not cover yet

The permission catalog already contains `tenant.member.invite`, `tenant.member.remove`, `tenant.member.role.update`, `platform.user.list`, `platform.tenant.create` and similar permissions, but no endpoints use them yet. In the current release there is no API or dashboard page to:

- list users,
- create or delete tenants (other than the personal tenant made at registration),
- invite a user into a tenant, remove a member or change a member's role,
- deactivate a user,
- require a second factor for a tenant.

The only endpoints under `/api/v1/admin/users/{userId}` are the billing ones described in [Billing administration](/docs/admin/billing).

## Manage members in the database

Until those endpoints exist, you make these changes directly in PostgreSQL. Open a shell on the Lahijan database (the user and database names come from `LAHIJAN_DATABASE_USER` and `LAHIJAN_DATABASE_NAME` in your [environment file](/docs/operations/environment); both are `lahijan` by default):

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

Stop a user from signing in:

```sql
UPDATE users SET is_active = false, updated_at = now() WHERE id = '<user id>';
```

An inactive user cannot sign in with a password or an external identity, but sessions that are already open and personal access tokens keep working until they expire or are revoked.

Require a second factor for everyone in a tenant:

```sql
UPDATE tenants SET mfa_required = true WHERE id = '<tenant id>';
```

Members without an authenticator app or passkey are then refused at sign-in. Read the second-factor limits in [Sign-in security](/docs/account/security#current-limits) before you turn this on.

Role changes take effect on the user's next request. The dashboard reads memberships at sign-in, so the user may need to reload the page to see a new tenant in the switcher.
