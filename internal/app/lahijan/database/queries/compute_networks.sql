-- compute_networks (WS-14): tenant-scoped Incus network catalog.

-- name: CreateComputeNetwork :one
--: tenant-scoped
INSERT INTO compute_networks (
    tenant_id, name, description, type, config_json, acl_names, forward_names
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetComputeNetworkByID :one
--: tenant-scoped
SELECT * FROM compute_networks
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeNetworkByName :one
--: tenant-scoped
SELECT * FROM compute_networks
WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: ListComputeNetworks :many
--: tenant-scoped
SELECT * FROM compute_networks
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY name ASC
LIMIT $2 OFFSET $3;

-- name: CountComputeNetworks :one
--: tenant-scoped
SELECT count(*) FROM compute_networks
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: UpdateComputeNetwork :exec
--: tenant-scoped
UPDATE compute_networks
SET description = $3, config_json = $4, acl_names = $5, forward_names = $6,
    updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeNetwork :exec
--: tenant-scoped
UPDATE compute_networks
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
