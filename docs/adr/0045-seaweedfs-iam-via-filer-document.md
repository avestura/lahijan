# ADR-0045: SeaweedFS IAM via the filer identity document; SeaweedFS 3.99

- **Status:** Accepted
- **Date:** 2026-09-24
- **Deciders:** maintainer
- **Related:** ADR-0011 (direct S3 + Lahijan control plane), WS-13, WS-16, WS-23

## Context

ADR-0011 has Lahijan mint per-user S3 credentials that users present to
SeaweedFS directly. The WS-13 driver assumed `weed s3` watches
`/etc/seaweedfs/identities/<access_key>.json` in the filer. Bringing the
prod stack up on a real Linux host showed four problems, none visible to
the in-memory fake:

1. `weed s3` watches no such directory. It loads and hot-reloads exactly
   one document, `/etc/iam/identity.json` (`iam_pb.S3ApiConfiguration`),
   and that document **replaces** its whole identity set on reload.
   Minted credentials never went live.
2. The filer HTTP API rejects a raw-body `POST` ("request Content-Type
   isn't multipart/form-data"); raw bodies need `PUT`. Every mint 500'd.
3. The s3 watcher reads the entry's **inline** content only. With the
   filer default (`-saveToFilerLimit=0`) the document is stored in a
   volume chunk and every reload fails with `unmarshal error`.
4. With `weed s3 -config=<static file>`, the static file wins at every
   s3 start, so a restart silently dropped every minted credential.

Separately, SeaweedFS 3.61 stores the `aws-chunked` framing and checksum
trailer that current AWS SDKs send by default (boto3 >= 1.36, aws-cli
>= 2.23) inside the object: uploads from any modern S3 client were
silently corrupted.

## Decision

- **One IAM document, owned by Lahijan.** Per-credential records stay
  under `/etc/seaweedfs/identities/` as Lahijan's bookkeeping, and after
  every mint / rotate / revoke — plus once at boot — the driver rebuilds
  `/etc/iam/identity.json` from the admin identity plus every enabled
  record (`Provider.SyncIAM`). The admin identity is always included, or
  Lahijan would lock itself out of the S3 control plane. Rebuilds are
  serialised in-process (`iamMu`).
- **Filer writes use `PUT`**, and the filer runs with
  `-saveToFilerLimit=65536` so the document is stored inline.
- **No static `-config` on `weed s3`.** A one-shot `seaweed-iam-init`
  job seeds the document with the admin identity only when it does not
  exist, before s3 starts. SeaweedFS treats zero identities as "auth
  disabled", so s3 must never start without the document.
- **SeaweedFS 3.99** (latest 3.x; same CLI flags as 3.61) replaces 3.61
  in the prod stack, pinned via `SEAWEEDFS_IMAGE_TAG`. 4.x is left for a
  separate evaluation.
- **Presigned URLs are signed for the public S3 origin**
  (`providers.seaweedfs.publicEndpoint` / `LAHIJAN_S3_PUBLIC_URL`).
  SigV4 covers the Host header, so URLs signed for the internal
  `http://seaweed-s3:8333` were unusable outside the compose network.

## Consequences

- Minted credentials go live within about a second (the s3 filer
  subscription), survive s3 and Lahijan restarts, and revocation takes
  effect immediately. Verified against 3.99 with aws-cli 2.27
  (checksum trailers on): byte-exact round-trip, cross-bucket
  `AccessDenied`, revoked key `InvalidAccessKeyId`.
- Multiple Lahijan replicas (ADR-0033) can still race two concurrent
  rebuilds: the last writer may briefly miss the other's newest
  identity until the next change or restart re-syncs. A follow-up can
  move the rebuild behind a Postgres advisory lock.
- The dev and test stacks still use 3.61 + a static `s3.json`; they
  have the same corruption and restart behaviour and should follow.
