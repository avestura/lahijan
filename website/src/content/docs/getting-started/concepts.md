---
title: Core concepts
description: Tenants and memberships, roles and permissions, the resources in each area, the audit log, billing and plugins.
---

A few ideas run through every part of Lahijan. Read this page once and the guides will make more sense.

## Tenants and memberships

A **tenant** is a workspace that owns resources. Every instance, zone, bucket, access key, audit event and usage record belongs to exactly one tenant. A tenant might be a team, a customer or just you.

A **membership** links a user to a tenant and gives them one role there. One user can be a member of several tenants, with a different role in each. A user who registers on their own gets a **personal tenant** that they own (if the operator keeps that setting on). The first administrator of a new deployment starts in a tenant called **Default Tenant**.

Tenants are strictly isolated. Lahijan keeps all tenants in one database, but every record carries its tenant, and every read and write is filtered by it. Members of one tenant cannot see or change another tenant's resources, and nothing is shared between tenants.

### Choosing the tenant you work in

Because a user can belong to several tenants, every request says which tenant it is for:

- **In the dashboard**, the tenant switcher in the top bar sets the active tenant. It only appears when you belong to more than one tenant. Every page shows and creates resources in the active tenant.
- **In the REST API**, you send the tenant's id in the `X-Tenant-Id` header:

  ```http
  GET /api/v1/compute/instances HTTP/1.1
  Host: app.example.com
  Authorization: Bearer lah_pat_...
  X-Tenant-Id: 3f2a9c1e-5b7d-4e8a-9f10-2c4b6d8e0a12
  ```

  A request without the header, or for a tenant you are not a member of, is refused for any tenant-scoped action. The header is only a hint: Lahijan always checks your membership before it acts. See [REST API](/docs/reference/api).

Some names are unique across the whole platform, not per tenant. For example, two tenants cannot host the same DNS zone.

## Roles and permissions

Every action in Lahijan is guarded by a **permission** with a dotted name, such as `compute.instance.create` or `dns.zone.delete`. A **role** is a named bundle of permissions. Your role in a tenant decides what you can do there.

Every tenant has four built-in roles:

| Role                         | In short                               |
| ---------------------------- | -------------------------------------- |
| **Owner** (`tenant.owner`)   | Full control of the tenant.            |
| **Admin** (`tenant.admin`)   | Manages all of the tenant's resources. |
| **Member** (`tenant.member`) | Creates and manages resources.         |
| **Viewer** (`tenant.viewer`) | Read-only access.                      |

On top of these, the **platform administrator** role (`platform.admin`) is a global role for the people who run the deployment. It passes every permission check in every tenant and gives access to the **Administration** section of the dashboard: prices and balances, plugins, the plugin marketplace and agent policy.

If you try something your role does not allow, the dashboard hides the control or shows a permission message, and the API answers with an error.

For the full picture, see [Roles and permissions](/docs/admin/roles-and-permissions) and the list of every permission in [Permissions](/docs/reference/permissions).

## Resources in each area

| Area           | Main resources                                                                                                           | Guide                                             |
| -------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------- |
| Compute        | Instances (containers and virtual machines), images, profiles, networks, storage pools, snapshots, backups, floating IPs | [Compute overview](/docs/compute/overview)        |
| DNS            | Zones, records, DNSSEC keys, zone templates, domains                                                                     | [DNS overview](/docs/dns/overview)                |
| Object storage | Buckets, access keys, pre-signed URLs, quotas                                                                            | [Object storage overview](/docs/storage/overview) |
| Account        | Your profile, sign-in factors, personal access tokens, linked identities, sessions                                       | [Profile](/docs/account/profile)                  |

Compute, DNS and storage resources belong to a tenant. Your account settings belong to you and follow you across tenants.

Object storage works a little differently from the other two areas. You create buckets and access keys through Lahijan, but your S3 tools then read and write objects by talking to the storage endpoint directly with those keys. Object data never passes through the Lahijan API.

## The audit log

Every privileged action is recorded as an **audit event**: who did it, what they did, which resource it touched, when, and whether it worked. Lahijan records the event before it acts and appends the result afterwards, so a failed or interrupted action still leaves a trace (the status is `pending`, `success` or `failure`).

Audit events cannot be edited or deleted: the database rejects any attempt, whoever makes it. Members with the `audit.read` permission can browse their tenant's events on the **Audit Log** page or through the API, and members with `audit.export` can export them. See [Audit log](/docs/audit/overview).

## Billing

Lahijan bills from a prepaid **balance**, and the balance belongs to you as a user, not to a tenant.

- **Top-ups** add money to your balance. In the basic setup, a platform administrator tops up balances by hand. If the operator has connected a payment provider, you can also pay online and buy plans; see [Payments and plans](/docs/billing/payments).
- **Metering** is designed to measure the resources you run every minute and turn that usage into **charges** through the price catalog. Resource metering is not active yet, so today the balance only changes through top-ups, refunds and payments. See [Balance and usage](/docs/billing/overview).
- The **ledger** is the record of every top-up, charge and refund. Like the audit log, it is append-only: a correction is a new entry (for example a refund), never an edit.

If your balance reaches zero and stays there past a grace period, Lahijan can stop your running instances until you top up. The **Billing** page shows your balance, usage and ledger. See [Balance and usage](/docs/billing/overview).

## Plugins

A **plugin** is a small WebAssembly program that extends a deployment: it can react to events (for example "a bucket was created"), call external services, store its own data, schedule jobs or add API endpoints. Plugins run in a sandbox and can do only what they have permission for.

Each plugin ships as a `.lahx` package containing its **manifest**, which lists the permissions it needs. A platform administrator installs it (by upload or from the marketplace) and approves each permission separately, much like app permissions on a phone. Until a permission is approved, the plugin cannot use it.

As a user you mostly notice plugins through what they do. To install them, see [Installing plugins](/docs/admin/plugins); to write one, see [How plugins work](/docs/plugins/overview).

## Next steps

- [First steps](/docs/getting-started/first-steps): put these ideas into practice.
- [Glossary](/docs/reference/glossary): short definitions of every term.
