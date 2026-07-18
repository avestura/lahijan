-- 0030_compute_networks: per-tenant network catalog (WS-14).
--
-- An Incus network is a project-scoped network bridge / macvlan / etc. that
-- instances attach to via a nic device. compute_networks is the Lahijan-side
-- record of the networks a tenant owns within their project. ACLs and
-- forwards are stored inline as JSONB because their shape is fluid and the
-- authoritative state lives in Incus; the row is a cache for fast UI
-- rendering and a tenant-scoped join key for audit + billing.

CREATE TABLE compute_networks (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    -- Bridge, macvlan, physical, ... (Incus reports this).
    type            TEXT        NOT NULL DEFAULT 'bridge',
    -- Free-form config map mirroring Incus' shape.
    config_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Cached ACL + forward names so the UI can render counts without
    -- round-tripping to Incus.
    acl_names       TEXT[]      NOT NULL DEFAULT '{}',
    forward_names   TEXT[]      NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_compute_networks_tenant_id ON compute_networks (tenant_id);
CREATE UNIQUE INDEX uq_compute_networks_tenant_name
    ON compute_networks (tenant_id, name)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE compute_networks IS 'Per-tenant Incus network catalog. Mirrors Incus networks within the tenant project.';
