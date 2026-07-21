-- 0046_dns_domains.down.sql: reverse of 0046_dns_domains.up.sql (WS-28).
--
-- Drops the dns_domains table. The downstream dns_zones table (WS-12)
-- is left intact — dns_domains.zone_id is a nullable FK with ON DELETE
-- SET NULL, so dropping dns_domains does not cascade into dns_zones.

DROP TABLE IF EXISTS dns_domains;
