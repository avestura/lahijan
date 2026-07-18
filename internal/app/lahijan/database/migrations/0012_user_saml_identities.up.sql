-- 0012_user_saml_identities: links a user to an external SAML 2.0 identity
-- provider (WS-07b). One row per (provider, name_id) pair: a user can have
-- many SAML identities (e.g. one per enterprise IdP) but a single
-- (provider, name_id) is unique across the platform because name_id is
-- IdP-scoped.
--
-- Global table (no tenant_id): an external SAML identity belongs to the user,
-- not to any tenant — login is global and a user can be a member of many
-- tenants (ADR-0002, ADR-0004).
--
-- Unlike user_oauth_identities, this table holds no tokens to encrypt: SAML
-- assertions are short-lived (minutes), and the only stateful piece worth
-- persisting is the attribute set at the time of the most recent login. The
-- attributes_json column carries that snapshot as a free-form JSONB blob whose
-- schema is documented in the application layer (auth/idp).

CREATE TABLE user_saml_identities (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- "saml:<config_key>" (e.g. "saml:entra", "saml:okta"). The "saml:" prefix
    -- keeps the namespace distinct from OAuth presets ("google") and OIDC
    -- providers ("oidc:<key>") in user_oauth_identities.
    provider        TEXT        NOT NULL,
    -- The SAML NameID the IdP asserted for this user. Stable across logins for
    -- the same user at the same IdP; the natural primary lookup key.
    name_id         TEXT        NOT NULL,
    -- The IdP's entity ID (the Issuer element of the SAML response). Stored
    -- alongside name_id so a future migration can disambiguate if two IdPs
    -- ever reuse a name_id (it should not happen, but the cost is tiny).
    idp_entity_id   TEXT        NOT NULL,
    -- Snapshot of the SAML attribute statement at the time of the most recent
    -- login. Schema: { "<attr_name>": ["<value>", ...], ... }. Multi-valued
    -- attributes are arrays per the SAML spec; we never flatten.
    attributes_json JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A single (provider, name_id) pair maps to exactly one user across the
-- platform; attempting to link a second user to the same pair must fail.
CREATE UNIQUE INDEX uq_user_saml_identities_provider_name_id
    ON user_saml_identities (provider, name_id);

-- A user can have at most one identity per SAML provider (one Entra, one
-- Okta, ...). Linking a second SAML identity for the same provider key to the
-- same user fails.
CREATE UNIQUE INDEX uq_user_saml_identities_user_provider
    ON user_saml_identities (user_id, provider);

-- Hot lookup paths.
CREATE INDEX idx_user_saml_identities_user_id ON user_saml_identities (user_id);

COMMENT ON TABLE  user_saml_identities                          IS 'Links a user to an external SAML 2.0 IdP; global, no tokens stored (assertions are short-lived).';
COMMENT ON COLUMN user_saml_identities.provider                 IS 'Provider key: saml:<config_key>.';
COMMENT ON COLUMN user_saml_identities.name_id                  IS 'SAML NameID the IdP asserted; stable across logins.';
COMMENT ON COLUMN user_saml_identities.idp_entity_id            IS 'The IdP entity ID (Issuer element). Stored to disambiguate if IdPs ever reuse a name_id.';
COMMENT ON COLUMN user_saml_identities.attributes_json          IS 'Snapshot of the attribute statement from the most recent login: {attr: [values]}.';
