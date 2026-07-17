-- 0001_tenants: the top-level isolation boundary (global table, no tenant_id).
-- Every tenant-scoped table later references tenants(id). See ADR-0002.

CREATE TABLE tenants (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

-- slug is the URL-friendly identifier (e.g. "acme"). Unique across the
-- platform because it shows up in routing and must not collide between tenants.
CREATE UNIQUE INDEX uq_tenants_slug              ON tenants (slug) WHERE deleted_at IS NULL;
CREATE INDEX        idx_tenants_active           ON tenants (is_active);
CREATE INDEX        idx_tenants_deleted_at       ON tenants (deleted_at);

COMMENT ON TABLE  tenants        IS 'Top-level tenancy boundary; one row per organization.';
COMMENT ON COLUMN tenants.slug   IS 'URL-friendly identifier; unique among non-deleted tenants.';
COMMENT ON COLUMN tenants.name   IS 'Human-readable display name.';
COMMENT ON COLUMN tenants.is_active IS 'When false, members cannot log into this tenant.';
