-- Memberships: tenant-scoped. This is the canonical example of tenant scoping.
-- Every tenant-scoped query takes tenant_id as its first parameter so the
-- repository layer can bake it in from the request context (ADR-0002).

-- name: CreateMembership :one
--: tenant-scoped
INSERT INTO memberships (tenant_id, user_id, role_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetMembership :one
--: tenant-scoped
SELECT * FROM memberships
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetMembershipByUser :one
--: tenant-scoped
SELECT * FROM memberships
WHERE tenant_id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: ListMembershipsForTenant :many
--: tenant-scoped
SELECT * FROM memberships
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountMembershipsForTenant :one
--: tenant-scoped
SELECT count(*) FROM memberships
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: ListMembershipsForUser :many
--: user-scoped (cross-tenant; used to list the tenants a user belongs to)
SELECT * FROM memberships
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: SetMembershipRole :exec
--: tenant-scoped
UPDATE memberships
SET role_id = $3, updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteMembership :exec
--: tenant-scoped
UPDATE memberships
SET deleted_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
