-- Tenants: global table, the top-level isolation boundary.
-- Per ADR-0002, every tenant-scoped table references tenants(id).
-- Optional fields use explicit params (not COALESCE) so sqlc emits concrete
-- types; the repository wrapper supplies defaults for omitted values.

-- name: CreateTenant :one
INSERT INTO tenants (slug, name, is_active)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1;

-- name: GetTenantBySlug :one
SELECT * FROM tenants WHERE slug = $1 AND deleted_at IS NULL;

-- name: ListTenants :many
SELECT * FROM tenants
WHERE deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountTenants :one
SELECT count(*) FROM tenants WHERE deleted_at IS NULL;

-- name: SetTenantActive :exec
UPDATE tenants
SET is_active = $2, updated_at = now()
WHERE id = $1;

-- name: SoftDeleteTenant :exec
UPDATE tenants
SET deleted_at = now(), updated_at = now(), is_active = FALSE
WHERE id = $1 AND deleted_at IS NULL;
