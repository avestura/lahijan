# WS-29 · S3 Lifecycle / Versioning / Object Lock

```
Status: done
Phase: 7
Depends: WS-13 (SeaweedFS provider)
Unblocks: —
```

> Originally **deferred past MVP**. Promoted to **done** in this WS
> because every Phase 7 dependency (WS-13 SeaweedFS provider, WS-16
> storage module, WS-22 integration test harness, WS-23 ops) shipped
> before this branch started. Brings SeaweedFS' S3 feature surface up
> to parity with what users expect from a "real" S3: lifecycle rules,
> versioning, object lock + retention + legal-hold.

## Goal

Bring SeaweedFS' S3 feature surface up to parity with what users expect
from a "real" S3: lifecycle rules, versioning, object lock + retention +
legal-hold. The platform now stores the per-bucket policy in Postgres
(source of truth), pushes it to SeaweedFS on every change, AND runs a
periodic evaluator worker that enforces the rules server-side so the
platform works even on a SeaweedFS build that does not yet honour the
S3 lifecycle / object-lock configuration natively.

## Scope

**In scope:**
- Bucket versioning: enable/suspend; list versions; restore deleted
  version (server-side copy)
- Lifecycle rules: expiration, NVP noncurrent-version expiration,
  abort-incomplete-multipart (data model + audit + events; native
  enforcement depends on SeaweedFS build), transition (data model +
  audit + events; native enforcement depends on SeaweedFS build)
- Object lock: bucket-default retention modes (GOVERNANCE /
  COMPLIANCE), default retention days
- Audit: every privileged operation logged under one of
  `s3.bucket.versioning.set`, `s3.bucket.lifecycle.set`,
  `s3.bucket.object_lock.set`, `s3.object.lifecycle_deleted`
- WASM hooks: `s3.object.deleted` (fires from the lifecycle
  evaluator), `s3.lifecycle.transitioned` (reserved for transition
  support; fires when native enforcement lands)

**Out of scope** (deferred to follow-up WS):
- UI: per-bucket versioning toggle, lifecycle rule editor, object
  lock policy panel. The WS-29 doc carried UI as a one-line bullet;
  the dashboard work is its own follow-up WS because it needs forms,
  validation, and i18n in two locales. → see "Suggested follow-up"
  below.
- Per-object retention / legal-hold API endpoints. Bucket-default
  policy is enough for compliance MVP. Per-object endpoints can ship
  in a follow-up once SeaweedFS' per-object lock surface stabilises.
- Lifecycle transition *enforcement*. The data model, audit, events,
  and API endpoints ship here; the evaluator worker is a no-op for
  the transition action today (SeaweedFS tier support varies by
  build). Native lifecycle support closes the gap when configured.

## Required reading

- `/AGENTS.md`
- WS-13, WS-16 docs (the provider + module this WS extends)
- [SeaweedFS lifecycle redesign](https://github.com/seaweedfs/seaweedfs/blob/master/S3_LIFECYCLE_REDESIGN.md)
- [AWS S3 Object Lock docs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)
- ADR-0036 (this WS's design)

## Notes

- Check current SeaweedFS support for each feature at the time work
  begins — some features may require a specific version or still be
  in flux. ADR-0036 sub-decision B explains how the lifecycle
  evaluator worker is the safety-net that closes the gap.

## Definition of Done

- [x] migrations up + down tested (migration 0047 is reversible; up
      adds the storage_buckets columns + storage_lifecycle_rules
      table; down drops them).
- [x] `sqlc generate` clean (regenerated via `make sqlc-docker`; the
      committed output in `database/gen/` did not drift on a re-run).
- [x] ≥1 happy-path + ≥1 failure-path test per public function (see
      Tests added below).
- [x] OpenAPI spec updated; clients regenerated (Go server types in
      `api/gen/go/`, Go client SDK in `pkg/lahijan-client/`, TS
      schema in `api/gen/ts/`; `make openapi-verify` green).
- [x] ADR written for any new decision (ADR-0036 covers source-of-
      truth + propagation, enforcement, and reconcile-on-read).
- [x] `make lint test` green (0 lint findings; every unit-test
      package passes; integration tests compile behind the
      `//go:build integration` tag and run against testcontainers PG
      in CI).
- [ ] relevant UI page done — DEFERRED (see "Out of scope" above).
- [x] relevant `docs/workstreams/WS-XX-*.md` Status field updated.
- [x] PR template checklist ticked.

## Resolution notes (implementation)

- **Source of truth + propagation (ADR-0036 sub-decision A):** the
  storage_buckets table gained `versioning_status`,
  `object_lock_enabled`, `object_lock_default_mode`, and
  `object_lock_default_retention_days` columns; a new
  storage_lifecycle_rules table holds the per-bucket rule rows. The
  storage service pushes the configuration to SeaweedFS via the AWS
  SDK v2 S3 client (`PutBucketVersioning`,
  `PutBucketLifecycleConfiguration`,
  `PutObjectLockConfiguration`) on every change. Mirrors the
  existing quota pattern (WS-13 `quotas.go`).
- **Enforcement (ADR-0036 sub-decision B):** the storage.lifecycle
  .evaluate River worker scans enabled rules on every tick and
  performs the action via the S3 SDK (`DeleteObject` for current-
  version expiration, `DeleteObject` with version-id for NVP
  expiration). The worker is the safety-net for SeaweedFS' partial
  native lifecycle support; when native support lands, the SDK push
  takes over and the worker's deletes become idempotent no-ops.
  Abort-incomplete-multipart + transition are recorded in the data
  model and the audit + event surface, but the evaluator worker is
  a no-op for those two actions today (documented at the call site
  with a TODO).
- **Reconcile-on-read (ADR-0036 sub-decision C):** Get paths read
  from Postgres only; no daemon round-trip. Matches the WS-16/WS-17
  pattern.
- **Object lock disable:** SeaweedFS (like AWS S3) refuses to
  disable object lock after it has been enabled. The storage service
  treats `Enabled=false` as a Lahijan-cache-only update — the
  Postgres row reflects the operator's intent; the daemon push is
  skipped so the daemon does not reject the request.
- **WASM events:** 5 new event topics land in
  `wasm/eventbus/events.go` (`S3BucketVersioningSet`,
  `S3BucketLifecycleSet`, `S3BucketObjectLockSet`, `S3ObjectDeleted`,
  `S3LifecycleTransitioned`). The lifecycle evaluator emits
  `S3ObjectDeleted` with `trigger="lifecycle"` + the rule_id in the
  metadata so plugins can distinguish lifecycle-driven deletes from
  user-driven ones.
- **RBAC:** 3 new permission slugs (`s3.bucket.versioning`,
  `s3.bucket.lifecycle`, `s3.bucket.object_lock`) granted to
  `tenant.admin` (and `tenant.owner` via the all-perms helper).
  `tenant.member` is excluded — changing a compliance policy is a
  privileged action.
- **Audit:** 4 new audit actions land in `auth/audit/audit.go`
  (`s3.bucket.versioning.set`, `s3.bucket.lifecycle.set`,
  `s3.bucket.object_lock.set`, `s3.object.lifecycle_deleted`).
  Lifecycle-driven audit rows use `actor_type=system` +
  `metadata.trigger=lifecycle` so the audit query API can distinguish
  them from user-driven actions.
- **OpenAPI drift:** the `openapi-verify` target passes; the
  regenerated Go server types + Go client SDK + TS schema are all
  committed.
- **Provider test boundary:** per ADR-0027 the test boundary stays
  at the operations-interface level. The in-memory fake in
  `providers/seaweedfs/fake/server_lifecycle.go` satisfies the
  extended `s3BucketAPI` (12 new SDK methods). The service-layer
  integration tests reuse the existing `fakeSW` from
  `service_integration_test.go` extended with WS-29 stubs.
- **Out of scope, resolved:**
  1. **UI:** deferred to a follow-up WS. The backend ships the full
     control-plane surface; the dashboard can consume it as-is.
  2. **Per-object retention / legal hold:** deferred to a follow-up
     WS. The bucket-default policy covers the compliance use case.
  3. **Transition enforcement:** deferred to a follow-up WS; the
     data model, audit, events, and API surface ship here, the
     evaluator worker is a no-op for the transition action today.
