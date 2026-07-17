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
