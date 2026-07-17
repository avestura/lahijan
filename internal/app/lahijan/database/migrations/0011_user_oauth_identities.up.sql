-- 0011_user_oauth_identities: links a user to an external identity provider
-- (WS-07a). One row per (provider, subject) pair: a user can have many
-- identities (Google + GitHub + Keycloak) but a single (provider, subject) is
-- unique across the platform because subject is provider-scoped.
--
-- Global table (no tenant_id): an external identity belongs to the user, not
-- to any tenant — login is global and a user can be a member of many tenants.
--
-- The access_token and refresh_token columns are stored encrypted (AES-GCM;
-- see auth/secrets.crypto.go) because these are bearer tokens that grant the
-- platform access to the user's account at the IdP. Storing them in plaintext
-- would let anyone with DB read access impersonate the user at the IdP.

CREATE TABLE user_oauth_identities (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- "google" | "github" | "oidc:<provider_key>" (e.g. "oidc:keycloak").
    -- The prefix on OIDC keeps the namespace distinct from OAuth presets.
    provider        TEXT        NOT NULL,
    -- The IdP-stable subject identifier ("sub" claim for OIDC; numeric login
    -- for GitHub; numeric sub for Google).
    subject         TEXT        NOT NULL,
    -- AES-GCM ciphertext of the access token (nonce || ciphertext || tag,
    -- base64-encoded). NULL when the IdP did not return one.
    access_token    TEXT,
    -- AES-GCM ciphertext of the refresh token, same encoding as access_token.
    refresh_token   TEXT,
    -- Scope strings the IdP granted; informational (the IdP is the source of
    -- truth for what the tokens actually allow).
    scopes          TEXT[]      NOT NULL DEFAULT '{}',
    -- When the access_token expires (IdP-supplied). NULL when non-expiring.
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A single (provider, subject) pair maps to exactly one user across the
-- platform; attempting to link a second user to the same pair must fail.
CREATE UNIQUE INDEX uq_user_oauth_identities_provider_subject
    ON user_oauth_identities (provider, subject);

-- A user can have at most one identity per provider (one Google, one GitHub,
-- one OIDC:keycloak). Linking a second Google identity to the same user fails.
CREATE UNIQUE INDEX uq_user_oauth_identities_user_provider
    ON user_oauth_identities (user_id, provider);

-- Hot lookup paths.
CREATE INDEX idx_user_oauth_identities_user_id ON user_oauth_identities (user_id);

COMMENT ON TABLE  user_oauth_identities                        IS 'Links a user to an external identity provider; global, tokens encrypted at rest.';
COMMENT ON COLUMN user_oauth_identities.provider               IS 'Provider key: google | github | oidc:<config_key>.';
COMMENT ON COLUMN user_oauth_identities.subject                IS 'IdP-stable subject identifier (sub claim for OIDC).';
COMMENT ON COLUMN user_oauth_identities.access_token           IS 'AES-GCM ciphertext of the access token; NULL when the IdP returned none.';
COMMENT ON COLUMN user_oauth_identities.refresh_token          IS 'AES-GCM ciphertext of the refresh token; NULL when the IdP returned none.';
COMMENT ON COLUMN user_oauth_identities.scopes                 IS 'Scopes the IdP granted at issue/refresh time; informational.';
COMMENT ON COLUMN user_oauth_identities.expires_at             IS 'When the access_token expires; NULL when non-expiring.';
