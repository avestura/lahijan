-- 0033_storage_buckets: down — drops the per-tenant object storage buckets
-- table. Reversible: every object created by the up migration is dropped.

DROP TABLE IF EXISTS storage_buckets;
