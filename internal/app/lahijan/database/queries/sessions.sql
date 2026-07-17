-- Sessions: a logical login session (WS-06). Global, backed by an opaque
-- signed cookie whose SHA-256 hash matches sessions.token_hash.

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent, ip_address)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = $1;

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now()
WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeAllSessionsForUser :exec
UPDATE sessions
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: ListSessionsForUser :many
SELECT * FROM sessions
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;
