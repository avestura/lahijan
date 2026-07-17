# ADR-0011: Direct S3 for data + Lahijan for control plane

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** maintainer

## Context

SeaweedFS exposes an S3-compatible API. End users need to read and write
objects in buckets. We must decide how data flows.

Options considered:

- **Proxy all S3 traffic through Lahijan** — full audit, full control; every
  byte goes through our process; severe performance penalty for large objects.
- **Direct S3 — users get their own credentials and hit SeaweedFS directly** —
  best performance; Lahijan only manages bucket lifecycle and credentials;
  audit is via SeaweedFS access logs.
- **Both modes** — direct for data plane, Lahijan for control plane.

## Decision

Lahijan uses the **hybrid** model:

- **Control plane** (create/delete buckets, set policies, mint/revoke
  per-user credentials, manage quotas, configure lifecycle) goes through the
  Lahijan API. Every action requires `RequirePerm` and emits an audit event.
- **Data plane** (GET/PUT/DELETE objects, list bucket, multipart uploads,
  pre-signed URLs) goes **directly** from the user's S3 client to SeaweedFS,
  using credentials minted by Lahijan.

SeaweedFS' S3 IAM subsystem stores the per-user credentials and enforces
per-bucket ACLs.

## Consequences

- **Positive:** no proxy bottleneck; large object uploads don't touch Lahijan.
- **Positive:** users can use any S3-compatible tool (aws-cli, rclone, s3cmd,
  SDKs) against Lahijan-issued credentials.
- **Positive:** Lahijan stays small and stateless for data plane.
- **Negative:** per-request audit of object reads/writes relies on SeaweedFS
  access logs, not Lahijan. Mitigated by: billing metering via SeaweedFS
  metrics, and access log shipping into the audit table.
- **Negative:** credential rotation must propagate to SeaweedFS IAM reliably.

## Compliance

- `internal/app/lahijan/providers/seaweedfs/` ships the S3 client + IAM
  integration.
- `internal/app/lahijan/storage/` exposes bucket CRUD, credential minting,
  quota enforcement via the Lahijan API.
- SeaweedFS S3 IAM is configured to refuse requests that don't carry a
  Lahijan-minted credential.
- Bucket ARNs follow the pattern `arn:aws:s3:::<tenant-uuid>-<bucket-slug>`.

## References

- [SeaweedFS S3 API](https://github.com/seaweedfs/seaweedfs/wiki/Amazon-S3-API)
- ADR-0006 (managed deps — we configure SeaweedFS for the user)
- ADR-0007 (shared Postgres for Filer)
- WS-13 (SeaweedFS provider), WS-16 (object storage module)
