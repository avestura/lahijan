-- 0051_platform_settings: runtime settings an administrator changes from the
-- dashboard (global table, no tenant_id).
--
-- A row overrides the matching configuration default for as long as it
-- exists. Today the only key is `registration_enabled` (boolean): whether
-- anyone may create an account on their own. When there is no row, the value
-- of the `auth.signup.enabled` setting (env LAHIJAN_AUTH_SIGNUP_ENABLED) applies.

CREATE TABLE platform_settings (
    key         TEXT        PRIMARY KEY,
    value       JSONB       NOT NULL,
    updated_by  UUID        REFERENCES users (id) ON DELETE SET NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE platform_settings ADD CONSTRAINT platform_settings_key_valid
    CHECK (key ~ '^[a-z][a-z0-9_]{0,62}$');
