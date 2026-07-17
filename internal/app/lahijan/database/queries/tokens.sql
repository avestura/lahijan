-- Tokens: refresh_tokens and personal_access_tokens. Both global.
--
-- refresh_tokens belong to a session (session_id, added in WS-06 migration 0009)
-- and are grouped into a rotation family (family_id). Reusing a rotated token
-- revokes the whole family + the owning session (reuse detection).
--
-- PATs carry a scopes array (WS-06 migration 0009) listing the permission slugs
-- the token grants; enforcement lands in WS-08 (RequirePerm).

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, session_id, family_id, token_hash, expires_at, user_agent, ip_address)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
-- Reuse detection: revoke every token in the family, regardless of state.
UPDATE refresh_tokens
SET revoked_at = now()
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokensForSession :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE session_id = $1 AND revoked_at IS NULL;

-- name: RevokeAllRefreshTokensForUser :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreatePersonalAccessToken :one
INSERT INTO personal_access_tokens (user_id, name, token_hash, expires_at, scopes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetPersonalAccessTokenByHash :one
SELECT * FROM personal_access_tokens WHERE token_hash = $1;

-- name: GetPersonalAccessTokenByID :one
SELECT * FROM personal_access_tokens WHERE id = $1;

-- name: ListPersonalAccessTokensForUser :many
SELECT * FROM personal_access_tokens
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: TouchPersonalAccessToken :exec
UPDATE personal_access_tokens
SET last_used_at = now()
WHERE token_hash = $1;

-- name: RevokePersonalAccessToken :exec
UPDATE personal_access_tokens
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokePersonalAccessTokenByID :exec
UPDATE personal_access_tokens
SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;
