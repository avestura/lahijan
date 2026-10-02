-- Users: global table. password_hash is nullable for OAuth/SSO-only users.
-- WS-06 adds display_name, email_verified_at, and locale.
-- Optional fields use explicit params; the repository wrapper supplies defaults.

-- name: CreateUser :one
INSERT INTO users (email, password_hash, is_active, display_name, locale)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 AND deleted_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountUsers :one
SELECT count(*) FROM users WHERE deleted_at IS NULL;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $2, updated_at = now()
WHERE id = $1;

-- name: UpdateUserProfile :exec
UPDATE users
SET display_name = $2, updated_at = now()
WHERE id = $1;

-- name: VerifyUserEmail :exec
UPDATE users
SET email_verified_at = now(), updated_at = now()
WHERE id = $1 AND email_verified_at IS NULL;

-- name: UpdateUserEmail :exec
UPDATE users
SET email = $2, email_verified_at = now(), updated_at = now()
WHERE id = $1;

-- name: UpdateUserLocale :exec
UPDATE users
SET locale = $2, updated_at = now()
WHERE id = $1;

-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = now(), updated_at = now(), is_active = FALSE
WHERE id = $1 AND deleted_at IS NULL;

-- name: SearchUsers :many
-- Admin user list: optional case-insensitive match on email / display name.
-- An empty pattern ('') matches every user.
SELECT * FROM users
WHERE deleted_at IS NULL
  AND ($1::text = '' OR email ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%')
ORDER BY created_at DESC, id
LIMIT $2 OFFSET $3;

-- name: CountSearchUsers :one
SELECT count(*) FROM users
WHERE deleted_at IS NULL
  AND ($1::text = '' OR email ILIKE '%' || $1 || '%' OR display_name ILIKE '%' || $1 || '%');

-- name: SetUserActive :exec
UPDATE users
SET is_active = $2, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListMembershipDetailsForUsers :many
-- Tenant + role summary for a page of users (admin user list / detail).
SELECT m.user_id, m.tenant_id, t.slug AS tenant_slug, t.name AS tenant_name,
       r.slug AS role_slug
FROM memberships m
JOIN tenants t ON t.id = m.tenant_id AND t.deleted_at IS NULL
JOIN roles r   ON r.id = m.role_id
WHERE m.user_id = ANY($1::uuid[]) AND m.deleted_at IS NULL
ORDER BY t.name;

-- name: ListDirectorySourcesForUsers :many
-- Which directory connection (if any) each user in a page was imported from.
SELECT l.user_id, c.id AS connection_id, c.name AS connection_name, c.kind
FROM directory_user_links l
JOIN directory_connections c ON c.id = l.connection_id
WHERE l.user_id = ANY($1::uuid[]);
