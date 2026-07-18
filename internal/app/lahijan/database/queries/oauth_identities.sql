-- user_oauth_identities: links between users and external identity providers.
-- Global table. Tokens (access_token, refresh_token) are stored AES-GCM
-- encrypted at the application layer; this query file treats them as opaque
-- TEXT and never inspects their contents.

-- name: CreateOAuthIdentity :one
INSERT INTO user_oauth_identities (
    user_id,
    provider,
    subject,
    access_token,
    refresh_token,
    scopes,
    expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetOAuthIdentity :one
SELECT * FROM user_oauth_identities WHERE id = $1;

-- name: GetOAuthIdentityByProviderSubject :one
-- Lookup by (provider, subject): the path the callback handler takes after
-- the IdP redirects back with a code (the code is exchanged for tokens +
-- profile, and the profile's subject is used to find an existing identity).
SELECT * FROM user_oauth_identities
WHERE provider = $1 AND subject = $2;

-- name: GetOAuthIdentityForUser :one
-- Lookup by (user_id, provider): the path the link/unlink endpoints take.
SELECT * FROM user_oauth_identities
WHERE user_id = $1 AND provider = $2;

-- name: ListOAuthIdentitiesForUser :many
SELECT * FROM user_oauth_identities
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateOAuthIdentityTokens :exec
-- Rotates the stored tokens (and scopes + expiry) on every login or refresh.
-- Called by the IdP service when the IdP hands back a fresh access_token.
UPDATE user_oauth_identities
SET
    access_token  = $3,
    refresh_token = $4,
    scopes        = $5,
    expires_at    = $6,
    updated_at    = now()
WHERE id = $1
  AND user_id = $2;

-- name: DeleteOAuthIdentity :exec
-- Unlink: removes the (user, provider) link entirely. Enforced "at least one
-- auth method remaining" check happens in the service layer (it counts
-- password_hash + other identities + SAML links before calling this).
DELETE FROM user_oauth_identities
WHERE id = $1 AND user_id = $2;

-- name: CountOAuthIdentitiesForUser :one
SELECT count(*) FROM user_oauth_identities WHERE user_id = $1;
