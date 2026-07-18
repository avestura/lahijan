-- 0020_plugins: installed WASM plugins (WS-10a/b/c). Per ADR-0012 every
-- plugin is a wazero-sandboxed module with an admin-approved permission
-- manifest. tenant_id is NULLABLE here because a plugin may be tenant-scoped
-- (installed for one tenant) OR platform-wide (installed by a platform admin
-- for every tenant). The vast majority are tenant-scoped; the platform-wide
-- case is reserved for the marketplace (WS-10c) and for first-party plugins
-- the operator ships with the deploy.
--
-- The wasm blob itself (the compiled .wasm bytes) lives in this table as a
-- BYTEA column. MVP scope is single-node, so a local BYTEA is fine; a future
-- multi-node WS may move it to SeaweedFS (the S3 backend) and store a key
-- here instead. The schema makes that swap a single migration.

CREATE TABLE plugins (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NULLABLE: see comment above. NULL = platform-wide plugin.
    tenant_id       UUID        REFERENCES tenants (id) ON DELETE CASCADE,
    -- human-readable name scoped to (tenant_id, name, version); see unique
    -- index below.
    name            TEXT        NOT NULL,
    version         TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    -- sha256 of wasm_bytes; surfaced in the admin UI so two uploads of the
    -- same module are visibly the same artifact. The unique index on
    -- (tenant_id, name, version) catches accidental re-uploads; the hash
    -- catches intentional swaps.
    wasm_hash       TEXT        NOT NULL,
    -- raw compiled WASM bytes. See comment above re: future SeaweedFS move.
    wasm_bytes      BYTEA       NOT NULL,
    -- size of wasm_bytes in bytes; denormalised so the admin UI can render
    -- it without a length() call on the BYTEA column.
    wasm_size       BIGINT      NOT NULL,
    -- parsed manifest, kept verbatim so the admin can see exactly what the
    -- plugin declared at upload time. The schema is documented in
    -- internal/app/lahijan/wasm/manifest/manifest.go.
    manifest_json   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- pending  = uploaded but not yet enabled; the admin must grant at least
    --            one permission (or explicitly enable) before the runtime
    --            will instantiate it.
    -- active   = enabled; the runtime may instantiate it on demand.
    -- disabled = explicitly turned off by an admin; persists grants.
    status          TEXT        NOT NULL DEFAULT 'pending',
    -- optional signature blob (cosign / sigstore); when non-empty the
    -- upload flow verifies it against a pinned public key before persisting.
    -- WS-10a accepts and stores the field; verification lands in a follow-up
    -- (see WS-10a "Open questions" item 2).
    signature       BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- (tenant_id, name, version) is unique among platform-wide (tenant_id IS
-- NULL) plugins AND among tenant-scoped ones. A partial index per branch
-- keeps NULLs distinct from non-NULLs (the standard unique-on-nullable
-- pattern in Postgres).
CREATE UNIQUE INDEX uq_plugins_platform_name_version
    ON plugins (name, version) WHERE tenant_id IS NULL;
CREATE UNIQUE INDEX uq_plugins_tenant_name_version
    ON plugins (tenant_id, name, version) WHERE tenant_id IS NOT NULL;

-- Hot lookups for the admin "list" + "filter by tenant" paths.
CREATE INDEX idx_plugins_tenant_id ON plugins (tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX idx_plugins_status   ON plugins (status);
CREATE INDEX idx_plugins_wasm_hash ON plugins (wasm_hash);

COMMENT ON TABLE  plugins              IS 'Installed WASM plugins; wazero-sandboxed, admin-approved permissions.';
COMMENT ON COLUMN plugins.tenant_id    IS 'NULL for platform-wide plugins; otherwise the tenant the plugin is scoped to.';
COMMENT ON COLUMN plugins.wasm_hash    IS 'sha256 of wasm_bytes (hex); surfaced in the admin UI.';
COMMENT ON COLUMN plugins.wasm_bytes   IS 'Compiled .wasm bytes; MVP stores inline, future WS may externalise to SeaweedFS.';
COMMENT ON COLUMN plugins.manifest_json IS 'Parsed lahijan.manifest.yaml kept verbatim so the admin sees exactly what the plugin declared.';
COMMENT ON COLUMN plugins.status       IS 'pending | active | disabled; only active plugins can be instantiated.';
COMMENT ON COLUMN plugins.signature    IS 'Optional signed-manifest blob (cosign / sigstore); verification lands post-WS-10a.';

-- Add a CHECK on status so a typo cannot strand a row in an unknown state.
ALTER TABLE plugins ADD CONSTRAINT plugins_status_valid
    CHECK (status IN ('pending', 'active', 'disabled'));
