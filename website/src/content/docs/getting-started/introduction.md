---
title: Introduction
description: What Lahijan offers the people who use it, how you work with it, and how users, tenant admins and platform admins differ.
---

Lahijan is a self-hosted cloud platform. An organization runs it on its own servers, and the people it serves use it to create and manage infrastructure without asking anyone to set it up by hand. This page explains what you can do with a Lahijan deployment and who does what.

## What you can do

Every resource you create belongs to a **tenant**, a shared workspace that isolates your resources from everyone else's. See [Core concepts](/docs/getting-started/concepts) for how tenants work.

### Compute

Create **instances**: either lightweight system containers or full virtual machines. You pick an image (for example `ubuntu/24.04`), a size (vCPUs, memory and disk) and optionally a profile, then start, stop, restart and delete the instance as you need. From an instance's page you can open a console, run commands, take snapshots and backups, and attach a floating IP if your operator has set up a pool of public addresses. Power users also get profiles, networks and storage pools. See the [Compute overview](/docs/compute/overview).

### DNS

Host the authoritative DNS for domains you own. You create a **zone** for a domain, add records, and optionally sign the zone with DNSSEC. Zone templates let you add a common set of records in one step, and if your operator has turned it on, you can search for and register domain names. See the [DNS overview](/docs/dns/overview).

### Object storage

Create S3-compatible **buckets** and mint **access keys** scoped to a bucket. Your S3 tools and code talk to the storage endpoint directly with those keys, so uploads and downloads do not pass through Lahijan. You can also create pre-signed URLs to share a single object for a limited time, and set size and object-count quotas. See the [Object storage overview](/docs/storage/overview).

### Billing

Lahijan uses a prepaid balance. An administrator tops up your balance. Charging for the resources you run is planned: per-minute resource metering is not active yet. The **Billing** page shows your balance, usage and a ledger of every charge, top-up and refund. See [Balance and usage](/docs/billing/overview).

### Audit log

Every privileged action in a tenant (creating an instance, deleting a zone, minting a key, and so on) is recorded in an audit log that cannot be edited or deleted. The **Audit Log** page lets you browse and filter it. See [Audit log](/docs/audit/overview).

### Agent

The **Agent** page is a chat assistant that can act on Lahijan for you in plain language, for example "list my stopped instances". You connect your own model provider key under **Settings > AI Provider**, and your administrators decide which actions the agent may take. See [Agent](/docs/agent/overview).

### Plugins

Administrators can extend a deployment with plugins: small WebAssembly programs that react to events, call external services or add API endpoints. Each plugin declares the permissions it needs, and an administrator approves them when installing it. See [How plugins work](/docs/plugins/overview).

## How you use Lahijan

There are two ways in, and they reach the same features:

- **The dashboard**, a web application at the address your operator gives you. You sign in with your email and password and work from the sidebar: **Dashboard**, **Instances**, **DNS**, **Object Storage**, **Billing**, **Agent** and **Audit Log**, plus your account pages under **Settings**.
- **The REST API**, under `/api/v1/` on the same address. Scripts and other programs authenticate with a personal access token and choose a tenant with the `X-Tenant-Id` header. See [REST API](/docs/reference/api) and [Access tokens](/docs/account/access-tokens).

For object data, S3 clients connect to the storage endpoint directly with the access keys you mint. See [Using S3 clients](/docs/storage/s3-clients).

## Who does what

Lahijan separates three kinds of people. The same person can be more than one.

| Who                | What they do                                                                                                                                                                                            | Where to read                                                         |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| **User**           | A member of one or more tenants. Creates and manages compute, DNS and storage resources within the limits of their role.                                                                                | Guides, Account                                                       |
| **Tenant admin**   | Holds the admin or owner role in a tenant. Manages all of the tenant's resources, including those other members created.                                                                                | [Roles and permissions](/docs/admin/roles-and-permissions)            |
| **Platform admin** | Holds the platform administrator role across the whole deployment. Tops up balances, sets prices, installs plugins and sets agent policy. Sees the **Administration** section in the dashboard sidebar. | Administration                                                        |
| **Operator**       | Installs and runs the deployment itself: the servers, the containers, TLS, backups and upgrades. Often the same person as the first platform admin.                                                     | [Install on a server](/docs/getting-started/installation), Operations |

What you can do inside a tenant depends on your role. The built-in roles are owner, admin, member and viewer; see [Core concepts](/docs/getting-started/concepts#roles-and-permissions).

## Next steps

- New to the platform: read [Core concepts](/docs/getting-started/concepts).
- Just got an account: follow [First steps](/docs/getting-started/first-steps).
- Want to try Lahijan on your own machine: see the [Quickstart](/docs/getting-started/quickstart).
