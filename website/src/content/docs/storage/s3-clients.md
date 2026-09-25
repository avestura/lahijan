---
title: Using S3 clients
description: Configure the AWS CLI, rclone, s3cmd and the AWS SDKs to read and write objects in a Lahijan bucket.
---

Lahijan buckets speak the S3 protocol, so most S3 tools and SDKs work with them. This page gives working settings for common clients. You need three things first:

1. The **S3 endpoint**, from the bucket's **Connection** tab (click **Reveal endpoint**). The examples use `https://s3.example.com`.
2. The bucket's **canonical name**, shown on the **Connection** and **Overview** tabs. The examples use `0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups`.
3. An **access key and secret key** for the bucket, from the **Credentials** tab (see [Access keys](/docs/storage/credentials)).

## Settings every client needs

| Setting          | Value                                                                                                                                |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Endpoint         | The S3 endpoint from the **Connection** tab.                                                                                         |
| Addressing style | **Path-style** (`https://s3.example.com/<bucket>/<key>`). Virtual-hosted style (`https://<bucket>.s3.example.com`) is not supported. |
| Region           | `us-east-1`, unless your operator tells you otherwise. The region is required by most clients but is not used to route requests.     |
| Signature        | AWS Signature Version 4 (the default in current clients).                                                                            |
| Bucket name      | The canonical name, never just the slug.                                                                                             |

Things that behave differently from a full S3 account:

- Each access key works on **one bucket only**. Commands that list all buckets (such as `aws s3 ls` with no bucket) show nothing useful or fail with access denied. Always name the bucket.
- Create and delete buckets in Lahijan, not with your S3 client.
- What a key may do depends on the actions it was minted with. A key without **Write** cannot upload; a key without **List** cannot list.

## AWS CLI

Add a profile to your AWS config files:

```ini title="~/.aws/credentials"
[lahijan]
aws_access_key_id = lahm5xq2k7w3p9r4t6yb
aws_secret_access_key = k3n5p7r9t2v4x6z8b1d3f5h7j9l2n4p6r8t1v3x5
```

```ini title="~/.aws/config"
[profile lahijan]
region = us-east-1
endpoint_url = https://s3.example.com
s3 =
    addressing_style = path
```

`endpoint_url` in the config file needs AWS CLI 2.13 or newer. With older versions, pass `--endpoint-url https://s3.example.com` on every command instead.

Try it:

```sh
export AWS_PROFILE=lahijan
BUCKET=0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups

aws s3 cp ./db.sql.gz s3://$BUCKET/nightly/db.sql.gz
aws s3 ls s3://$BUCKET/nightly/
aws s3 cp s3://$BUCKET/nightly/db.sql.gz ./restore.sql.gz
aws s3 sync ./site s3://$BUCKET/site
```

## rclone

```ini title="~/.config/rclone/rclone.conf"
[lahijan]
type = s3
provider = Other
access_key_id = lahm5xq2k7w3p9r4t6yb
secret_access_key = k3n5p7r9t2v4x6z8b1d3f5h7j9l2n4p6r8t1v3x5
endpoint = https://s3.example.com
region = us-east-1
force_path_style = true
```

Use the canonical bucket name after the remote name:

```sh
rclone ls lahijan:0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups
rclone copy ./photos lahijan:0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups/photos
```

Because the key cannot create buckets, add `--s3-no-check-bucket` if rclone tries to create the bucket and fails with access denied.

## s3cmd

```ini title="~/.s3cfg"
[default]
access_key = lahm5xq2k7w3p9r4t6yb
secret_key = k3n5p7r9t2v4x6z8b1d3f5h7j9l2n4p6r8t1v3x5
host_base = s3.example.com
host_bucket = s3.example.com
bucket_location = us-east-1
use_https = True
signature_v2 = False
```

`host_base` and `host_bucket` take the host name (and port, if any) **without** `https://`. Setting `host_bucket` to the same value as `host_base`, with no `%(bucket)s` placeholder, makes s3cmd use path-style requests. If your endpoint is plain `http://`, set `use_https = False`.

The **Connection** tab has a similar s3cmd snippet you can copy; remove the `https://` from its `host_base` and `host_bucket` lines if s3cmd rejects them.

```sh
s3cmd put ./report.pdf s3://0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups/reports/
s3cmd ls s3://0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups/reports/
```

## Python (boto3)

```python
import boto3
from botocore.config import Config

s3 = boto3.client(
    "s3",
    endpoint_url="https://s3.example.com",
    aws_access_key_id="lahm5xq2k7w3p9r4t6yb",
    aws_secret_access_key="k3n5p7r9t2v4x6z8b1d3f5h7j9l2n4p6r8t1v3x5",
    region_name="us-east-1",
    config=Config(s3={"addressing_style": "path"}),
)

bucket = "0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups"
s3.upload_file("db.sql.gz", bucket, "nightly/db.sql.gz")
for obj in s3.list_objects_v2(Bucket=bucket, Prefix="nightly/").get("Contents", []):
    print(obj["Key"], obj["Size"])
```

## Go (AWS SDK v2)

```go
package main

import (
	"context"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	client := s3.New(s3.Options{
		BaseEndpoint: aws.String("https://s3.example.com"),
		Region:       "us-east-1",
		UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(
			"lahm5xq2k7w3p9r4t6yb",
			"k3n5p7r9t2v4x6z8b1d3f5h7j9l2n4p6r8t1v3x5",
			"",
		),
	})

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("0b8f5a4e-1d2c-4e6f-9a7b-3c2d1e0f9a8b-backups"),
		Key:    aws.String("hello.txt"),
		Body:   strings.NewReader("hello from Lahijan"),
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

## Other SDKs

For other languages, look for these options in the S3 client settings:

| SDK                       | Endpoint option         | Path-style option       |
| ------------------------- | ----------------------- | ----------------------- |
| AWS SDK for JavaScript v3 | `endpoint`              | `forcePathStyle: true`  |
| AWS SDK for Java v2       | `endpointOverride(...)` | `forcePathStyle(true)`  |
| AWS SDK for .NET          | `ServiceURL`            | `ForcePathStyle = true` |

Always set the region to `us-east-1` (or the value your operator gives you).

## Troubleshooting

| Symptom                                                             | Likely cause                                                                                                                                                 |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `NoSuchBucket` or DNS lookup failures for `<bucket>.s3.example.com` | The client is using virtual-hosted style. Turn on path-style addressing.                                                                                     |
| `AccessDenied` on every request                                     | Wrong key or secret, the key was revoked, or you used the slug instead of the canonical bucket name.                                                         |
| `AccessDenied` on uploads only                                      | The key was minted without the **Write** action.                                                                                                             |
| `AccessDenied` when listing                                         | The key was minted without the **List** action.                                                                                                              |
| Connection refused or timeouts                                      | The endpoint is not reachable from where the client runs. If the **Connection** tab shows an internal address, ask your operator for the public S3 endpoint. |
| `SignatureDoesNotMatch`                                             | Clock skew on your machine, or a proxy that rewrites the request. Sync your clock and try again.                                                             |
