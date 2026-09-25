---
title: Buckets
description: Create, browse, update and delete object storage buckets, and upload and download objects from the dashboard.
---

A bucket is a container for objects (files). This page covers creating buckets, the tabs on a bucket's page, working with objects in the dashboard, and deleting buckets.

## Create a bucket

### In the dashboard

1. Open **Object Storage** and click **New bucket**.
2. Enter a **Slug**, for example `backups`. See the [slug rules](#slug-rules) below.
3. Optionally enter a **Label** (a display name, up to 100 characters) and a **Description**.
4. Optionally set **Size quota (bytes)** and **Object count quota**. `0` means unlimited. See [Quotas](/docs/storage/quotas).
5. Click **Create bucket**.

The bucket's real name on the S3 endpoint is its **canonical name**: your tenant ID, a dash, then the slug (for example `0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups`). Use the canonical name in S3 clients.

### With the API

`POST /api/v1/storage/buckets`

```sh
curl -X POST https://app.example.com/api/v1/storage/buckets \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"slug": "backups", "label": "Nightly backups", "quotaBytes": 0, "quotaObjects": 0}'
```

| Field          | Required | Notes                                               |
| -------------- | -------- | --------------------------------------------------- |
| `slug`         | Yes      | See the rules below.                                |
| `label`        | No       | Up to 100 characters.                               |
| `description`  | No       | Free text.                                          |
| `quotaBytes`   | No       | Size limit in bytes. `0` (default) means unlimited. |
| `quotaObjects` | No       | Object count limit. `0` (default) means unlimited.  |

The response (`201`) is the bucket, including `id`, `name` (the canonical name), `slug`, `ownerId`, the quota fields and `bytesUsed` / `objectsUsed`.

### Slug rules

A slug must:

- be 1 to 26 characters long,
- use only lowercase letters, digits and dashes,
- start and end with a letter or digit,
- not contain two dashes in a row.

A bad slug returns `400 bad_request` with a message saying which rule failed.

Slugs are unique within your tenant, **including buckets you have deleted**. Reusing the slug of an existing or deleted bucket returns `409 conflict`, so pick a new slug instead.

## The bucket list

The **Object Storage** page lists your buckets with **Bucket**, **Usage**, **Objects**, **Quota** and **Created** columns. Use the filter box ("Filter by name or slug…") to narrow the list. Click a bucket's name, or choose **Overview** from its actions menu, to open it.

> [!NOTE]
> The **Usage** and **Objects** figures are not measured yet and currently stay at zero. See [Usage figures](/docs/storage/quotas#usage-figures).

API: `GET /api/v1/storage/buckets` (paginated) and `GET /api/v1/storage/buckets/{bucketId}`.

## Bucket tabs

A bucket's page has six tabs:

| Tab                 | What it shows                                                                                                                                                  |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Objects**         | A browser for the bucket's objects, with upload and download. See [Work with objects](#work-with-objects).                                                     |
| **Overview**        | The usage card, plus the **Canonical name**, **Slug**, **Owner**, **Label**, **Description**, **Bytes used** and **Objects used**.                             |
| **Connection**      | The **S3 endpoint** (click **Reveal endpoint**) and copyable settings for **s3cmd** and **aws CLI / boto3**. See [Using S3 clients](/docs/storage/s3-clients). |
| **Credentials**     | Access keys for this bucket. See [Access keys](/docs/storage/credentials).                                                                                     |
| **Pre-signed URLs** | A form that issues time-limited URLs. See [Pre-signed URLs](/docs/storage/presigned-urls).                                                                     |
| **Quota**           | The size and object count limits. See [Quotas](/docs/storage/quotas).                                                                                          |

## Work with objects

The **Objects** tab lists the current objects in the bucket, up to 200 at a time. Type in **Filter by prefix…** to show only keys that start with a given prefix, for example `logs/2026/`.

### Upload a file

1. Click **Upload** and pick a file.
2. A progress bar shows the upload. When it finishes you see "Uploaded _file name_."

The object key is the file's name, placed at the top of the bucket. Uploading a file with the same name as an existing object replaces it. To upload into a "folder" (a key with a prefix), or to upload many files, use an S3 client.

### Download an object

Click the download icon on the object's row. The dashboard creates a pre-signed download URL that is valid for 5 minutes and opens it in a new browser tab.

### How browser uploads and downloads work

The dashboard does not send file contents through Lahijan. It asks Lahijan for a pre-signed URL, then your browser talks to the S3 endpoint directly. For that to work:

- The S3 endpoint must be reachable from your browser.
- The bucket must allow requests from the dashboard's web address (a CORS rule). Lahijan adds this rule to every bucket automatically, using the dashboard address your operator configured, so you do not need to set it up.

If an upload fails with "Upload failed. The S3 endpoint may not be reachable from your browser.", one of these two conditions is not met. Ask your operator to check the public S3 address and the allowed dashboard origin.

### Delete an object

The dashboard has no delete button for objects. Delete them with an S3 client using a key that has the **Write** action, for example:

```sh
aws s3 rm s3://<canonical-bucket-name>/path/to/file.bin --endpoint-url https://s3.example.com
```

## Change the label or description

`PATCH /api/v1/storage/buckets/{bucketId}` with `label` and/or `description`:

```sh
curl -X PATCH https://app.example.com/api/v1/storage/buckets/$BUCKET_ID \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"label": "Nightly backups (prod)"}'
```

The slug and canonical name cannot be changed. Quotas are changed separately (see [Quotas](/docs/storage/quotas)). You need `s3.bucket.update`.

## Delete a bucket

- **Dashboard:** on the **Object Storage** page, open the bucket's actions menu and choose **Delete**.
- **API:** `DELETE /api/v1/storage/buckets/{bucketId}` returns `204`.

Deleting a bucket:

1. revokes every access key for the bucket, so they stop working immediately,
2. deletes the bucket on the S3 endpoint,
3. keeps a record of the bucket in Lahijan for the audit trail, which is why its slug cannot be reused.

> [!CAUTION]
> The dashboard deletes the bucket as soon as you click **Delete**, with no confirmation step. Download any objects you want to keep first, and do not count on getting them back afterwards.

You need `s3.bucket.delete`. The default **Tenant member** role does not have it.
