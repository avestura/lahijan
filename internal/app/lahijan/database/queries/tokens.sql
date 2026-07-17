-- Tokens: refresh_tokens and personal_access_tokens. Both global.
-- Auth issuance/rotation lands in WS-06; this WS persists storage only.

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, expires_at, user_agent, ip_address)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllRefreshTokensForUser :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreatePersonalAccessToken :one
INSERT INTO personal_access_tokens (user_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPersonalAccessTokenByHash :one
SELECT * FROM personal_access_tokens WHERE token_hash = $1;

-- name: TouchPersonalAccessToken :exec
UPDATE personal_access_tokens
SET last_used_at = now()
WHERE token_hash = $1;

-- name: RevokePersonalAccessToken :exec
UPDATE personal_access_tokens
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;
