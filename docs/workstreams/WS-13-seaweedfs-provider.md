# WS-13 · SeaweedFS Provider

```
Status: in-progress
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

- [ ] `make dev-up` starts SeaweedFS S3 reachable at the configured URL
- [ ] create a bucket via the client → aws-cli can list it (with admin creds)
- [ ] mint user credentials scoped to one bucket → user can PUT/GET only that bucket
- [ ] revoke credentials → user can no longer access
- [ ] quota enforcement tested
- [ ] pre-signed URL works for upload + download
- [ ] tenant isolation: tenant A's user cannot access tenant B's bucket via S3
- [ ] every privileged action emits an audit event
- [ ] `make lint test` green

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
