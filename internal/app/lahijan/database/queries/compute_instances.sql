-- compute_instances (WS-14): tenant-scoped records of Incus instances.
-- Every query here filters by tenant_id (set by WithTenant at the repo seam)
-- except the cross-tenant admin lookups which are explicitly marked. Soft-
-- deleted rows (deleted_at IS NOT NULL) are excluded from the unique name
-- index and from list/count, but the rows are kept for historical audit +
-- billing joins.

-- name: CreateComputeInstance :one
--: tenant-scoped
INSERT INTO compute_instances (
    tenant_id,
    project_name,
    name,
    type,
    status,
    status_code,
    image_alias,
    image_fingerprint,
    profiles,
    config_json,
    description
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetComputeInstanceByID :one
--: tenant-scoped
SELECT * FROM compute_instances
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeInstanceByName :one
--: tenant-scoped
SELECT * FROM compute_instances
WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: ListComputeInstances :many
--: tenant-scoped
SELECT * FROM compute_instances
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountComputeInstances :one
--: tenant-scoped
SELECT count(*) FROM compute_instances
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: CountComputeInstancesByStatus :one
--: tenant-scoped; used by the quota checker to count running instances.
SELECT count(*) FROM compute_instances
WHERE tenant_id = $1 AND deleted_at IS NULL AND status = $2;

-- name: ListComputeInstanceConfigsForQuota :many
--: tenant-scoped; returns the (id, config_json) pairs the quota checker
--: walks to aggregate CPU/RAM/disk usage. We do the math in Go (not SQL)
--: because limits.cpu can be "4" (int) or "4,4,4" (pinned CPUs) and
--: limits.memory / root.size accept unit suffixes (GiB, MiB, ...). A single
--: round-trip keeps this cheap.
SELECT id, config_json FROM compute_instances
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: SetComputeInstanceStatus :exec
--: tenant-scoped; caches the last-known Incus status. Called after every
--: lifecycle transition (start/stop/restart/freeze) and on read-reconcile.
UPDATE compute_instances
SET status = $3, status_code = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetComputeInstanceImageFingerprint :exec
--: tenant-scoped; records the resolved fingerprint after a successful
--: CreateInstance against Incus.
UPDATE compute_instances
SET image_fingerprint = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: UpdateComputeInstanceConfig :exec
--: tenant-scoped; replaces the cached config snapshot after a PATCH.
UPDATE compute_instances
SET config_json = $3, profiles = $4, description = $5, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeInstance :exec
--: tenant-scoped; marks the row deleted_at=now() so historical audit +
--: billing joins remain valid. The Incus instance itself is deleted via the
--: provider driver before this runs.
UPDATE compute_instances
SET deleted_at = now(), status = 'deleted', status_code = 0, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
