---
title: Quotas
description: Limit how much data and how many objects a bucket can hold, and understand the usage figures shown for each bucket.
---

A quota caps how big a bucket can grow. Each bucket has two independent limits: total size in bytes and number of objects. Use them to stop a runaway job from filling your storage, or to keep a shared bucket within an agreed size.

## The two limits

| Limit        | Dashboard label        | API field      | `0` means       |
| ------------ | ---------------------- | -------------- | --------------- |
| Size         | **Size quota (bytes)** | `quotaBytes`   | No size limit.  |
| Object count | **Object count quota** | `quotaObjects` | No count limit. |

New buckets have no limits unless you set them. Values are plain numbers: 10 GiB is `10737418240` bytes, 500 MiB is `524288000`.

## Set quotas when creating a bucket

In the **New bucket** dialog, fill in **Size quota (bytes)** and **Object count quota** ("0 = unlimited."). In the API, pass `quotaBytes` and `quotaObjects` to `POST /api/v1/storage/buckets` (see [Buckets](/docs/storage/buckets#create-a-bucket)).

## Change quotas

### In the dashboard

1. Open the bucket and go to the **Quota** tab.
2. Enter the new **Size quota (bytes)** and **Object count quota**.
3. Click **Update quota**. You see "Quota updated."

### With the API

`POST /api/v1/storage/buckets/{bucketId}/quota`

```sh
curl -X POST https://app.example.com/api/v1/storage/buckets/$BUCKET_ID/quota \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "X-Tenant-Id: $TENANT_ID" \
  -H "Content-Type: application/json" \
  -d '{"quotaBytes": 10737418240, "quotaObjects": 0}'
```

Both fields are required. Send `0` for a limit you want to remove. Negative values return `400 bad_request` with `quota dimensions must be non-negative`. A successful call returns `204`.

Changing quotas needs the `s3.bucket.update` permission, which the default **Tenant member**, **Tenant admin** and **Tenant owner** roles have.

## How limits are enforced

Lahijan stores the limits and passes them to the object storage backend behind the S3 endpoint, which is responsible for refusing uploads that would go over them. Lahijan itself does not inspect uploads, because they go straight to the S3 endpoint. When an upload is refused, your S3 client, SDK or the dashboard upload reports the failed request.

Things to know:

- The size limit is passed to the backend in whole mebibytes (MiB), rounded **up**. A limit of `1000` bytes behaves like a 1 MiB limit.
- Lowering a limit below what the bucket already holds does not delete anything.

> [!TIP]
> Test a new limit on a scratch bucket before relying on it for production data, so you know exactly how your client reports a refused upload.

## Usage figures

The bucket list (**Usage** and **Objects** columns), the usage card on the **Overview** tab, and `GET /api/v1/storage/buckets/{bucketId}/usage` all show how much a bucket holds:

```json
{
	"quotaBytes": 10737418240,
	"quotaObjects": 0,
	"bytesUsed": 0,
	"objectsUsed": 0
}
```

> [!NOTE]
> Lahijan does not measure bucket usage yet. `bytesUsed` and `objectsUsed` (shown as **Bytes used** and **Objects used**) currently stay at `0` no matter how much data the bucket holds, and the usage bars do not fill up. This does not change the limits you set. To see real usage, list the bucket with an S3 client, for example:
>
> ```sh
> aws s3 ls s3://<canonical-bucket-name> --recursive --summarize --endpoint-url https://s3.example.com
> ```

Quota changes are recorded in the [audit log](/docs/audit/overview).
