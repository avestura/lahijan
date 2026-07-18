-- user_totp_secrets: per-user TOTP (RFC 6238) secret used as a second
-- factor at login (WS-07c). The secret column carries AES-GCM ciphertext
-- produced by auth/secrets.Crypto; this query file treats it as opaque
-- TEXT and never inspects its contents.

-- name: CreateTOTPSecret :one
-- Upsert: replace any existing row for this user_id with a fresh secret.
-- A re-enrollment invalidates the old secret across every authenticator app
-- the user had it loaded into.
INSERT INTO user_totp_secrets (user_id, secret)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET secret = EXCLUDED.secret, confirmed_at = NULL, updated_at = now()
RETURNING *;

-- name: GetTOTPSecret :one
SELECT * FROM user_totp_secrets WHERE user_id = $1;

-- name: ConfirmTOTPSecret :exec
UPDATE user_totp_secrets
SET confirmed_at = now(), updated_at = now()
WHERE user_id = $1 AND confirmed_at IS NULL;

-- name: DeleteTOTPSecret :exec
DELETE FROM user_totp_secrets WHERE user_id = $1;
