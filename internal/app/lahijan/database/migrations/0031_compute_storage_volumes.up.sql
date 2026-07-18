-- 0031_compute_storage_volumes: per-tenant custom storage volumes (WS-14).
--
-- An Incus storage volume is a project-scoped disk the user can attach to
-- instances via a disk device. compute_storage_volumes is the Lahijan-side
-- record of the custom volumes a tenant owns within their project. Image
-- and container volumes are NOT tracked here (they are derived from the
-- owning instance / image).

CREATE TABLE compute_storage_volumes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    -- Volume type: "custom" (the only one we track) | "virtual-machine" |
    -- "container" | "image". Lahijan only manages "custom" today.
    type            TEXT        NOT NULL DEFAULT 'custom',
    -- The storage pool the volume lives in ("default" in single-pool MVP).
    pool_name       TEXT        NOT NULL DEFAULT 'default',
    -- Free-form config map mirroring Incus' shape (size, block.filesystem,
    -- security.shifted, ...).
    config_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_compute_storage_volumes_tenant_id ON compute_storage_volumes (tenant_id);
CREATE UNIQUE INDEX uq_compute_storage_volumes_tenant_name
    ON compute_storage_volumes (tenant_id, pool_name, name)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE compute_storage_volumes IS 'Per-tenant custom storage volumes. Mirrors Incus volumes within the tenant project.';
