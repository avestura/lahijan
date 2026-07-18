-- 0022_plugin_kv: per-plugin durable key-value store (WS-10b). Backs the
-- kv_get / kv_set / kv_delete host functions. Per ADR-0012 + WS-10b "kv:
-- per-plugin namespace (no cross-plugin reads)", the (plugin_id, key)
-- pair is the unique key — a plugin cannot address another plugin's
-- namespace because the host function injects the plugin_id from the
-- calling Instance's identity (WS-10b context propagation).
--
-- TTL is supported via an optional expires_at column. Reads filter out
-- expired rows on the read path so the host function never returns
-- stale data; a periodic cleanup job (post-MVP) reclaims the rows.
--
-- tenant_id is NULLABLE here for the same reason as plugins: a
-- platform-wide plugin has NULL tenant_id. The vast majority of rows
-- will be tenant-scoped.

CREATE TABLE plugin_kv (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULLABLE: matches plugins.tenant_id semantics. NULL for rows
    -- owned by platform-wide plugins.
    tenant_id       UUID        REFERENCES tenants (id) ON DELETE CASCADE,
    -- The plugin that owns this row. CASCADE so deleting a plugin
    -- removes its KV namespace atomically.
    plugin_id       UUID        NOT NULL REFERENCES plugins (id) ON DELETE CASCADE,
    -- The key within the plugin's namespace. 1 KiB cap is enforced at
    -- the host function layer; the DB does not cap because TEXT caps
    -- are an antipattern (see database-conventions skill).
    key             TEXT        NOT NULL,
    -- The value bytes. BYTEA (not TEXT) so binary values (a serialised
    -- protobuf, a compressed blob) round-trip cleanly.
    value           BYTEA       NOT NULL,
    -- Optional absolute expiry. NULL means "no TTL". Reads filter
    -- expires_at <= now() out; the cleanup job (post-MVP) reclaims them.
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Composite PK alternative (plugin_id, key) would also work; we use a
-- synthetic id + a unique index because the upsert path (kv_set) needs
-- to ON CONFLICT by (plugin_id, key) and the unique index is the
-- constraint target. The synthetic id is useful for audit/debugging.
CREATE UNIQUE INDEX uq_plugin_kv_plugin_key ON plugin_kv (plugin_id, key);

-- Hot lookups.
CREATE INDEX idx_plugin_kv_plugin_id  ON plugin_kv (plugin_id);
CREATE INDEX idx_plugin_kv_tenant_id  ON plugin_kv (tenant_id) WHERE tenant_id IS NOT NULL;
-- Partial index on unexpired rows so the cleanup job can scan cheaply.
CREATE INDEX idx_plugin_kv_expires_at ON plugin_kv (expires_at)
    WHERE expires_at IS NOT NULL;

COMMENT ON TABLE  plugin_kv              IS 'Per-plugin durable KV store; backs the kv_* host functions (WS-10b).';
COMMENT ON COLUMN plugin_kv.tenant_id   IS 'NULL for rows owned by platform-wide plugins; otherwise the scoping tenant.';
COMMENT ON COLUMN plugin_kv.plugin_id   IS 'Owning plugin; CASCADE on plugins.id delete.';
COMMENT ON COLUMN plugin_kv.value       IS 'Opaque bytes; the host function does not interpret the contents.';
COMMENT ON COLUMN plugin_kv.expires_at  IS 'Absolute expiry; NULL = no TTL. Reads filter expired rows.';
