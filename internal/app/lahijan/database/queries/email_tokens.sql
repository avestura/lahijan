-- Email tokens: single-use, expiring tokens for email verification, password
-- reset, and email change (WS-06). Global; only the SHA-256 hash is stored.

-- name: CreateEmailToken :one
INSERT INTO email_tokens (user_id, token_hash, kind, new_email, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetEmailTokenByHash :one
SELECT * FROM email_tokens WHERE token_hash = $1;

-- name: ConsumeEmailToken :execrows
-- Single-use: stamp used_at. The app layer checks used_at IS NULL before
-- consuming, then runs this UPDATE and uses the returned row count to detect
-- a race (0 affected = already consumed or did not exist).
UPDATE email_tokens
SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL;

-- name: RevokeEmailTokensForUser :exec
-- Invalidate every outstanding email token of a kind for a user (e.g. when
-- re-issuing a verification token, revoke the previous one).
UPDATE email_tokens
SET used_at = now()
WHERE user_id = $1 AND kind = $2 AND used_at IS NULL;
