-- compute_profiles (WS-14): tenant-scoped Incus profile catalog.

-- name: CreateComputeProfile :one
--: tenant-scoped
INSERT INTO compute_profiles (tenant_id, name, description, config_json)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetComputeProfileByID :one
--: tenant-scoped
SELECT * FROM compute_profiles
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeProfileByName :one
--: tenant-scoped
SELECT * FROM compute_profiles
WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: ListComputeProfiles :many
--: tenant-scoped
SELECT * FROM compute_profiles
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY name ASC
LIMIT $2 OFFSET $3;

-- name: CountComputeProfiles :one
--: tenant-scoped
SELECT count(*) FROM compute_profiles
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: UpdateComputeProfile :exec
--: tenant-scoped
UPDATE compute_profiles
SET description = $3, config_json = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeProfile :exec
--: tenant-scoped
UPDATE compute_profiles
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
