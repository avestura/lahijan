-- 0023_plugin_config: admin-set per-plugin config (WS-10b). Backs the
-- config_get host function. The admin sets these via the admin plugin API
-- (POST /api/v1/admin/plugins/{id}/config); the plugin reads them through
-- the gated host function. Per ADR-0012 + WS-10b "config_get returns
-- admin-set values, never secrets", rows flagged is_secret = true are
-- NEVER returned to the plugin through config_get. The is_secret flag
-- exists so a single admin UI can show "these keys exist but are not
-- surfaced to the plugin" — the plugin's manifest config_schema is the
-- authoritative list of what it expects to see.
--
-- tenant_id is NULLABLE for the same reason as plugins / plugin_kv.

CREATE TABLE plugin_config (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        REFERENCES tenants (id) ON DELETE CASCADE,
    plugin_id       UUID        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    -- The config key within the plugin's namespace; matches a key in
    -- the plugin's manifest config_schema. The host function returns
    -- JSON-encoded values; the plugin decodes per its schema.
    key             TEXT        NOT NULL,
    -- JSON-encoded value. JSONB (not TEXT) so admins can use Postgres
    -- JSON operators when inspecting config in a pinch.
    value           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- When true, the value is a secret (an API token, a password) and
    -- MUST NOT be returned through config_get. The host function
    -- filters these out at read time; the admin UI surfaces them so
    -- the operator knows they exist. Encryption-at-rest is handled at
    -- the application layer (AES-GCM via auth/secrets); the column
    -- itself is plain JSONB because the host function never reads it.
    is_secret       BOOLEAN     NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One value per (plugin, key); upserts target this constraint.
CREATE UNIQUE INDEX uq_plugin_config_plugin_key ON plugin_config (plugin_id, key);

CREATE INDEX idx_plugin_config_plugin_id ON plugin_config (plugin_id);
CREATE INDEX idx_plugin_config_tenant_id ON plugin_config (tenant_id) WHERE tenant_id IS NOT NULL;

COMMENT ON TABLE  plugin_config              IS 'Admin-set per-plugin config; backs config_get host function (WS-10b).';
COMMENT ON COLUMN plugin_config.is_secret   IS 'When true, the value is never returned to the plugin via config_get.';
