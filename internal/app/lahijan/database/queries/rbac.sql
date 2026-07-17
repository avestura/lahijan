-- RBAC base tables: roles, permissions, role_permissions.
-- All global. Policy enforcement ships in WS-08; this WS only persists data.
-- Optional fields use explicit params; the repository wrapper supplies defaults.

-- name: CreateRole :one
INSERT INTO roles (slug, name, description, is_system)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetRoleByID :one
SELECT * FROM roles WHERE id = $1;

-- name: GetRoleBySlug :one
SELECT * FROM roles WHERE slug = $1 AND deleted_at IS NULL;

-- name: ListRoles :many
SELECT * FROM roles
WHERE deleted_at IS NULL
ORDER BY created_at DESC;

-- name: CreatePermission :one
INSERT INTO permissions (slug, description)
VALUES ($1, $2)
RETURNING *;

-- name: GetPermissionBySlug :one
SELECT * FROM permissions WHERE slug = $1;

-- name: ListPermissions :many
SELECT * FROM permissions ORDER BY slug;

-- name: GrantPermissionToRole :exec
INSERT INTO role_permissions (role_id, permission_id)
VALUES ($1, $2)
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- name: ListPermissionsForRole :many
SELECT p.* FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
WHERE rp.role_id = $1
ORDER BY p.slug;
