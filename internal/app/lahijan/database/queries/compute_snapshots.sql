-- compute_snapshots (WS-25): tenant-scoped records of Incus snapshots.
-- Every query here filters by tenant_id (set by WithTenant at the repo seam).
-- Soft-deleted rows (deleted_at IS NOT NULL) are excluded from the unique
-- name index, list/count, and the prune scan, but the rows are kept for
-- historical audit + billing joins.

-- name: CreateComputeSnapshot :one
--: tenant-scoped
INSERT INTO compute_snapshots (
    tenant_id,
    instance_id,
    name,
    stateful,
    size_bytes,
    expires_at,
    description,
    policy_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetComputeSnapshotByID :one
--: tenant-scoped
SELECT * FROM compute_snapshots
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeSnapshotByName :one
--: tenant-scoped
SELECT * FROM compute_snapshots
WHERE tenant_id = $1 AND instance_id = $2 AND name = $3 AND deleted_at IS NULL;

-- name: ListComputeSnapshots :many
--: tenant-scoped
SELECT * FROM compute_snapshots
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListComputeSnapshotsByInstance :many
--: tenant-scoped
SELECT * FROM compute_snapshots
WHERE tenant_id = $1 AND instance_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountComputeSnapshots :one
--: tenant-scoped
SELECT count(*) FROM compute_snapshots
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: CountComputeSnapshotsByInstance :one
--: tenant-scoped; used by the prune worker to enforce retain_count.
SELECT count(*) FROM compute_snapshots
WHERE tenant_id = $1 AND instance_id = $2 AND deleted_at IS NULL;

-- name: ListExpiredComputeSnapshots :many
--: tenant-scoped; returns snapshots whose expires_at has passed, ordered
--: oldest-first so the prune worker trims in creation order. The prune
--: worker caps the batch via the LIMIT it passes.
SELECT * FROM compute_snapshots
WHERE tenant_id = $1 AND deleted_at IS NULL
  AND expires_at IS NOT NULL
  AND expires_at <= $2
ORDER BY expires_at ASC
LIMIT $3;

-- name: ListOldestComputeSnapshotsForPolicy :many
--: tenant-scoped; returns the oldest N snapshots created by the given
--: policy, oldest-first. The prune worker deletes everything past the
--: retain_count by passing LIMIT = (current_count - retain_count).
SELECT * FROM compute_snapshots
WHERE tenant_id = $1 AND policy_id = $2 AND deleted_at IS NULL
ORDER BY created_at ASC
LIMIT $3;

-- name: SetComputeSnapshotSize :exec
--: tenant-scoped; records the daemon-reported size after a successful
--: CreateSnapshot call.
UPDATE compute_snapshots
SET size_bytes = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetComputeSnapshotExpiry :exec
--: tenant-scoped; sets/clears the expires_at column. The take worker
--: stamps it from the policy's cadence + retain window when the snapshot
--: is created by a schedule.
UPDATE compute_snapshots
SET expires_at = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeSnapshot :exec
--: tenant-scoped; marks the row deleted_at=now() so historical audit +
--: billing joins remain valid. The Incus snapshot itself is deleted via
--: the provider driver before this runs.
UPDATE compute_snapshots
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
