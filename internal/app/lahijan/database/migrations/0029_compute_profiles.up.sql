-- 0029_compute_profiles: per-tenant profile catalog (WS-14).
--
-- An Incus profile is a named bundle of config + devices applied to
-- instances. Per ADR-0010 we expose the full surface: users create and
-- manage profiles within their tenant's project. compute_profiles is the
-- Lahijan-side record (the authoritative state lives in Incus).

CREATE TABLE compute_profiles (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- Profile name; unique within (tenant_id, name).
    name            TEXT        NOT NULL,
    -- Optional description.
    description     TEXT        NOT NULL DEFAULT '',
    -- Free-form config + devices mirroring Incus' shape:
    --   {config: {...}, devices: {...}}
    config_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_compute_profiles_tenant_id ON compute_profiles (tenant_id);
CREATE UNIQUE INDEX uq_compute_profiles_tenant_name
    ON compute_profiles (tenant_id, name)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE compute_profiles IS 'Per-tenant Incus profile catalog. Mirrors Incus profiles within the tenant project.';
