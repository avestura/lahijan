-- compute_storage_volumes (WS-14): tenant-scoped custom storage volumes.

-- name: CreateComputeStorageVolume :one
--: tenant-scoped
INSERT INTO compute_storage_volumes (
    tenant_id, name, description, type, pool_name, config_json
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetComputeStorageVolumeByID :one
--: tenant-scoped
SELECT * FROM compute_storage_volumes
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeStorageVolumeByName :one
--: tenant-scoped; lookup by (pool, name) — the Incus composite key.
SELECT * FROM compute_storage_volumes
WHERE tenant_id = $1 AND pool_name = $2 AND name = $3 AND deleted_at IS NULL;

-- name: ListComputeStorageVolumes :many
--: tenant-scoped
SELECT * FROM compute_storage_volumes
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY name ASC
LIMIT $2 OFFSET $3;

-- name: CountComputeStorageVolumes :one
--: tenant-scoped
SELECT count(*) FROM compute_storage_volumes
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: UpdateComputeStorageVolume :exec
--: tenant-scoped
UPDATE compute_storage_volumes
SET description = $3, config_json = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeStorageVolume :exec
--: tenant-scoped
UPDATE compute_storage_volumes
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
