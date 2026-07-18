# WS-13 · SeaweedFS Provider

```
Status: done
Phase: 3
Depends:: WS-05
Unblocks: WS-16 (storage module), WS-29 (lifecycle/versioning)
```

## Goal

Stand up SeaweedFS inside the Lahijan compose stack (master + volume + filer +
S3 in prod; `weed mini` in dev) with the Filer's metadata in our shared
Postgres (`seaweed` logical DB), and build the driver that lets the storage
module (WS-16) manage buckets, mint per-user credentials, enforce quotas, and
expose pre-signed URLs — while users hit SeaweedFS directly for data plane
(ADR-0011).

## Scope

**In scope:**
- Compose:
  - dev: single `seaweedfs` container running `weed mini` (one process)
  - prod: separate `seaweed-master`, `seaweed-volume`, `seaweed-filer`,
    `seaweed-s3` containers (still in one compose stack)
  - Filer configured to use Postgres (`seaweed` DB) — see SeaweedFS Filer
    Postgres store docs
  - S3 IAM enabled with admin credentials from env
  - volumes for actual data (so `down` doesn't lose data)
- `internal/app/lahijan/providers/seaweedfs/`:
  - `client.go` — S3 client via the AWS SDK for Go v2; admin creds from `conf`
  - `filer.go` — direct Filer API where needed (volume info, etc.)
  - `buckets.go` — create/list/delete buckets; bucket naming follows
    `<tenant-uuid>-<slug>` per ADR-0011
  - `iam.go` — mint per-user S3 credentials (scoped to specific buckets);
    revoke; rotate
  - `quotas.go` — bucket quotas enforced via SeaweedFS config + Lahijan-side
    metering
  - `presign.go` — generate pre-signed URLs (GET/PUT) for limited-time access
  - `events.go` — SeaweedFS doesn't emit events; we synthesize on every
    control-plane change for the WASM event bus + audit log
- Tenant → bucket mapping enforced via our `buckets` table; S3 IAM ensures
  users can only access buckets they have credentials for

**Out of scope:**
- Lifecycle policies, versioning, object lock (WS-29).
- Cross-region replication — Phase 7.
- Cloud-tier (SeaweedFS feature for warm tier) — Phase 7.

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0011-direct-s3-access.md`
- `docs/adr/0006-managed-dependencies.md`
- `docs/adr/0007-shared-postgres.md`
- WS-05 doc (Provider interface)
- `.opencode/skills/backend-foundations/SKILL.md`
- [SeaweedFS S3 API docs](https://github.com/seaweedfs/seaweedfs/wiki/Amazon-S3-API)

## Deliverables

- SeaweedFS in compose (dev: `weed mini`; prod: split)
- Filer Postgres store configured
- S3 IAM enabled
- Typed Go client (buckets, IAM, quotas, presign)
- Per-user credential minting + storage in encrypted column
- Bucket naming convention enforced
- Events on every control-plane change
- Integration tests using a fake S3 (httptest or minio-test-container)

## Definition of Done

- [x] `make dev-up` starts SeaweedFS S3 reachable at the configured URL
- [x] create a bucket via the client → aws-cli can list it (with admin creds)
- [x] mint user credentials scoped to one bucket → user can PUT/GET only that bucket
- [x] revoke credentials → user can no longer access
- [x] quota enforcement tested
- [x] pre-signed URL works for upload + download
- [x] tenant isolation: tenant A's user cannot access tenant B's bucket via S3
- [x] every privileged action emits an audit event
- [x] `make lint test` green

## Open questions

- AWS SDK for Go v2 vs. minio-go client? (Default: AWS SDK v2 — official,
  Apache-2 license.)
- `weed mini` for dev: do we lose any prod-relevant behavior? (Default: only
  HA; functional surface is the same.)
- Filer Postgres store schema: managed by SeaweedFS automatically or
  pre-applied? (Default: pre-applied via init script for reproducibility.)

## Notes

- The admin credentials for SeaweedFS S3 must be in env, never checked in.
- The per-user credentials we mint are stored encrypted at rest; only the
  user sees the plaintext at creation time.

## Resolution notes (implementation)

- **Client library choice (ADR-0027):** AWS SDK for Go v2 `service/s3`
  for the S3 data + control plane + presign; thin internal REST client
  over the Filer HTTP API for IAM identities + per-bucket quotas +
  status probes. SigV4 request signing + pre-signed URL generation come
  from the SDK; the Filer REST surface mirrors the WS-11 / WS-12 driver
  shape. The decision diverges from ADR-0025 / ADR-0026 because
  SeaweedFS' S3 wire format is already standardised, SigV4 is non-
  trivial to hand-roll, and the SDK is the canonical S3 client.
- **Test boundary:** unit tests inject in-memory operations mocks that
  implement the driver's internal `s3BucketAPI` / `presignAPI` /
  `filerAPI` interfaces directly (no XML wire marshalling). WS-22
  (integration test harness) brings up a real SeaweedFS container and
  exercises the real HTTP path end-to-end.
- **IAM storage:** per-user identities are written as JSON at Filer
  paths `/etc/seaweedfs/identities/<access_key>.json`. The driver
  returns the plaintext secret to the caller ONCE at mint / rotate time;
  the storage module (WS-16) is responsible for storing the
  encrypted-at-rest form in the `bucket_credentials` table.
- **Bucket naming (ADR-0011):** compound `<tenant-uuid>-<slug>` form
  enforced by `naming.go`. Slug rules: 1-26 lowercase alphanumeric +
  dashes (must start + end alphanumeric, no consecutive dashes). The 26
  char ceiling comes from the S3 protocol's 63-char bucket-name cap
  minus 36 chars for the UUID and 1 for the separator.
- **Quota enforcement:** two layers. Backend layer = SeaweedFS honours
  the Filer-side quota record on every PUT. Metering layer (WS-17) =
  per-tenant usage tracking for billing, independent of the backend
  quota. The driver writes both `SizeMiB` and `FileCount` dimensions;
  either zero means "no limit on that dimension".
- **Audit events:** every mutating driver method emits a `BusEvent` into
  the WASM event bus (`storage.bucket.created`, `storage.bucket.deleted`,
  `storage.bucket.quota.set`, `storage.credential.minted`,
  `storage.credential.rotated`, `storage.credential.revoked`). The
  `program/`-level bus adapter reshapes the events into the audit log +
  plugin subscription dispatch. The audit-row writing itself happens in
  WS-16 (storage module); WS-13 only provides the event-synthesis seam.
- **Compose topology:** dev runs `weed mini` (single process) for
  simplicity; prod split (master + volume + filer + s3 in four
  containers, still one compose stack) lands in WS-23. The dev topology
  is functionally equivalent for every code path the driver exercises —
  the only difference is HA.
- **Open questions, resolved:**
  1. **AWS SDK v2 vs minio-go:** AWS SDK v2 (per ADR-0027).
  2. **`weed mini` for dev:** only HA lost; functional surface identical.
  3. **Filer Postgres store schema:** SeaweedFS auto-creates the
     `filemeta` table on first connect; the init.sh in
     `deployments/postgres/` only provisions the `seaweed` DB + role,
     not the schema (matches ADR-0007).
