---
title: Roles and permissions
description: How Lahijan decides who may do what, the built-in roles and what each one grants, and how permission names are formed.
---

Every privileged request in Lahijan is checked against one permission, in one tenant, for one user. This page explains how that check works and what the built-in roles grant. The complete list of permissions is in the [Permissions reference](/docs/reference/permissions).

## Permission names

A permission is a string of the form `scope.action`:

- The first part is the module: `compute`, `dns`, `s3` (object storage), `billing`, `plugins`, `agent`, `audit`, `rbac`, `tenant`, `auth` or `platform`.
- The rest names the target and the verb, for example `instance.create`, `zone.delete` or `bucket.lifecycle`.

Examples: `compute.instance.start`, `dns.record.update`, `s3.credentials.revoke`, `billing.balance.adjust`, `agent.policy.manage`.

The same names appear in the audit log's metadata, in `403` error responses (`error.details.permission`) and in the scopes field of access tokens.

## How a request is checked

1. **Who.** Lahijan identifies the user from the session cookie or from an `Authorization: Bearer` access token. No valid credential means `401`.
2. **Where.** The tenant comes from the `X-Tenant-Id` header (or `X-Tenant-Slug`, or a `tenant_id` query parameter for the browser console connection). No tenant means `400` with `tenant_scope_required`.
3. **What.** Each route maps to exactly one permission. Lahijan looks up the user's membership in that tenant and the permissions of its role. A user with no membership in the tenant is refused. A missing permission means `403` with `forbidden`.

A user whose role in the tenant is `platform.admin` passes every check in that tenant. That is the only bypass.

Some endpoints only need a signed-in user and no permission, because they act on the caller's own account: profile, password, access tokens, second factors and linked identities (`/api/v1/auth/me`, `/api/v1/auth/personal-access-tokens`, `/api/v1/me/mfa/*`, `/api/v1/me/identities`). `GET /api/v1/billing/config`, `GET /api/v1/billing/plans` and the Stripe webhook are not permission-gated.

When the [Agent](/docs/agent/overview) looks something up for a user, the lookup is checked against that user's permissions in the same way.

> [!NOTE]
> Access token scopes are stored but not enforced in this release. A token has the full permissions of its owner's role in whichever tenant it addresses.

The dashboard hides or disables buttons based on an approximate copy of the role grants. The server check is the one that counts; a button that is visible can still return `403`.

## Built-in roles

Lahijan seeds five roles at startup. They are marked as system roles.

| Role             | Name                   | Intended for                                                                                                                      |
| ---------------- | ---------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `platform.admin` | Platform Administrator | The operator. Bypasses every permission check in the tenant where it is held, and is the only role with `platform.*` permissions. |
| `tenant.owner`   | Tenant Owner           | Full control of a tenant.                                                                                                         |
| `tenant.admin`   | Tenant Administrator   | Manages resources, billing and plugins in a tenant.                                                                               |
| `tenant.member`  | Tenant Member          | Creates and runs resources, but deletes few of them.                                                                              |
| `tenant.viewer`  | Tenant Viewer          | Read-only access.                                                                                                                 |

### Tenant owner

`tenant.owner` holds every permission in the catalog except those starting with `platform.`. That includes some permissions meant for operators:

- `billing.balance.adjust`, `billing.price_catalog.update`, `billing.plan.manage` and `billing.promo_code.manage`: an owner can credit any balance in the tenant, including their own.
- `compute.ip_pool.manage`: the IP pools are shared by all tenants, so an owner can change pools other tenants use.
- `audit.read_global` (not used by any endpoint yet).

> [!CAUTION]
> Every self-registered user owns a personal tenant (see [Users and tenants](/docs/admin/users-and-tenants#self-registration)). On an installation with open registration, remember that every new account gets these owner permissions, and review who can register.

### Tenant administrator

`tenant.admin` holds everything a member has, plus:

- Deleting instances, DNS zones and buckets, creating networks, and removing domains.
- Snapshot deletion, backup targets, backup deletion and snapshot schedules.
- Evacuating and restoring compute cluster members.
- Bucket versioning, lifecycle rules and object lock.
- Billing administration: balance top-ups and refunds, the price catalog, receipts, plans, promo codes and the webhook log.
- Installing, removing and approving plugins.
- The agent policy (`agent.policy.manage`).
- Exporting the audit log.
- The tenant, member and role management permissions (not used by any endpoint yet).

It does not hold `tenant.delete`, `audit.read_global`, `compute.ip_pool.manage` or any `platform.*` permission.

### Tenant member

`tenant.member` can:

- Create, change, start, stop and restart instances, open the text and graphical consoles, apply profiles, take and read snapshots, restore a snapshot, read backups, list cluster members and migrate instances, and manage floating IPs. Members cannot delete instances, create networks or delete snapshots.
- Create and update DNS zones, and create, update and delete records. Members cannot delete zones. Search, register, renew and transfer domains, but not remove them.
- Create, read and update buckets, create pre-signed URLs, and create and revoke access keys. Members cannot delete buckets or change versioning, lifecycle or object lock.
- Read their balance, ledger, receipts and the price catalog; manage their own cards and subscriptions, top up by card and redeem promo codes.
- Read the audit log and the plugin list.
- Use the Agent and manage their own provider keys.

### Tenant viewer

`tenant.viewer` can read instances, profiles, images, networks, storage pools, snapshots, backups, backup targets, snapshot schedules, cluster members, floating IPs, DNS zones, records and domains (including domain search), buckets and objects, balances, ledgers, receipts, the price catalog, plans, plugins, the audit log and Agent conversations. It cannot change anything.

The exact grants of every role are in the [Permissions reference](/docs/reference/permissions).

## Custom roles

The catalog contains `rbac.role.create`, `rbac.role.update`, `rbac.role.delete` and `rbac.role.list`, but there are no endpoints for managing roles yet.

Roles are global: a role means the same thing in every tenant. The role table is re-seeded at every start, which adds any missing permission back to the built-in roles, so removing a grant from a built-in role in the database does not last. If you need a narrower role today, create a new row in `roles`, give it permissions in `role_permissions`, and point memberships at it (see [Users and tenants](/docs/admin/users-and-tenants#manage-members-in-the-database)). Keep such roles to a minimum until role management is supported.
