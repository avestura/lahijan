-- user_recovery_codes: per-user single-use recovery codes (WS-07c). The
-- code_hash column carries the SHA-256 hex of the raw code; the raw code
-- is never stored.

-- name: CreateRecoveryCode :one
INSERT INTO user_recovery_codes (user_id, code_hash)
VALUES ($1, $2)
RETURNING *;

-- name: GetRecoveryCodeByUserAndHash :one
-- Lookup path: SHA-256 the user-supplied code, find a row scoped by
-- (user_id, hash) where used_at IS NULL.
SELECT * FROM user_recovery_codes
WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: ListRecoveryCodesForUser :many
SELECT * FROM user_recovery_codes
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: ConsumeRecoveryCode :exec
UPDATE user_recovery_codes
SET used_at = now()
WHERE id = $1 AND user_id = $2 AND used_at IS NULL;

-- name: DeleteAllRecoveryCodesForUser :exec
-- Used before regenerating a fresh batch: every old code is invalidated.
DELETE FROM user_recovery_codes WHERE user_id = $1;

-- name: CountUnusedRecoveryCodesForUser :one
SELECT count(*) FROM user_recovery_codes
WHERE user_id = $1 AND used_at IS NULL;
