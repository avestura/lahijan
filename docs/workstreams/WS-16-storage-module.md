# WS-16 · Object Storage Module

```
Status: pending
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

- [ ] every endpoint under `/api/v1/storage/*` uses the error envelope
- [ ] every privileged action calls `RequirePerm` + emits audit
- [ ] credentials hashed at rest; plaintext shown once at creation
- [ ] credentials scoped to buckets (tenant A's user cannot access tenant B's
      bucket via S3)
- [ ] quota exceeded → PUT rejected
- [ ] pre-signed URL works for GET and PUT
- [ ] every user-facing string i18n'd; en + fa in sync
- [ ] `make lint test` green

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
