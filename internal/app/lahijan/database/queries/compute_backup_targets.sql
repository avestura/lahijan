-- compute_backup_targets (WS-25): tenant-scoped off-host backup destinations.
-- Every query here filters by tenant_id (set by WithTenant at the repo seam).

-- name: CreateComputeBackupTarget :one
--: tenant-scoped
INSERT INTO compute_backup_targets (
    tenant_id,
    name,
    kind,
    description,
    config_json,
    encrypted_secret_json,
    enabled
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetComputeBackupTargetByID :one
--: tenant-scoped
SELECT * FROM compute_backup_targets
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeBackupTargetByName :one
--: tenant-scoped
SELECT * FROM compute_backup_targets
WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: ListComputeBackupTargets :many
--: tenant-scoped
SELECT * FROM compute_backup_targets
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountComputeBackupTargets :one
--: tenant-scoped
SELECT count(*) FROM compute_backup_targets
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: UpdateComputeBackupTarget :exec
--: tenant-scoped; replaces the user-editable fields. The encrypted_secret_json
--: column is updated separately via SetComputeBackupTargetSecret so a config
--: edit does not require re-uploading the credentials.
UPDATE compute_backup_targets
SET name = $3, description = $4, config_json = $5, enabled = $6, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetComputeBackupTargetSecret :exec
--: tenant-scoped; replaces the AES-GCM-encrypted credentials envelope.
UPDATE compute_backup_targets
SET encrypted_secret_json = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeBackupTarget :exec
--: tenant-scoped; marks the row deleted_at=now() so historical backup
--: rows remain joinable.
UPDATE compute_backup_targets
SET deleted_at = now(), enabled = false, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
