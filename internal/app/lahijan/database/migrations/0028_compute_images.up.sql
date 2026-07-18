-- 0028_compute_images: per-tenant image catalog (WS-14).
--
-- An image is the template an instance is created from (e.g. ubuntu/24.04).
-- Lahijan's view of an image is a tenant-scoped pointer to either:
--
--   * a featured public image (source = "featured"; alias comes from
--     conf.providers.incus.featuredImages; the row is a cache so the UI
--     does not need to round-trip to the daemon to render the picker).
--   * a custom image the tenant imported (source = "custom"; the row
--     records the fingerprint + the alias the tenant asked for).
--
-- The fingerprint is the canonical id Incus uses internally; the alias is
-- the human-friendly name users pick at instance-create time. The (tenant,
-- alias) pair is unique among non-deleted rows.

CREATE TABLE compute_images (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- Human-friendly alias ("ubuntu/24.04"). Unique within (tenant_id, alias).
    alias           TEXT        NOT NULL,
    -- "featured" (seeded from conf) or "custom" (user-uploaded).
    source          TEXT        NOT NULL DEFAULT 'featured',
    -- The Incus-assigned sha256 fingerprint; "" until the image is resolved.
    fingerprint     TEXT        NOT NULL DEFAULT '',
    -- "container" or "virtual-machine". Empty means container.
    type            TEXT        NOT NULL DEFAULT 'container',
    -- CPU architecture ("x86_64", "aarch64"). Empty means default.
    architecture    TEXT        NOT NULL DEFAULT '',
    -- Image size in bytes (cached for UI).
    size_bytes      BIGINT      NOT NULL DEFAULT 0,
    -- Free-form properties Incus reports (os, release, variant, ...).
    properties_json JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Optional description / public-alias note.
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_compute_images_tenant_id ON compute_images (tenant_id);
CREATE UNIQUE INDEX uq_compute_images_tenant_alias
    ON compute_images (tenant_id, alias)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_compute_images_tenant_fingerprint
    ON compute_images (tenant_id, fingerprint)
    WHERE fingerprint <> '';

COMMENT ON TABLE  compute_images            IS 'Per-tenant image catalog. Mirrors the Incus image store; featured rows are seeded from conf.';
COMMENT ON COLUMN compute_images.source     IS '"featured" (seeded from conf) or "custom" (user-uploaded).';
COMMENT ON COLUMN compute_images.fingerprint IS 'The Incus-assigned sha256 fingerprint; canonical id for source-of-truth lookups.';
