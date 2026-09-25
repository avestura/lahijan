---
title: Access keys
description: Mint, list, rotate and revoke the S3 access keys your applications use to read and write objects in a bucket.
---

S3 clients authenticate with an access key and a secret key. In Lahijan you mint these per bucket, choose what each key may do, and revoke keys you no longer need. The dashboard calls them **Credentials** ("Per-app S3 keys scoped to this bucket.").

## How access keys are scoped

- Every access key belongs to **exactly one bucket**. It cannot see or touch any other bucket, even in the same tenant. There are no account-wide keys; mint one key per bucket your application needs.
- Each key carries a set of **actions** that limit what it can do in that bucket.
- The key is tied to the user who minted it and to the current tenant.

Mint a separate key for each application or machine, with a label that says what it is for. That way you can revoke one without breaking the others.

## Actions

| Action      | Allows                                                         |
| ----------- | -------------------------------------------------------------- |
| **Read**    | Download objects.                                              |
| **Write**   | Upload and delete objects.                                     |
| **List**    | List the objects in the bucket.                                |
| **Tagging** | Set and remove object tags.                                    |
| **Admin**   | Every action on the bucket, including bucket-level operations. |

Pick the smallest set that works. Some common choices:

- A backup job that uploads and prunes old files: **Read**, **Write** and **List**.
- A web server that only serves files: **Read** (add **List** if it needs to browse).
- A tool that only inventories the bucket: **List**.

Give **Admin** only when a tool really needs bucket-level control.

## Mint an access key

### In the dashboard

1. Open the bucket and go to the **Credentials** tab.
2. Click **Mint credential**.
3. Enter a **Label**, for example `ci-uploader` (up to 100 characters).
4. Tick one or more **Actions**.
5. Optionally set **Lifetime (seconds)**. Leave it empty for no expiry. See [Expiry](#expiry).
6. Click **Mint**.

The **Credential minted** dialog shows the **Access key** and the **Secret key**. Click **Copy secret**, store it somewhere safe, then click **I've saved it**.

> [!CAUTION]
> The secret key is shown **once**. Lahijan stores only a fingerprint of it and cannot show it again or recover it. If you lose it, revoke the key and mint a new one.

### With the API

`POST /api/v1/storage/buckets/{bucketId}/credentials`

```sh
curl -X POST https://app.example.com/api/v1/storage/buckets/$BUCKET_ID/credentials \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"label": "ci-uploader", "actions": ["Read", "Write", "List"]}'
```

| Field              | Required | Notes                                                        |
| ------------------ | -------- | ------------------------------------------------------------ |
| `actions`          | Yes      | At least one of `Read`, `Write`, `List`, `Tagging`, `Admin`. |
| `label`            | No       | Up to 100 characters.                                        |
| `expiresInSeconds` | No       | Lifetime in seconds, 1 or more. Leave out for no expiry.     |

The response (`201`) contains the key's details and the secret, once:

```json
{
	"credential": {
		"id": "4d7e1b2a-9c3f-4e8d-a1b2-c3d4e5f6a7b8",
		"bucketId": "9a8b7c6d-5e4f-4a3b-2c1d-0e9f8a7b6c5d",
		"tenantId": "0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b",
		"userId": "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
		"accessKeyId": "lahm5xq2k7w3p9r4t6yb",
		"label": "ci-uploader",
		"actions": ["Read", "Write", "List"],
		"lastUsedAt": null,
		"expiresAt": null,
		"createdAt": "2026-09-25T10:00:00Z",
		"revokedAt": null
	},
	"secretKey": "k3n5p7r9t2v4x6z8b1d3f5h7j9l2n4p6r8t1v3x5"
}
```

Access keys start with `lah`. You need `s3.credentials.create`.

## List access keys

The **Credentials** tab shows each active key with its **Access key**, **Label**, **Last used** and **Expires** values. Revoked keys are not listed.

API: `GET /api/v1/storage/buckets/{bucketId}/credentials` (paginated with `limit` and `offset`). Secrets are never returned here. Listing needs only `s3.bucket.read`.

> [!NOTE] > **Last used** is not tracked yet and always shows **None**.

## Expiry

If you set a lifetime, Lahijan records the expiry time and shows it in the **Expires** column.

> [!WARNING]
> Lahijan does not yet revoke keys automatically when they expire, and the S3 endpoint does not enforce the expiry either. A key keeps working after its **Expires** time until you revoke it. Treat the expiry as a reminder and revoke expired keys yourself.

## Revoke an access key

- **Dashboard:** on the **Credentials** tab, click **Revoke** on the key's row, then confirm in the **Revoke credential?** dialog.
- **API:** `DELETE /api/v1/storage/buckets/{bucketId}/credentials/{credentialId}` returns `204`.

The access key stops signing requests immediately. Any client still using it gets access-denied errors. Revoking is permanent; revoking an already revoked key succeeds without doing anything. You need `s3.credentials.revoke`.

Deleting a bucket revokes all of its keys.

## Rotate an access key

Lahijan has no single "rotate" action. Rotate by overlapping two keys:

1. Mint a new key for the same bucket with the same actions.
2. Update your application or client configuration to use the new key and secret.
3. Check that the application works with the new key.
4. Revoke the old key.

Minting, revoking and the actions involved are all recorded in the [audit log](/docs/audit/overview). The secret key never appears in the audit log.
