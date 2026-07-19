-- 0033_storage_buckets: per-tenant object storage buckets (WS-16).
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns. storage_buckets is the canonical Lahijan-side record of a
-- single S3 bucket: the row mirrors the SeaweedFS bucket the storage service
-- (WS-16) creates at request time and is the source of truth on the Lahijan
-- side (the SeaweedFS daemon is derived).
--
-- The compound "name" column carries the canonical "<tenant-uuid>-<slug>"
-- form mandated by ADR-0011 and enforced by providers/seaweedfs.BucketName.
-- The compound name is globally unique because SeaweedFS' S3 namespace is
-- cluster-wide; the (tenant_id, slug) pair is unique within a tenant so two
-- tenants can independently use the same slug.
--
-- The row NEVER holds live object data. Quotas are persisted here AND pushed
-- to SeaweedFS via the provider's SetBucketQuota so the daemon enforces the
-- ceiling server-side on every PUT. Usage columns (bytes_used / objects_used)
-- are cached by the WS-17 metering job; the storage module's GET /usage
-- endpoint reads the cache.
--
-- A soft-delete pattern (deleted_at) is used so the audit trail can reference
-- a row even after the user removes the bucket. SeaweedFS itself removes the
-- bucket on delete; the Lahijan row stays for forensics.

CREATE TABLE storage_buckets (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- canonical "<tenant-uuid>-<slug>" form; globally unique because the
    -- SeaweedFS S3 namespace is cluster-wide.
    name            TEXT        NOT NULL,
    -- user-picked slug portion (1-26 lowercase alphanumeric + dashes). The
    -- service layer validates against providers/seaweedfs.validateSlug.
    slug            TEXT        NOT NULL,
    -- the user who created (and therefore owns) the bucket. The tenant admin
    -- can transfer ownership via a future endpoint; for now it stays fixed.
    owner_user_id   UUID        NOT NULL REFERENCES users (id) ON DELETE NO ACTION,
    -- optional user-facing label. Falls back to the slug when empty.
    label           TEXT        NOT NULL DEFAULT '',
    description     TEXT        NOT NULL DEFAULT '',
    -- per-bucket quota ceiling. A zero value on either dimension means
    -- "unlimited on that dimension". The WS-16 storage service pushes the
    -- quota to SeaweedFS on every change so the daemon enforces it.
    quota_bytes     BIGINT      NOT NULL DEFAULT 0,
    quota_objects   BIGINT      NOT NULL DEFAULT 0,
    -- usage cache. Refreshed periodically by the WS-17 metering job; read by
    -- GET /api/v1/storage/buckets/{id}/usage. Bytes are the total object
    -- size in bytes; objects is the total object count.
    bytes_used      BIGINT      NOT NULL DEFAULT 0,
    objects_used    BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- canonical name is globally unique (two tenants cannot own the same name).
CREATE UNIQUE INDEX uq_storage_buckets_name        ON storage_buckets (name);
-- (tenant_id, slug) is unique within a tenant among non-deleted buckets.
CREATE UNIQUE INDEX uq_storage_buckets_tenant_slug ON storage_buckets (tenant_id, slug)
    WHERE deleted_at IS NULL;
-- Hot lookups for the per-tenant list + the "is this bucket owned by tenant X"
-- check the storage service performs on every privileged call.
CREATE INDEX        idx_storage_buckets_tenant_id  ON storage_buckets (tenant_id)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE  storage_buckets             IS 'Per-tenant object storage buckets. Mirrors SeaweedFS buckets; the storage service (WS-16) upserts on every change.';
COMMENT ON COLUMN storage_buckets.name        IS 'Canonical "<tenant-uuid>-<slug>" form (ADR-0011); globally unique.';
COMMENT ON COLUMN storage_buckets.slug        IS 'User-picked slug portion (1-26 lowercase alphanumeric + dashes).';
COMMENT ON COLUMN storage_buckets.owner_user_id IS 'The user who created the bucket; fixed unless transferred by an admin.';
COMMENT ON COLUMN storage_buckets.quota_bytes IS 'Per-bucket size ceiling in bytes; 0 = unlimited. Enforced server-side by SeaweedFS.';
COMMENT ON COLUMN storage_buckets.quota_objects IS 'Per-bucket object-count ceiling; 0 = unlimited. Enforced server-side by SeaweedFS.';
COMMENT ON COLUMN storage_buckets.bytes_used  IS 'Cached total object size in bytes; refreshed by the WS-17 metering job.';
COMMENT ON COLUMN storage_buckets.objects_used IS 'Cached total object count; refreshed by the WS-17 metering job.';
COMMENT ON COLUMN storage_buckets.deleted_at  IS 'Soft-delete timestamp; the row stays for the audit trail after the SeaweedFS bucket is removed.';
