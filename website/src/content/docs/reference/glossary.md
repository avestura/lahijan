---
title: Glossary
description: Short definitions of the terms used across Lahijan and its documentation.
---

Terms are listed alphabetically. Names in `code` are the exact identifiers used in the API or configuration.

### .lahx package

The file format for distributing a [plugin](#plugin). A `.lahx` file is a ZIP archive with two entries: the [manifest](#manifest) `lahijan.manifest.yaml` and the compiled WebAssembly module `plugin.wasm`. Build and check one with the `lahx` tool; see [Command line](/docs/reference/cli#lahx) and [Packaging (.lahx)](/docs/plugins/packaging).

### Access key

A pair of S3 credentials (an access key id and a secret key) that lets an S3 client read or write one [bucket](#bucket) directly. In the dashboard it is called a **credential** and is created with **Mint credential**. Each access key is scoped to a single bucket, can be limited to certain actions (Read, Write, List, Tagging, Admin) and can have an expiry. The secret is shown only once. See [Access keys](/docs/storage/credentials).

### Access token

See [Personal access token](#personal-access-token).

### Agent

The chat assistant on the **Agent** page. It turns plain-language requests into actions on Lahijan, using a model provider key you add under **Settings > AI Provider**. The tenant's [agent policy](#agent-policy) limits what it may do, and it can ask you to approve an action before running it. See [Agent](/docs/agent/overview).

### Agent policy

Per-tenant rules for the [agent](#agent): which models may be used, rate and spending limits, and which tools it may not call. Managed under **Administration > Agent Policy**. See [Agent policy](/docs/admin/agent-policy).

### Audit event

One entry in the audit log: who did what to which resource, when, and with what result. Lahijan records the event before it acts and appends the outcome afterwards, so its status is `pending`, `success` or `failure`. Audit events cannot be changed or deleted. See [Audit log](/docs/audit/overview).

### Backup

A full export of an [instance](#instance), stored outside the compute host on a **backup target** (an S3, NFS or SSH destination). Unlike a [snapshot](#snapshot), a backup survives the loss of the host. See [Snapshots and backups](/docs/compute/snapshots-and-backups).

### Balance

The prepaid amount of money a user has available. [Top-ups](#top-up) and refunds increase it; metered usage is charged against it. The balance belongs to a user, not to a tenant. Amounts are stored in whole cents. See [Balance and usage](/docs/billing/overview).

### Bootstrap admin

The first account on a new deployment, created automatically on the first start against an empty database from the `bootstrap.*` settings. It holds the [platform administrator](#platform-administrator) role. See [Install on a server](/docs/getting-started/installation#6-sign-in-as-the-bootstrap-admin).

### Bucket

A container for objects in object storage, reached with any S3-compatible client. A bucket is created in a tenant and identified by its **slug** (lowercase letters, digits and dashes). Bucket names are unique across the platform. See [Buckets](/docs/storage/buckets).

### Console

Interactive access to a running [instance](#instance) from the browser: a text terminal for any instance, and a graphical console for virtual machines. See [Console and exec](/docs/compute/console).

### Container

An [instance](#instance) type that shares the host's kernel. A system container behaves like a lightweight Linux machine (with its own init system, users and services) and starts in seconds. Compare [virtual machine](#virtual-machine).

### DNSSEC

DNS Security Extensions: cryptographic signatures on a [zone's](#zone) records that let resolvers check the answers are genuine. You turn it on per zone, then publish the resulting DS record at your registrar. See [DNSSEC](/docs/dns/dnssec).

### Domain

A domain name you register, renew or transfer through Lahijan, when the operator has connected a domain registrar. Registering a domain is separate from hosting its [zone](#zone). See [Domains](/docs/dns/domains).

### Exec

Running a single command inside an [instance](#instance) and getting its output, without an interactive session. See [Console and exec](/docs/compute/console).

### Floating IP

A public IP address allocated to a tenant from an operator-managed **IP pool**, which you can attach to an [instance](#instance) and later move to another. It stays allocated to the tenant until you release it. See [Floating IPs](/docs/compute/floating-ips).

### Image

The starting file system an [instance](#instance) is created from, such as `ubuntu/24.04`. Images come from the remote image catalog, and you can add custom images to your tenant's catalog. See [Images](/docs/compute/images).

### Instance

A compute unit: either a system [container](#container) or a [virtual machine](#virtual-machine). You create it from an [image](#image) with a size (vCPUs, memory, disk) and start, stop, restart and delete it as you need. See [Instances](/docs/compute/instances).

### Ledger

The append-only record of every change to a user's [balance](#balance): charges, [top-ups](#top-up) and refunds. Entries are never edited or deleted; a correction is a new entry. See [Balance and usage](/docs/billing/overview).

### Linked identity

An external sign-in account (for example from an OAuth, OIDC or SAML provider) connected to your Lahijan account, so you can sign in with it. Managed under **Settings > Identities**. See [Linked identities](/docs/account/identities).

### Manifest

The file `lahijan.manifest.yaml` inside every [.lahx package](#lahx-package). It declares the plugin's name, version, the [permission scopes](#permission-scope) it requests, its configuration schema and its entry points. See [Manifest reference](/docs/plugins/manifest).

### Marketplace

A catalog of plugins that a platform administrator can browse and install from, under **Administration > Marketplace**. See [Plugin marketplace](/docs/admin/marketplace).

### Membership

The link between a user and a [tenant](#tenant), carrying the user's [role](#role) in that tenant. A user can have memberships in several tenants.

### Metering

Measuring the resources each user runs, once a minute. Metered usage is priced with the price catalog and charged to the user's [balance](#balance). See [Balance and usage](/docs/billing/overview).

### Network

A virtual network that connects [instances](#instance) to each other and to the outside world. See [Networks](/docs/compute/networks).

### Permission

A named right to perform one action, written as dotted words such as `compute.instance.create` or `audit.read`. Permissions are grouped into [roles](#role). See [Permissions](/docs/reference/permissions).

### Permission scope

A permission a [plugin](#plugin) requests in its [manifest](#manifest), such as `network.outbound:hooks.slack.com` or `events.listen:compute.instance.*`. The part after the colon narrows the permission to one target; a trailing `*` matches everything under that prefix. A platform administrator approves each scope separately when installing the plugin. See [Manifest reference](/docs/plugins/manifest).

### Personal access token

A secret token that authenticates API requests as you, sent in the `Authorization: Bearer` header. Tokens start with `lah_pat_`. Create and revoke them under **Settings > Access Tokens**. See [Access tokens](/docs/account/access-tokens).

### Personal tenant

The [tenant](#tenant) created automatically for a user who registers on their own, with that user as its owner. Controlled by the `auth.signup.personalTenant` setting (on by default).

### Platform administrator

The global role `platform.admin`, held by the people who run the deployment. It passes every permission check in every tenant and gives access to the **Administration** section of the dashboard. See [Roles and permissions](/docs/admin/roles-and-permissions).

### Plugin

A WebAssembly program that extends a deployment, for example by reacting to events, calling external services or adding API endpoints. Plugins run in a sandbox and can only use the [permission scopes](#permission-scope) an administrator approved. See [How plugins work](/docs/plugins/overview).

### Pre-signed URL

A time-limited URL that lets anyone who has it download (GET) or upload (PUT) one object in a [bucket](#bucket) without their own [access key](#access-key). See [Pre-signed URLs](/docs/storage/presigned-urls).

### Price catalog

The list of unit prices per resource type (for example a price per vCPU-hour) that turns [metered](#metering) usage into charges. Managed under **Administration > Billing**. See [Billing administration](/docs/admin/billing).

### Profile

A reusable set of configuration and devices (such as network interfaces, disks and resource limits) applied to [instances](#instance). An instance can use one or more profiles. See [Profiles](/docs/compute/profiles).

### Quota

A limit on a [bucket](#bucket): a maximum total size in bytes, a maximum number of objects, or both. `0` means unlimited. See [Quotas](/docs/storage/quotas).

### Record

One DNS entry in a [zone](#zone), such as an `A` record for `www.example.org.` or an `MX` record for mail. See [Records](/docs/dns/records).

### Recovery code

A single-use code that stands in for your second factor if you lose your authenticator or passkey. Generated under **Settings > Security**. See [Sign-in security](/docs/account/security).

### Role

A named set of [permissions](#permission). Every tenant has the built-in roles owner (`tenant.owner`), admin (`tenant.admin`), member (`tenant.member`) and viewer (`tenant.viewer`); there is also the global [platform administrator](#platform-administrator) role. See [Roles and permissions](/docs/admin/roles-and-permissions).

### Session

A signed-in browser. Signing in creates a session held in a cookie, renewed with a refresh token. You can see and end your sessions under **Settings > Sessions**. See [Sessions](/docs/account/sessions).

### Snapshot

A point-in-time copy of an [instance](#instance) kept on the same host, which you can restore the instance to. A **snapshot policy** takes snapshots on a schedule. Compare [backup](#backup). See [Snapshots and backups](/docs/compute/snapshots-and-backups).

### Storage pool

Disk space on the compute host from which [instances](#instance) get their root disks and extra volumes. See [Storage pools](/docs/compute/storage).

### Tenant

A workspace that owns resources: instances, zones, buckets, audit events and more. Tenants are isolated from each other; a request picks its tenant with the `X-Tenant-Id` header, and the dashboard with the tenant switcher. See [Core concepts](/docs/getting-started/concepts#tenants-and-memberships).

### Top-up

A credit added to a user's [balance](#balance). A platform administrator can top up any user by hand; if online payments are set up, users can also top up themselves. See [Billing administration](/docs/admin/billing) and [Payments and plans](/docs/billing/payments).

### TOTP

Time-based one-time password: the 6-digit codes from an authenticator app, used as a second sign-in factor. Set up under **Settings > Security > Authenticator app**. See [Sign-in security](/docs/account/security).

### Virtual machine

An [instance](#instance) type with its own kernel, running on hardware virtualization. It is fully isolated from the host and can run any operating system the image provides, at the cost of more memory and a slower start than a [container](#container).

### WebAuthn

The web standard behind passkeys and hardware security keys, usable as a second factor under **Settings > Security > Passkeys / security keys** when the operator has configured it. See [Sign-in security](/docs/account/security).

### X-Tenant-Id

The HTTP request header that tells the API which [tenant](#tenant) a request is for. Its value is the tenant's id (a UUID). See [REST API](/docs/reference/api).

### Zone

The DNS data for one domain (for example `example.org.`) and every name under it, served authoritatively by Lahijan. Zone names are written with a trailing dot and are unique across the platform. See [Zones](/docs/dns/zones).

### Zone template

A predefined set of [records](#record) you can add to a zone in one step, when you create it or later. See [Zone templates](/docs/dns/templates).
