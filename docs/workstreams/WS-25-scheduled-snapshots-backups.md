# WS-25 · Scheduled Snapshots & Backups (DEFERRED)

```
Status: deferred
Phase: 7
Depends on: WS-11 (Incus provider), WS-09 (River)
Unblocks: —
```

> **Deferred past MVP.** WS-14 ships manual snapshots; this WS adds schedules
> and automated off-host backup.

## Goal

Let users define snapshot schedules for instances (e.g. "every hour, keep
last 24; every day, keep last 7; every week, keep last 4") and let admins
configure off-host backup targets (S3-compatible, NFS, etc.).

## Scope (when work begins)

- `compute_snapshots_policies` table (per-instance + per-tenant defaults):
  cadence, retention, target
- River job: `compute.snapshot.take` (runs on schedule)
- River job: `compute.snapshot.prune` (enforces retention)
- Backup target abstraction: `BackupTarget` interface with implementations
  for S3-compatible, local NFS, ssh+rsync
- Backup restore UI: pick a snapshot, restore to a new or existing instance
- Billing: snapshots + backups metered at storage rates
- WASM plugin hook: `compute.snapshot.taken`, `compute.backup.completed`

## Required reading (when work begins)

- `/AGENTS.md`
- WS-09, WS-11, WS-14, WS-17 docs
- [Incus backups](https://linuxcontainers.org/incus/docs/main/howto/instances_backup/)

## Notes

- Incus has native backup + snapshot primitives; this WS orchestrates them
  via schedules.
- Backup target abstraction leaves room for cross-region replication later.
