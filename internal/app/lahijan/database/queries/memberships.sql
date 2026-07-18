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

-- name: AnyTenantRequiresMFAForUser :one
--: user-scoped (cross-tenant; the login flow calls this to decide whether
--: the user must complete an MFA challenge before the real session is
--: issued, per the per-tenant MFA policy in WS-07c).
-- Returns the first tenant the user is a member of that has
-- mfa_required = TRUE; or no rows if none of the user's tenants require
-- MFA. Joining through memberships means a soft-deleted membership or
-- tenant is excluded automatically.
SELECT t.mfa_required FROM tenants t
JOIN memberships m ON m.tenant_id = t.id
WHERE m.user_id = $1
  AND m.deleted_at IS NULL
  AND t.deleted_at IS NULL
  AND t.mfa_required = TRUE
LIMIT 1;


-- name: GetMembershipByUserAndTenant :one
--: user-scoped (cross-tenant check by user + tenant; used by the tenant
--: middleware to verify the caller is a member of the requested tenant). The
--: tenant id comes from the caller, NOT from ctx, because this is the lookup
--: that PROVES the user can adopt that tenant for the request.
SELECT * FROM memberships
WHERE user_id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListPermissionsForUser :many
--: user-scoped (cross-tenant; the policy evaluator calls this for RequirePerm).
-- Returns every permission granted to the user via the role on their membership
-- in the given tenant. Used by RBAC policy enforcement (WS-08).
SELECT p.* FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
JOIN memberships m ON m.role_id = rp.role_id
WHERE m.user_id = $1
  AND m.tenant_id = $2
  AND m.deleted_at IS NULL
ORDER BY p.slug;


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
