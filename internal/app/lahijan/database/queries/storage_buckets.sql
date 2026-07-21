-- Storage buckets: tenant-scoped mapping (WS-16). The SeaweedFS driver
-- operates on the canonical bucket name (output of providers/seaweedfs.BucketName);
-- the storage service consults this table to translate a tenant context into
-- the canonical name before calling the SeaweedFS driver. Every query is
-- tenant-scoped via WithTenant (database/tenant.go) EXCEPT the admin-only
-- "canonical name -> row" lookup which is global.

-- name: CreateStorageBucket :one
--: tenant-scoped
INSERT INTO storage_buckets (
    tenant_id, name, slug, owner_user_id, label, description,
    quota_bytes, quota_objects, bytes_used, objects_used
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, 0)
RETURNING *;

-- name: GetStorageBucketByID :one
--: tenant-scoped
SELECT * FROM storage_buckets
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetStorageBucketBySlug :one
--: tenant-scoped
SELECT * FROM storage_buckets
WHERE tenant_id = $1 AND slug = $2 AND deleted_at IS NULL;

-- name: GetStorageBucketByCanonicalName :one
--: tenant-scoped
SELECT * FROM storage_buckets
WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: GetStorageBucketByCanonicalNameGlobal :one
-- Admin-only path: no tenant scoping. Used by the storage service's
-- cross-tenant "is this canonical name owned by anyone?" check at create
-- time. Includes soft-deleted rows so a re-create after delete is rejected
-- with a clear "name claimed" error rather than colliding silently.
SELECT * FROM storage_buckets WHERE name = $1;

-- name: ListStorageBuckets :many
--: tenant-scoped
SELECT * FROM storage_buckets
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountStorageBuckets :one
--: tenant-scoped
SELECT count(*) FROM storage_buckets
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: UpdateStorageBucketLabel :exec
--: tenant-scoped
UPDATE storage_buckets
SET label = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: UpdateStorageBucketDescription :exec
--: tenant-scoped
UPDATE storage_buckets
SET description = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetStorageBucketQuota :exec
--: tenant-scoped
-- Pushes the new quota dimensions to the row. The SeaweedFS daemon is
-- updated separately by the storage service via the provider's
-- SetBucketQuota so the daemon enforces the ceiling server-side.
UPDATE storage_buckets
SET quota_bytes = $3, quota_objects = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetStorageBucketUsage :exec
--: tenant-scoped
-- Updates the cached bytes_used / objects_used columns. Called by the
-- WS-17 metering job after it polls SeaweedFS for the live bucket size.
UPDATE storage_buckets
SET bytes_used = $3, objects_used = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteStorageBucket :exec
--: tenant-scoped
-- Marks the row as deleted. The SeaweedFS bucket is removed separately by
-- the storage service via the provider's DeleteBucket; the row stays so
-- the audit trail can reference it.
UPDATE storage_buckets
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetStorageBucketVersioning :exec
--: tenant-scoped
-- Flips the cached versioning_status column (WS-29). The SeaweedFS
-- daemon is updated separately by the storage service via the provider's
-- SetBucketVersioning so the daemon enforces the version semantics on
-- every subsequent PUT / DELETE.
UPDATE storage_buckets
SET versioning_status = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SetStorageBucketObjectLock :exec
--: tenant-scoped
-- Replaces the bucket-level object-lock policy (WS-29). When
-- object_lock_enabled is FALSE the mode + days columns are cleared so
-- a re-enable after disable starts from a clean state. The SeaweedFS
-- daemon is updated separately by the storage service via the provider's
-- SetObjectLockConfiguration.
UPDATE storage_buckets
SET object_lock_enabled = $3,
    object_lock_default_mode = $4,
    object_lock_default_retention_days = $5,
    updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
