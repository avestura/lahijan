-- 0047_storage_lifecycle_versioning.down.sql: reverse of
-- 0047_storage_lifecycle_versioning.up.sql (WS-29, ADR-0036).
--
-- Drops the storage_lifecycle_rules table + the storage_buckets columns +
-- the constraints added by the up migration. Reversible: re-running up
-- after down returns the schema to the post-WS-29 state.

DROP TABLE IF EXISTS storage_lifecycle_rules;

ALTER TABLE storage_buckets DROP CONSTRAINT IF EXISTS storage_buckets_object_lock_coherent;
ALTER TABLE storage_buckets DROP CONSTRAINT IF EXISTS storage_buckets_object_lock_days_valid;
ALTER TABLE storage_buckets DROP CONSTRAINT IF EXISTS storage_buckets_object_lock_mode_valid;
ALTER TABLE storage_buckets DROP CONSTRAINT IF EXISTS storage_buckets_versioning_status_valid;

ALTER TABLE storage_buckets
    DROP COLUMN IF EXISTS object_lock_default_retention_days,
    DROP COLUMN IF EXISTS object_lock_default_mode,
    DROP COLUMN IF EXISTS object_lock_enabled,
    DROP COLUMN IF EXISTS versioning_status;
