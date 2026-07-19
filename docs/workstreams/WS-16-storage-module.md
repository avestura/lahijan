# WS-16 · Object Storage Module

```
Status: done
Phase: 4
Depends on: WS-13, WS-08
Unblocks: WS-21 (S3 UI)
```

## Goal

Expose S3 (SeaweedFS) through Lahijan's user-facing API. Users create buckets,
mint per-app credentials, generate pre-signed URLs, and Lahijan enforces
quotas — while data flows directly to SeaweedFS using those credentials.

## Scope

**In scope:**
- `internal/app/lahijan/storage/`:
  - `service.go` — orchestrates SeaweedFS provider + audit + plugin hooks
  - `buckets.go` — bucket CRUD per tenant; bucket name validation (DNS-safe,
    globally unique in SeaweedFS)
  - `credentials.go` — mint per-user credentials scoped to specific buckets;
    revoke; rotate; show once at creation
  - `presign.go` — generate pre-signed GET/PUT URLs (time-limited)
  - `quotas.go` — per-bucket quotas (size + object count); enforcement +
    metering
- API endpoints under `/api/v1/storage/*`:
  - `GET/POST /buckets`, `GET/PATCH/DELETE /buckets/{id}`
  - `POST /buckets/{id}/credentials`, `GET /buckets/{id}/credentials`,
    `DELETE /buckets/{id}/credentials/{id}`
  - `POST /buckets/{id}/presign` (body: method, key, expires_in)
  - `POST /buckets/{id}/quota`, `GET /buckets/{id}/usage`
- DB tables (tenant-scoped):
  - `storage_buckets` (id, tenant_id, name, owner_user_id, quota_bytes,
    quota_objects, created_at, updated_at, deleted_at)
  - `storage_credentials` (id, bucket_id, tenant_id, user_id, access_key_id,
    secret_hash, label, last_used_at, expires_at, created_at, revoked_at)
- WASM plugin hooks: every bucket / credential change emits events
- Audit: every privileged action emits audit pre + post
- Billing: storage usage metered via WS-17's metering job (reads bucket sizes
  periodically)

**Out of scope:**
- S3 lifecycle policies, versioning, object lock (WS-29).
- Direct object browsing/uploading through Lahijan API — users use any S3
  client directly per ADR-0011.
- Cross-region replication (Phase 7).

## Required reading for the AI session

- `/AGENTS.md`
- `docs/adr/0011-direct-s3-access.md`
- `docs/adr/0013-ledger-billing.md`
- WS-08 doc (RBAC + audit)
- WS-13 doc (SeaweedFS provider)
- `.opencode/skills/backend-foundations/SKILL.md`
- `.opencode/skills/database-conventions/SKILL.md`

## Deliverables

- Full storage API surface
- Tenant-scoped storage tables
- Credential minting (shown once, hashed at rest)
- Pre-signed URL generation
- Quota enforcement
- WASM event emission on every change
- Integration tests: bucket CRUD, credential mint + use + revoke, presign,
  quota exceeded

## Definition of Done

- [x] every endpoint under `/api/v1/storage/*` uses the error envelope
- [x] every privileged action calls `RequirePerm` + emits audit
- [x] credentials hashed at rest; plaintext shown once at creation
- [x] credentials scoped to buckets (tenant A's user cannot access tenant B's
      bucket via S3)
- [x] quota exceeded → PUT rejected
- [x] pre-signed URL works for GET and PUT
- [x] every user-facing string i18n'd; en + fa in sync
- [x] `make lint test` green

## Open questions

- Bucket naming: `<tenant-uuid>-<slug>` per ADR-0011, or user picks any DNS-safe
  name (with a uniqueness check)? (Default: user picks; uniqueness enforced
  globally in SeaweedFS.)
- Credential expiry: required? Default lifetime? (Default: optional; default
  lifetime 90 days if set.)
- Object-level audit (who GET/PUT which object)? (Default: not in MVP; relies
  on SeaweedFS access logs which are coarse.)

## Notes

- This WS is the place to lock down the "credentials shown once" UX pattern,
  which we reuse for PATs (WS-06) and plugin API keys.
- Billing for storage is event-of-the-billing-cycle (size * time); metered via
  WS-17.

## Resolution notes (implementation)

- **Bucket naming:** stuck with ADR-0011's compound `<tenant-uuid>-<slug>`
  form. The WS doc's "Open questions" item 1 floated "user picks any
  DNS-safe name" as the default, but ADR-0011 (Accepted) already
  locked the compound form and the WS-13 driver (done) enforces it via
  `providers/seaweedfs.BucketName`. The user-facing API therefore
  takes a `slug` (the user-picked portion); the service composes the
  canonical name. This satisfies both: "user picks a slug" at the API
  surface + ADR-0011's compound form on the wire.
- **Credential expiry:** optional with no default lifetime today. The
  wire field `expiresInSeconds` controls it; when absent, the
  credential never expires. The 90-day default floated in the WS doc
  is deferred to a follow-up (it needs a config knob + a metering-job
  janitor, both of which depend on WS-17). The schema's `expires_at`
  column is nullable so the janitor path lands cleanly.
- **Object-level audit:** confirmed deferred. Per-credential audit
  (mint + revoke) is the audit surface; object-level access audit
  relies on SeaweedFS access logs (ADR-0011 negative consequence #1).
- **Secret at rest:** sha256 fingerprint (`secret_hash` column) chosen
  over AES-GCM because the Lahijan side NEVER needs to recover the
  plaintext after mint time. sha256 is sufficient because the secret
  is a 40-char random base32 string (no rainbow table risk); argon2id
  is reserved for user-chosen passwords.
- **Quota enforcement:** two layers, exactly per WS-13. (1) Backend —
  SeaweedFS honours the Filer-side quota record on every PUT. The
  storage service pushes the quota via `provider.SetBucketQuota` on
  every SetBucketQuota API call AND at bucket-create time when the
  caller passes non-zero dimensions. (2) Metering — WS-17 will
  refresh the cached `bytes_used` / `objects_used` columns; the
  `GET /buckets/{id}/usage` endpoint reads the cache.
- **Soft-delete:** buckets soft-delete (`deleted_at`); credentials
  soft-revoke (`revoked_at`). Both keep the audit trail alive. A
  future WS will add a hard-purge admin endpoint that drops rows past
  a retention window.
- **Audit actions:** 7 new audit actions land in `auth/audit/audit.go`
  (`s3.bucket.create`, `s3.bucket.update`, `s3.bucket.delete`,
  `s3.bucket.quota.set`, `s3.credentials.create`,
  `s3.credentials.revoke`, `s3.presign`). Each carries the bucket_id
  (or credential_id) + the actor + the slug in metadata. The audit
  gate's path-aware `RequirePerm` maps every `/api/v1/storage/*`
  route to one of the existing `rbac.PermS3*` slugs.
- **Event bus:** 4 new topics land in `wasm/eventbus/events.go`
  (`s3.bucket.quota.set`, `s3.credential.minted`,
  `s3.credential.revoked`, `s3.presign.issued`) alongside the 3
  WS-13-declared topics (`s3.bucket.created/updated/deleted`). The
  driver emits the bucket-lifecycle topics; the storage service
  emits the credential + presign topics so plugins can react to
  credential rotations + URL issuances.
- **Cross-tenant isolation:** enforced at the repository seam. The
  `storage_buckets` + `storage_credentials` queries all carry
  `tenant_id = $1` (sourced from `database.TenantFromContext`); a
  cross-tenant bucket id surfaces as `ErrBucketNotFound`, never as
  the row itself. The `TestTenantIsolation_*` tests at both the
  service layer and the HTTP boundary pin this.
- **OpenAPI drift:** the `openapi-verify` target passes; the
  regenerated Go server types + Go client SDK + TS schema are all
  committed.
- **Open questions, resolved:**
  1. **Bucket naming:** compound form per ADR-0011 (see above).
  2. **Credential expiry:** optional, no default lifetime today (see above).
  3. **Object-level audit:** deferred per ADR-0011 (see above).
