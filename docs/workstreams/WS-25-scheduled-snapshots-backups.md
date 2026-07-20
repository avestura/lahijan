# WS-25 · Scheduled Snapshots & Backups

```
Status: done
Phase: 7
Depends on: WS-11 (Incus provider), WS-09 (River)
Unblocks: —
```

> Originally **deferred past MVP**. Implemented as part of the Phase 7
> follow-on wave (after WS-24) — the doc is brief by design; this update
> records the resolution notes + the derived Definition of Done.

## Goal

Let users define snapshot schedules for instances (e.g. "every hour, keep
last 24; every day, keep last 7; every week, keep last 4") and let admins
configure off-host backup targets (S3-compatible, NFS, ssh+rsync).

## Scope (as implemented)

- `compute_snapshots` table (per-instance) — the canonical Lahijan-side
  record. Mirrors Incus state; soft-deleted on snapshot delete.
- `compute_snapshot_policies` table (per-instance + per-tenant defaults):
  ISO 8601 cadence, retain count, optional target.
- `compute_backup_targets` table (per-tenant, AES-GCM-encrypted secrets):
  S3-compatible, NFS (locally-mounted directory), ssh+rsync.
- `compute_backups` table (per-snapshot exported backups, append-only).
- River jobs:
  - `compute.snapshot.take` (scans due policies; one per minute via the
    periodic scheduler).
  - `compute.snapshot.prune` (enforces `expires_at`; per-policy retention
    is enforced inline by the take worker).
  - `compute.backup.create` (oneshot per (snapshot, target); queued by
    the take worker).
- Backup target abstraction: `BackupTarget` interface with three drivers
  (S3 / NFS-local / SSH) — open to extension without a schema change.
- Backup restore API: snapshot-based restore (`POST
  /instances/{id}/snapshots/{snap}/restore`); the off-host backup
  restore path is wired at the service layer (`Service.DownloadBackupReader`)
  and surfaced via the API surface as `compute.backup.restore` perm.
- Billing: snapshots + backups metered at storage rates. The metering
  pipeline (WS-17) reads `compute_snapshots.size_bytes` +
  `compute_backups.size_bytes` directly; no new catalog rows are
  introduced by this WS.
- WASM plugin hooks: `compute.snapshot.taken`, `compute.snapshot.pruned`,
  `compute.backup.created`, `compute.backup.deleted` are registered
  canonical event-bus topics.

## Required reading (when work begins)

- `/AGENTS.md`
- WS-09, WS-11, WS-14, WS-17 docs
- [Incus backups](https://linuxcontainers.org/incus/docs/main/howto/instances_backup/)

## Definition of Done

- [x] `compute_snapshots_policies` table (per-instance + per-tenant
      defaults) — shipped as `compute_snapshots` +
      `compute_snapshot_policies` + `compute_backup_targets` +
      `compute_backups` (migrations 0037 + 0038 + 0039).
- [x] River job: `compute.snapshot.take` (runs on schedule)
      — `internal/app/lahijan/compute/jobs.go:SnapshotTakeWorker`.
- [x] River job: `compute.snapshot.prune` (enforces retention)
      — `internal/app/lahijan/compute/jobs.go:SnapshotPruneWorker`.
- [x] Backup target abstraction: `BackupTarget` interface with
      implementations for S3-compatible, local NFS, ssh+rsync
      — `internal/app/lahijan/compute/backups{,_s3,_local,_ssh}.go`.
- [x] Backup restore API: pick a snapshot, restore to the existing
      instance. The scope said "to a new or existing instance"; the
      Incus restore primitive (`POST /instances/<id>/snapshots/<snap>/
      restore`) restores in-place. The off-host backup restore path
      (download from the target + create-instance-from-backup) is
      implemented at the service layer (`Service.DownloadBackupReader`)
      and exposed via `compute.backup.restore` perm; a follow-up WS
      will wire the full "create new instance from backup" UI once the
      WS-14 `CreateInstance(imageSource=fingerprint)` path is extended
      to accept a backup tarball as the source.
- [x] Billing: snapshots + backups metered at storage rates — the
      `compute_snapshots.size_bytes` + `compute_backups.size_bytes`
      columns are the metering source. WS-17's catalog gains two rows
      (`compute.snapshot.gb_month`, `compute.backup.gb_month`) at
      operator config time; this WS ships the data those rows sum.
- [x] WASM plugin hook: `compute.snapshot.taken`, `compute.backup.completed`
      — registered as canonical event-bus topics
      (`internal/app/lahijan/wasm/eventbus/events.go`).
- [x] every endpoint under the new paths uses the error envelope
      — `internal/app/lahijan/api/compute_snapshots_handlers.go` +
      `mapComputeSnapshotError`.
- [x] every privileged action calls `RequirePerm`
      — `AuditGate` in `router.go` dispatches every new path.
- [x] every state-changing privileged action emits audit pre + post
      — `compute/snapshots.go` + `compute/snapshot_policies.go` +
      `compute/backup_targets.go` follow the pending -> success | failure
      pattern.
- [x] multi-tenant isolation: tenant A cannot see/manage tenant B's
      snapshots — enforced at the repository layer (`compute_snapshots*
      _repo.go` reads tenant_id from ctx).
- [x] every user-facing string i18n'd; en + fa in sync
      — `internal/app/lahijan/i18n/locales/{en,fa}.json` updated; the
      i18n sync test passes.
- [x] frontend panel: the WS-20 placeholder is replaced by a real
      Snapshots tab (create + list + restore + delete + i18n).
- [x] `make lint test` green.

## Open questions

All resolved by this implementation; defaults adopted as proposed:

- **Schedule execution: Incus-side `snapshots.schedule` vs River-driven?**
  River-driven. See ADR-0032 for the rationale; the short version is
  Lahijan's rows own the policy + retention + audit + tenancy story and
  River's durability matches Incus' schedule feature for free.
- **Backup credential storage: encrypted column vs external Vault?**
  AES-GCM-encrypted column (BYTEA) via the existing `auth/secrets`
  envelope. Same approach the auth subsystem uses for IdP tokens; a
  future WS will move to an external Vault when the operator surface
  needs it (ADR-0032 records the trade-off).
- **Restore target: existing instance only, or also create-new?**
  Existing-only for this WS. The Incus restore primitive is in-place;
  the off-host backup restore (`Service.DownloadBackupReader`) is wired
  at the service layer; a follow-up WS will add the
  create-new-instance-from-backup flow once WS-14's image-source path
  accepts a backup tarball.
- **SSH host-key validation strategy?** Default to
  `InsecureIgnoreHostKey` when no fingerprint is configured; the
  operator can pin via `host_key_fingerprint`. A future WS will wire a
  known_hosts file once the operator surface needs it.

## Resolution notes (implementation)

- **Phase 7 reference doc:** the WS-25 doc was originally `Status:
  deferred`. This implementation lands it as `Status: done`. The WS
  has no upstream blockers (WS-11 + WS-09 are done), and the
  follow-on Phase 7 wave (after WS-24) is the right time to ship it.
- **`sqlc v1.27.0 on Windows`** still hits the wasilibs/go-pgquery
  panic from the WS-14 notes; local devs use `make sqlc-docker` to
  regenerate. The committed output is identical to the Linux run.
- **WS-09 placeholder removal:** the WS-09 doc shipped a placeholder
  `compute.instance.snapshot` worker; this WS removes it (the WS-09
  doc explicitly says "WS-25 ships the real implementation") and adds
  three real workers (`compute.snapshot.take`, `.prune`,
  `compute.backup.create`) in the compute package. The
  `examples_test.go` count was updated from 4 to 3 accordingly.
- **Periodic scheduling wiring:** the take + prune workers are
  registered on the River registry in `program.Start` via
  `registerComputeSnapshotWorkers`. The periodic schedules themselves
  (every 1 minute for take, every 5 minutes for prune) are conservative
  defaults; a future WS will make them configurable via
  `conf.compute.snapshot.schedule.*`.
- **Backup target "nfs" is locally-mounted:** the operator pre-mounts
  the NFS share and points the target at the mount point. The driver
  is plain filesystem IO; true NFS mounting is operator infra, not
  application code. The "kind" is `nfs` so the UI shows the right
  config shape.
- **S3 driver streams through Lahijan:** the snapshot tarball is
  downloaded from Incus into Lahijan memory + re-uploaded to the S3
  endpoint. For very large instances this is a memory + bandwidth
  cost; a future optimisation is to pre-sign S3 URLs + hand them to
  Incus' native `POST /instances/<id>/backups` API (which the upstream
  surface already supports).
- **Restore from off-host backup:** the service-layer
  `DownloadBackupReader` opens the remote backup for streaming. The
  HTTP-level "create new instance from backup" endpoint is left to a
  follow-up WS (the WS-14 `CreateInstance` path needs an
  `imageSource=backup_tarball` extension).

## Notes

- Incus has native backup + snapshot primitives; this WS orchestrates them
  via schedules.
- Backup target abstraction leaves room for cross-region replication later.
- ADR-0032 records the architectural decision in detail.
