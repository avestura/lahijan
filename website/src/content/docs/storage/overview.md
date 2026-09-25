---
title: Object storage overview
description: How Lahijan object storage works, where your data goes, how to find the S3 endpoint and which permissions you need.
---

Lahijan object storage gives you S3-compatible buckets. You create buckets and access keys in Lahijan, then read and write objects with any S3 tool or SDK. This page explains how the pieces fit together.

In the dashboard, object storage lives under **Object Storage** ("S3-compatible buckets you can write to directly."). The REST API is under `/api/v1/storage/buckets`.

## How it works

Object storage has two paths:

- **Control plane (through Lahijan).** Creating and deleting buckets, minting and revoking access keys, generating pre-signed URLs and setting quotas all go through the Lahijan dashboard or API. Lahijan checks your permissions and records each action in the [audit log](/docs/audit/overview).
- **Data plane (direct).** Object uploads and downloads go straight from your S3 client, script or browser to the platform's **S3 endpoint**. The bytes never pass through the Lahijan API.

So a typical workflow is:

1. Create a bucket (see [Buckets](/docs/storage/buckets)).
2. Mint an access key for it (see [Access keys](/docs/storage/credentials)).
3. Point your S3 client at the S3 endpoint with that key (see [Using S3 clients](/docs/storage/s3-clients)).

For one-off sharing without a key, you can generate a time-limited [pre-signed URL](/docs/storage/presigned-urls).

## Buckets and names

Each bucket has two names:

| Name               | Example                                        | Where it is used                                                                        |
| ------------------ | ---------------------------------------------- | --------------------------------------------------------------------------------------- |
| **Slug**           | `backups`                                      | The short name you choose. Shown in the dashboard.                                      |
| **Canonical name** | `0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups` | Your tenant ID, a dash, then the slug. This is the real bucket name on the S3 endpoint. |

Always use the **canonical name** in S3 clients and SDKs. The dashboard shows it as the page title of each bucket and as **Canonical name** on the **Overview** and **Connection** tabs.

Buckets belong to the tenant you are working in. Other tenants cannot see them, and access keys are limited to a single bucket.

## Find the S3 endpoint

The S3 endpoint is the address your S3 clients connect to. It is set by your operator and is the same for every bucket on the installation.

1. Open a bucket and go to the **Connection** tab ("Connect to this bucket").
2. Click **Reveal endpoint**. The **S3 endpoint** appears, for example `https://s3.example.com`.

The tab also shows ready-to-copy snippets for **s3cmd** and **aws CLI / boto3**.

> [!NOTE]
> The endpoint shown is the address pre-signed URLs are signed for. If it is an internal address (a short container name or a private IP address), your operator has not set a public S3 address, and clients outside the server's own network cannot reach it. Ask your operator for the public endpoint.

The endpoint uses **path-style** addressing: `https://s3.example.com/<canonical-bucket-name>/<object-key>`. See [Using S3 clients](/docs/storage/s3-clients) for client settings.

## Permissions

Every control-plane action is checked against your role in the tenant.

| Permission              | Allows                                                                                                         |
| ----------------------- | -------------------------------------------------------------------------------------------------------------- |
| `s3.bucket.read`        | List and view buckets, see usage, list access keys, browse objects in the dashboard.                           |
| `s3.bucket.create`      | Create buckets.                                                                                                |
| `s3.bucket.update`      | Change a bucket's label or description, set quotas.                                                            |
| `s3.bucket.delete`      | Delete buckets.                                                                                                |
| `s3.credentials.create` | Mint access keys.                                                                                              |
| `s3.credentials.revoke` | Revoke access keys.                                                                                            |
| `s3.object.read`        | Generate pre-signed URLs, both download (`GET`) and upload (`PUT`), including dashboard uploads and downloads. |

Note that `s3.object.read` covers upload URLs too: anyone who can generate pre-signed URLs for a bucket can create URLs that write to it.

With the default roles:

- **Tenant owner** and **tenant admin** have all of the above.
- **Tenant member** has everything except `s3.bucket.delete`.
- **Tenant viewer** has `s3.bucket.read` and `s3.object.read`.

See [Roles and permissions](/docs/admin/roles-and-permissions) and the [Permissions reference](/docs/reference/permissions).

What an S3 client can do with an access key is a separate question, set by the actions you choose when minting the key (see [Access keys](/docs/storage/credentials#actions)).

## Using the API

API requests need an access token and the tenant ID:

```sh
curl https://app.example.com/api/v1/storage/buckets \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID"
```

Create a token under **Settings > Access Tokens** (see [Access tokens](/docs/account/access-tokens)). List endpoints accept `limit` (default 50, maximum 200) and `offset`.

If the operator has not enabled object storage, every storage endpoint returns `501` with code `not_implemented`, and the dashboard shows "This feature is not enabled".
