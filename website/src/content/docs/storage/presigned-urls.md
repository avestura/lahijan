---
title: Pre-signed URLs
description: Generate time-limited URLs that let anyone download or upload a single object without an access key.
---

A pre-signed URL grants temporary access to one object. Anyone who has the URL can download the object (a `GET` URL) or upload to that key (a `PUT` URL) until the URL expires, without an access key or a Lahijan account. Use them to share a file, or to let a browser or another service upload a file straight to your bucket.

## Generate a URL in the dashboard

1. Open the bucket and go to the **Pre-signed URLs** tab ("Issue time-limited direct-access URLs.").
2. Choose the **Method**: `GET` to download, `PUT` to upload.
3. Enter the **Object key**, for example `reports/2026-09.pdf`.
4. Set **Lifetime (seconds)**, from 1 to 86400 (24 hours). The default is 3600 (1 hour).
5. Click **Generate URL**.

The result shows the **Pre-signed URL** and when it **Expires at**. Click **Copy URL** to copy it.

> [!WARNING]
> Anyone with this URL can perform the signed action until it expires. Share it only with the people or systems that need it, and keep lifetimes short.

## Generate a URL with the API

`POST /api/v1/storage/buckets/{bucketId}/presign`

```sh
curl -X POST https://app.example.com/api/v1/storage/buckets/$BUCKET_ID/presign \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"method": "GET", "key": "reports/2026-09.pdf", "expiresInSeconds": 900}'
```

| Field              | Required | Notes                                                                                 |
| ------------------ | -------- | ------------------------------------------------------------------------------------- |
| `method`           | Yes      | `GET` (download) or `PUT` (upload).                                                   |
| `key`              | No       | The object key. Leave it out or empty to target the bucket root instead of an object. |
| `expiresInSeconds` | No       | 1 to 86400. Defaults to 3600 when left out.                                           |

Response (`200`):

```json
{
	"url": "https://s3.example.com/0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups/reports/2026-09.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=900&...",
	"method": "GET",
	"bucket": "0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups",
	"key": "reports/2026-09.pdf",
	"expiresAt": "2026-09-25T10:15:00Z"
}
```

The URL points at the platform's S3 endpoint and uses standard AWS Signature Version 4 query parameters, so any HTTP client can use it.

## Use a URL

Download with a `GET` URL:

```sh
curl -o 2026-09.pdf "$PRESIGNED_URL"
```

Upload with a `PUT` URL:

```sh
curl -X PUT --upload-file ./2026-09.pdf "$PRESIGNED_URL"
```

Always quote the URL in the shell, because it contains `&` characters. A `PUT` URL writes to exactly the key it was signed for; uploading replaces any existing object with that key.

To upload from a web page, send the file with an HTTP `PUT` request to the URL. Browser requests to the S3 endpoint are only allowed from the dashboard's own address (the CORS rule Lahijan adds to each bucket), so browser uploads from other websites are blocked. Uploads from servers, scripts and command-line tools are not affected.

## Limits and behavior

- **Lifetime:** at most 24 hours. A larger value returns `400 bad_request` with `presign ttl must be between 1 second and 24 hours`. For longer-lived access, use an [access key](/docs/storage/credentials).
- **Method:** only `GET` and `PUT`. Anything else returns `presign method must be GET or PUT`.
- **One object per URL.** To share many files, generate one URL per file, or use an access key.
- **No early cancel.** A pre-signed URL is not tied to your access keys, so revoking keys does not cancel it. It stays valid until it expires. Deleting the object makes a `GET` URL useless; deleting the bucket stops both kinds.

## Permissions and audit

Generating any pre-signed URL, `GET` or `PUT`, needs the `s3.object.read` permission. The default **Tenant viewer** role has it, so viewers can create upload URLs too. Keep that in mind when you give people viewer access.

Every URL you generate is recorded in the [audit log](/docs/audit/overview) with the method, key and lifetime, so you can see who issued access to which object and when.
