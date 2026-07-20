-- compute_backups (WS-25): tenant-scoped per-snapshot exported backups.
-- Append-only for audit (no UPDATEs to size_bytes or status beyond the
-- worker's progress); soft-deleted when the remote bytes are removed.

-- name: CreateComputeBackup :one
--: tenant-scoped
INSERT INTO compute_backups (
    tenant_id,
    snapshot_id,
    instance_id,
    target_id,
    remote_location,
    status
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetComputeBackupByID :one
--: tenant-scoped
SELECT * FROM compute_backups
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: ListComputeBackups :many
--: tenant-scoped
SELECT * FROM compute_backups
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListComputeBackupsBySnapshot :many
--: tenant-scoped
SELECT * FROM compute_backups
WHERE tenant_id = $1 AND snapshot_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListComputeBackupsByInstance :many
--: tenant-scoped; used by the instance-detail UI to show every backup
--: taken for any snapshot of the instance.
SELECT * FROM compute_backups
WHERE tenant_id = $1 AND instance_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListComputeBackupsByTarget :many
--: tenant-scoped
SELECT * FROM compute_backups
WHERE tenant_id = $1 AND target_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountComputeBackups :one
--: tenant-scoped
SELECT count(*) FROM compute_backups
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: SetComputeBackupStatus :exec
--: tenant-scoped; updates the worker's progress. The status transitions
--: are: pending -> uploading -> completed | failed. The error_message
--: column is set when status="failed".
UPDATE compute_backups
SET status = $3, error_message = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetComputeBackupResult :exec
--: tenant-scoped; records the final size + checksum + remote_location
--: after a successful upload. Only the worker calls this.
UPDATE compute_backups
SET size_bytes = $3, checksum_sha256 = $4, remote_location = $5,
    status = 'completed', error_message = '', updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeBackup :exec
--: tenant-scoped; marks the row deleted_at=now() after the worker has
--: removed the remote bytes. The row is retained for historical audit.
UPDATE compute_backups
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
