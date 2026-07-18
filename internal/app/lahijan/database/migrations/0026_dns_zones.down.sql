-- 0026_dns_zones: down — drops the tenant -> zone mapping for WS-12.
-- Reversible: every object created by the up migration is dropped.

DROP TABLE IF EXISTS dns_zones;
