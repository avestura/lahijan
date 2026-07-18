-- user_webauthn_credentials: per-user WebAuthn / passkey credentials (WS-07c).

-- name: CreateWebauthnCredential :one
INSERT INTO user_webauthn_credentials (
    user_id,
    credential_id,
    public_key,
    aaguid,
    sign_count,
    transports,
    name
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetWebauthnCredential :one
SELECT * FROM user_webauthn_credentials WHERE id = $1;

-- name: GetWebauthnCredentialByUserAndID :one
-- Lookup used at assertion time: the credential_id from the browser is the
-- natural key (alongside user_id) to find the stored public_key + sign_count
-- the RP needs to verify the signature.
SELECT * FROM user_webauthn_credentials
WHERE user_id = $1 AND credential_id = $2;

-- name: ListWebauthnCredentialsForUser :many
SELECT * FROM user_webauthn_credentials
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateWebauthnSignCount :exec
-- Bumps the sign counter on every successful assertion; the RP rejects any
-- future assertion whose count is not strictly greater.
UPDATE user_webauthn_credentials
SET sign_count = $3, last_used_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: DeleteWebauthnCredential :exec
DELETE FROM user_webauthn_credentials WHERE id = $1 AND user_id = $2;

-- name: CountWebauthnCredentialsForUser :one
SELECT count(*) FROM user_webauthn_credentials WHERE user_id = $1;
