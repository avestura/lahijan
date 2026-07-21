# ADR-0036: S3 Lifecycle / Versioning / Object Lock design (WS-29)

- **Status:** Accepted
- **Date:** 2026-07-21
- **Deciders:** maintainer

## Context

WS-29 ("S3 Lifecycle / Versioning / Object Lock") lifts Lahijan's object
storage surface from bucket CRUD + presign (WS-13/WS-16) up to parity with
what users expect from a "real" S3:

1. **Bucket versioning** — enable / suspend; list versions; restore a
   "deleted" version (S3's delete-marker dance).
2. **Lifecycle rules** — current-version expiration, noncurrent-version
   expiration (NVP), abort-incomplete-multipart, transition to a lower
   storage tier.
3. **Object Lock** — bucket-default WORM policy (GOVERNANCE / COMPLIANCE),
   default retention period, per-object legal hold.
4. **Audit** — every privileged operation emits an audit row.
5. **WASM hooks** — `storage.object.deleted` (fires from the lifecycle
   evaluator + from the data-plane delete path) and
   `storage.lifecycle.transitioned` (fires from the lifecycle evaluator).

Per ADR-0011, the data plane stays direct from the user's S3 client to
SeaweedFS. The control plane (configure versioning, edit lifecycle rules,
set object-lock policy) goes through Lahijan. Per ADR-0027 the driver
speaks to SeaweedFS through the AWS SDK v2 S3 client + a thin Filer HTTP
wrapper; that hybrid stays.

The WS-29 doc itself notes: *"Check current SeaweedFS support for each
feature at the time work begins — some features may require a specific
version or still be in flux."* This ADR's central decision is how to
remain correct and useful across that flux.

Three sub-decisions land here.

### Sub-decision A — Source of truth + propagation

Options considered:

- **Option A1 — SeaweedFS as source of truth; Lahijan reads-through.**
  Pros: zero drift. Cons: every `GET` endpoint becomes a daemon round-
  trip; admin tooling that bypasses Lahijan leaves the user with no
  record of what changed; no place to hang tenant-scoped audit.
- **Option A2 — Postgres as source of truth; Lahijan pushes to SeaweedFS
  on every change.** Pros: matches the existing quota pattern (WS-13
  `quotas.go`); control plane stays fast; tenant-scoped audit fits. Cons:
  drift is possible if an operator hand-edits SeaweedFS. Mitigation: a
  reconcile worker (see Sub-decision C) can re-converge.
- **Option A3 — Hybrid: Postgres as source of truth for high-level
  policy, SeaweedFS as source of truth for live object state.** Same
  drawback as A1 for the policy surface.

### Sub-decision B — How lifecycle rules get enforced

SeaweedFS' native S3 lifecycle support is partial and version-dependent
(per the WS-29 doc). Options:

- **Option B1 — Trust SeaweedFS to enforce.** Pros: zero work. Cons: a
  rule configured through Lahijan silently does nothing on a build where
  SeaweedFS does not yet support it; violates the "every privileged
  action must take effect" expectation.
- **Option B2 — Run a periodic River worker that evaluates rules and
  performs expirations / transitions / aborts server-side via the S3
  SDK.** Pros: works regardless of SeaweedFS' lifecycle support; emits
  the WASM events the WS doc requires; cheap (the worker only touches
  buckets with rules). Cons: one more periodic job.
- **Option B3 — Both: push the policy via the SDK AND run the worker.**
  Pros: if SeaweedFS adds native support, the SDK path takes over; the
  worker is the safety net. Cons: double-action risk on a build where
  SeaweedFS does enforce (mitigation: the SDK delete is idempotent; an
  already-deleted object is a no-op).

### Sub-decision C — Reconcile-on-read vs. lazy reconcile

The WS-16 storage service caches `bytes_used / objects_used` and relies
on the WS-17 metering job to refresh. The same question arises for
versioning status + object-lock state.

Options considered:

- **Option C1 — Read-through on every `GET`.** Pros: always-fresh UI.
  Cons: per-`GET` daemon round-trip.
- **Option C2 — Cache + periodic reconcile worker.** Pros: matches
  WS-16/WS-17. Cons: short windows of drift after admin-side changes.

## Decision

WS-29 implements all three sub-decisions as follows.

### A — Postgres is the source of truth; push to SeaweedFS on every change

- The `storage_buckets` table gains `versioning_status`,
  `object_lock_enabled`, `object_lock_default_mode`, and
  `object_lock_default_retention_days` columns.
- A new `storage_lifecycle_rules` table holds the per-bucket rule rows.
  Normalising rules into rows (instead of a JSONB blob) lets us query,
  index, and audit a single rule change without rewriting the whole
  policy.
- Every Set operation: validate → audit pending → write Postgres →
  push to SeaweedFS via the AWS SDK v2 S3 client (`PutBucketVersioning`,
  `PutBucketLifecycleConfiguration`, `PutObjectLockConfiguration`)
  → event-bus emit → audit outcome. This is the same shape as
  `Service.SetBucketQuota` and `Service.UpdateBucket`.

### B — Push via the SDK AND run a periodic lifecycle evaluator

- The lifecycle evaluator is a River worker (`storage.lifecycle.evaluate`)
  wired as a periodic job by `program.Start`. Every tick it scans
  `storage_lifecycle_rules` for due actions and performs them via the S3
  SDK (`DeleteObject` for current-version expiration, `DeleteObject`
  with a version-id for NVP, `AbortMultipartUpload` for incomplete-
  multipart, `RestoreObject` or copy-down for transition).
- Every action emits `storage.object.deleted` or
  `storage.lifecycle.transitioned` into the WASM bus. The events have
  the same shape as a user-initiated delete so a plugin subscribed to
  `s3.object.deleted` reacts identically to lifecycle-driven and
  user-driven deletes.
- Per WS-29 "Notes": if the running SeaweedFS daemon adds native
  lifecycle support, the SDK `PutBucketLifecycleConfiguration` call
  takes effect and the worker's deletes become idempotent no-ops. No
  reconfiguration needed.

### C — Cache + periodic reconcile, same pattern as WS-16/WS-17

- Versioning status + object-lock state are written through to Postgres
  on every Set and read from Postgres on every Get. The reconcile
  worker (Sub-decision B) refreshes the Lahijan-side view of live
  object state but not the bucket-level policy (which is always
  Postgres-authoritative).

### What ships in this WS vs. follow-up

- **In scope (this WS):** bucket-level versioning, bucket-level object
  lock (default mode + default retention), bucket-level lifecycle rules
  (expiration / NVP / abort-incomplete-multipart / transition), the
  lifecycle evaluator worker, audit + events for every privileged
  action, OpenAPI surface + tests at every layer.
- **Deferred:** the dashboard UI (versioning toggle, lifecycle rule
  editor, object-lock policy panel) — the WS-29 doc's "UI" bullet is a
  one-liner; the dashboard work is its own follow-up WS because it
  needs forms, validation, and i18n in two locales. Per-object
  retention / legal-hold API endpoints are also deferred; the bucket-
  default policy is enough for compliance MVP.

## Consequences

- **Positive:** Postgres is the source of truth → tenant-scoped audit
  fits naturally; admin tooling that bypasses Lahijan leaves a
  discoverable drift, not a silent one.
- **Positive:** the lifecycle worker means features work regardless of
  SeaweedFS' maturity; as SeaweedFS catches up, the worker becomes a
  no-op safety net.
- **Positive:** the WASM event surface (`storage.object.deleted`,
  `storage.lifecycle.transitioned`) gives plugins a uniform hook for
  compliance workflows (e.g. archive-on-delete).
- **Negative:** one more periodic River worker. Mitigation: the worker
  is cheap when no rules are configured (early-exit on the rule scan).
- **Negative:** the lifecycle evaluator performs DeleteObject calls
  with the admin credentials; this is intentional (per ADR-0011 the
  control plane acts as the user) but means an audit row carries the
  system actor type rather than a user actor. Mitigation: the audit
  metadata carries `trigger=lifecycle` + the rule_id so a query can
  distinguish lifecycle-driven deletes from user-driven ones.
- **Negative:** Postgres and SeaweedFS can drift if an operator hand-
  edits the daemon. Mitigation: future WS can add a reconcile-on-tick
  that re-pushes Postgres state to SeaweedFS.

## Compliance

- `internal/app/lahijan/providers/seaweedfs/versioning.go`,
  `lifecycle.go`, `objectlock.go` add the AWS SDK v2 S3 calls. The
  existing `s3BucketAPI` interface is extended; the existing fake in
  `providers/seaweedfs/fake/server.go` is extended to satisfy the new
  methods so unit tests stay wire-format-free (per ADR-0027).
- `internal/app/lahijan/storage/versioning.go`, `lifecycle.go`,
  `objectlock.go`, `lifecycle_worker.go` add the service layer.
- Migration `0047_storage_lifecycle_versioning` adds the columns +
  the rules table; `queries/storage_lifecycle.sql` adds the sqlc
  queries; `storage_lifecycle_rules_repo.go` wraps them.
- Audit actions `s3.bucket.versioning.set`, `s3.bucket.lifecycle.set`,
  `s3.bucket.object_lock.set`, `s3.object.lifecycle_deleted` are added
  to `audit/audit.go`.
- RBAC permissions `s3.bucket.versioning`, `s3.bucket.lifecycle`,
  `s3.bucket.object_lock` are added to `rbac/permissions.go` and gated
  by the api/middleware RequirePerm seam.
- WASM event topics `S3BucketVersioningSet`, `S3BucketLifecycleSet`,
  `S3BucketObjectLockSet`, `S3ObjectDeleted`, `S3LifecycleTransitioned`
  are added to `wasm/eventbus/events.go`.
- OpenAPI schemas (`StorageVersioning`, `StorageLifecycleRule`,
  `StorageObjectLock`) and endpoints (`GET/PUT
  /api/v1/storage/buckets/{id}/versioning`, `/lifecycle`,
  `/object-lock`) are added; clients regenerated.
- Per pillar 1: the user-facing terms are "versioning", "lifecycle
  rule", "object lock". SeaweedFS is never named in the API or UI.

## References

- ADR-0011 (direct S3 for data + Lahijan for control plane)
- ADR-0002 (tenancy — every table is tenant-scoped)
- ADR-0027 (SeaweedFS client library — AWS SDK v2 + thin Filer HTTP)
- ADR-0008 (River job queue)
- ADR-0019 (append-only audit outcomes)
- WS-13 (SeaweedFS provider), WS-16 (object storage module), WS-29
  (this WS)
- [AWS S3 Object Lock overview](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)
- [SeaweedFS lifecycle redesign notes](https://github.com/seaweedfs/seaweedfs/blob/master/S3_LIFECYCLE_REDESIGN.md)
