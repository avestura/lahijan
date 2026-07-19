-- 0032_dns_records: down — drops the per-tenant DNS records table.
-- Reversible: every object created by the up migration is dropped.

DROP TABLE IF EXISTS dns_records;
