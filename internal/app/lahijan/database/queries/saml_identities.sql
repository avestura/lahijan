-- user_saml_identities: links between users and external SAML 2.0 identity
-- providers (WS-07b). Global table. The attributes_json column carries a
-- snapshot of the IdP's attribute statement from the most recent login; this
-- query file treats it as opaque JSONB and never inspects its contents.

-- name: CreateSAMLIdentity :one
INSERT INTO user_saml_identities (
    user_id,
    provider,
    name_id,
    idp_entity_id,
    attributes_json
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSAMLIdentity :one
SELECT * FROM user_saml_identities WHERE id = $1;

-- name: GetSAMLIdentityByProviderNameID :one
-- Lookup by (provider, name_id): the path the ACS handler takes after the
-- IdP posts back a signed assertion whose NameID we use to find an existing
-- identity.
SELECT * FROM user_saml_identities
WHERE provider = $1 AND name_id = $2;

-- name: GetSAMLIdentityForUser :one
-- Lookup by (user_id, provider): the path the link/unlink endpoints take.
SELECT * FROM user_saml_identities
WHERE user_id = $1 AND provider = $2;

-- name: ListSAMLIdentitiesForUser :many
SELECT * FROM user_saml_identities
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateSAMLIdentityAttributes :exec
-- Refreshes the attribute snapshot on every login. Called by the IdP service
-- on every successful ACS so the user's profile reflects the latest claims
-- the IdP asserted.
UPDATE user_saml_identities
SET
    attributes_json = $3,
    idp_entity_id   = $4,
    updated_at      = now()
WHERE id = $1
  AND user_id = $2;

-- name: DeleteSAMLIdentity :exec
-- Unlink: removes the (user, provider) SAML link entirely. Enforced "at least
-- one auth method remaining" check happens in the service layer (it counts
-- password_hash + OAuth/OIDC identities + other SAML identities before
-- calling this).
DELETE FROM user_saml_identities
WHERE id = $1 AND user_id = $2;

-- name: CountSAMLIdentitiesForUser :one
SELECT count(*) FROM user_saml_identities WHERE user_id = $1;
