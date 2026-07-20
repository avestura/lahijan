# ADR-0032: Scheduled snapshots + off-host backup design

- **Status:** Accepted
- **Date:** 2026-07-21
- **Deciders:** maintainer

## Context

WS-25 ("Scheduled Snapshots & Backups") adds three new long-lived concerns
on top of WS-14's compute module:

1. **Snapshot orchestration.** Incus has native snapshot + backup primitives;
   Lahijan must wrap them with per-tenant + per-instance schedules plus
   retention policies without leaking the word "Incus" to end users
   (pillar 1, "transparent infrastructure").
2. **Off-host backup.** Snapshots live on the compute node's storage pool;
   a node failure takes them down with it. WS-25 needs an off-host target
   abstraction (S3-compatible, NFS-mount, ssh+rsync) the operator configures
   per tenant.
3. **Schedule execution.** A durable scheduler (River) must scan for due
   policies every minute, fire a take + a prune, and queue a backup export
   when the policy has a target. The scheduling work must survive a
   process restart (River's durability guarantee) and remain idempotent
   on retry (duplicate takes must not double-charge).

Options considered:

- **Option A — Incus-side schedules only.** Incus supports
  `snapshots.schedule` per-instance config + a built-in
  `snapshots.expiry` retention. **Pros:** minimal Lahijan code; the
  daemon owns the schedule. **Cons:** no off-host backup story; no
  per-policy retention accounting; the schedule + retention live in
  instance config that the user can accidentally overwrite via PATCH;
  no audit trail at the policy level.
- **Option B — External cron driving Incus REST.** A crontab on the
  Lahijan host POSTs to Incus per schedule. **Pros:** simple. **Cons:**
  no durability; no per-tenant isolation; no off-host backup story.
- **Option C — River-driven schedules with Lahijan-owned rows.** The
  canonical rows live in Lahijan's Postgres (`compute_snapshots`,
  `compute_snapshot_policies`, `compute_backup_targets`,
  `compute_backups`); River scans + fires the right Incus API call.
  **Pros:** leverages existing durability + audit + tenancy; one
  scheduling mechanism for snapshots + backups + every future periodic
  job; the Incus side stays stateless from Lahijan's perspective.
  **Cons:** more Lahijan code (orchestration, retention, target
  drivers).

## Decision

Adopt **Option C**. Concretely:

- **Schedules live in Postgres.** `compute_snapshot_policies` carries
  cadence + retain_count + optional target_id. River's periodic
  scheduler inserts one `compute.snapshot.take` tick per minute; the
  worker scans `ListDue` + fires `Service.TakeSnapshot` per policy.
  Retention is enforced by the same worker (after a successful take,
  prune the oldest excess for that policy) plus a separate
  `compute.snapshot.prune` worker that scans `expires_at`.
- **Backup targets are an interface.** `BackupTarget` ships three
  drivers in-tree (S3-compatible, locally-mounted directory exposed as
  "nfs", ssh+rsync) and is open to extension. The factory decrypts the
  per-target credential blob via the process-wide AES-GCM envelope
  (reused from `auth/secrets`) so DB-only exfiltration cannot recover
  the credentials.
- **Off-host backup is a separate worker.** `compute.backup.create` is
  queued by the take worker when the policy has a `target_id`. The
  worker pulls the snapshot tarball from Incus + streams it to the
  driver; the per-(snapshot, target) row idempotently absorbs retries.
- **WASM event hooks.** Every successful take emits
  `compute.snapshot.taken`; every successful backup emits
  `compute.backup.created` (per the WS-25 scope item). Plugins
  subscribe via the existing WS-10b bus.
- **RBAC.** Eight new permission slugs cover the surface
  (`compute.snapshot.{create,read,delete}`, four
  `compute.backup.{target.*,...}`, and four `compute.snapshot_policy.*`).
  The audit gate maps every new path to the right slug; the restore
  path reuses `compute.instance.update` because restore replaces the
  instance state wholesale.

## Consequences

- **Positive:** the snapshot + backup surface inherits ADR-0002's
  row-level tenancy, ADR-0008's River durability, ADR-0019's audit
  pattern, and ADR-0017's event-bus seam. No new infrastructure
  primitives were introduced.
- **Negative:** more Lahijan code than Option A. The Incus-side
  `snapshots.schedule` feature is unused; a future operator who wants
  daemon-side schedules must re-implement the round-trip (acceptable
  today; revisit if WS-26 multi-node lands and the daemon's schedule
  becomes attractive again).
- **Negative:** the S3 driver streams snapshot tarballs through the
  Lahijan process (not directly Incus -> S3). For very large instances
  this is a memory + bandwidth cost; a future optimisation is to
  pre-sign S3 URLs + hand them to Incus' `POST
  /instances/<id>/backups` API (the upstream surface already supports
  it).
- **Neutral:** the SSH driver's host-key validation defaults to
  `InsecureIgnoreHostKey` when no fingerprint is configured. The
  operator can pin the fingerprint via `host_key_fingerprint`; a future
  WS will wire a known_hosts file.

## Compliance

- `internal/app/lahijan/compute/backups.go` declares the
  `BackupTarget` interface + the factory.
- `internal/app/lahijan/compute/jobs.go` declares the three River
  workers + `RegisterJobs`.
- `internal/app/lahijan/database/migrations/003{7,8,9}_compute_*`
  create the four tenant-scoped tables.
- `internal/app/lahijan/api/router.go` `AuditGate` dispatches every
  new path under `/api/v1/compute/{snapshots,backups,backup-targets,
  snapshot-policies}/*` to the right `rbac.Perm*` slug.
- `internal/app/lahijan/program/program.go`'s
  `registerComputeSnapshotWorkers` wires the three workers on the
  shared registry.

## References

- WS-25 doc: `docs/workstreams/WS-25-scheduled-snapshots-backups.md`
- WS-14 (Compute Module): `docs/workstreams/WS-14-compute-module.md`
- ADR-0008 (River job queue)
- ADR-0010 (full Incus surface)
- ADR-0013 (ledger billing) — snapshots + backups meter at storage rates
- ADR-0019 (append-only audit outcomes) — every privileged action here
  follows the pending -> success | failure pattern
- Incus backups howto: https://linuxcontainers.org/incus/docs/main/howto/instances_backup/
