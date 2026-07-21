-- 0048_compute_ip_pools_floating_ips.down.sql: reverse of
-- 0048_compute_ip_pools_floating_ips.up.sql (WS-30, ADR-0037).
--
-- Drops the three tables in reverse dependency order:
--   floating_ips   (references ip_pools + tenants + compute_instances)
--   ip_pool_ranges (references ip_pools)
--   ip_pools       (references dns_zones)
--
-- Reversible: re-running up after down returns the schema to the
-- post-WS-30 state. The ON DELETE SET NULL on dns_zones.ptr_zone_id is
-- not exercised here because no rows reference it after the floating_ips
-- + ip_pools tables are dropped.

DROP TABLE IF EXISTS floating_ips;

ALTER TABLE ip_pool_ranges DROP CONSTRAINT IF EXISTS ip_pool_ranges_family_valid;
DROP TABLE IF EXISTS ip_pool_ranges;

DROP TABLE IF EXISTS ip_pools;
