-- 0034_storage_credentials: down — drops the per-tenant object storage
-- credentials table. Reversible: every object created by the up migration
-- is dropped.

DROP TABLE IF EXISTS storage_credentials;
