-- compute_images (WS-14): tenant-scoped image catalog.
-- Featured rows are seeded at bootstrap from conf.providers.incus.featuredImages;
-- custom rows are inserted on user upload. Every query is tenant-scoped.

-- name: CreateComputeImage :one
--: tenant-scoped
INSERT INTO compute_images (
    tenant_id,
    alias,
    source,
    fingerprint,
    type,
    architecture,
    size_bytes,
    properties_json,
    description
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetComputeImageByID :one
--: tenant-scoped
SELECT * FROM compute_images
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeImageByAlias :one
--: tenant-scoped; resolves an alias to a fingerprint at instance-create time.
SELECT * FROM compute_images
WHERE tenant_id = $1 AND alias = $2 AND deleted_at IS NULL;

-- name: GetComputeImageByFingerprint :one
--: tenant-scoped; used by the upload path to detect duplicates.
SELECT * FROM compute_images
WHERE tenant_id = $1 AND fingerprint = $2 AND deleted_at IS NULL;

-- name: ListComputeImages :many
--: tenant-scoped
SELECT * FROM compute_images
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY source DESC, alias ASC
LIMIT $2 OFFSET $3;

-- name: CountComputeImages :one
--: tenant-scoped
SELECT count(*) FROM compute_images
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: UpsertComputeImageFingerprint :exec
--: tenant-scoped; sets the fingerprint on an existing row (the bootstrap
--: path seeds rows with fingerprint='' and the provider resolves them
--: lazily on first use).
UPDATE compute_images
SET fingerprint = $3, size_bytes = $4, properties_json = $5, updated_at = now()
WHERE tenant_id = $1 AND alias = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeImage :exec
--: tenant-scoped
UPDATE compute_images
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
