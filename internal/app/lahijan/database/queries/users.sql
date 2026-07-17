-- Users: global table. password_hash is nullable for OAuth/SSO-only users.
-- Optional fields use explicit params; the repository wrapper supplies defaults.

-- name: CreateUser :one
INSERT INTO users (email, password_hash, is_active)
VALUES ($1, $2, $3)
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

-- name: SoftDeleteUser :exec
UPDATE users
SET deleted_at = now(), updated_at = now(), is_active = FALSE
WHERE id = $1 AND deleted_at IS NULL;
