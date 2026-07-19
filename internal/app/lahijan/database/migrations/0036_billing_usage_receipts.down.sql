-- 0036_billing_usage_receipts: down — drops the usage_events + receipts
-- tables. Reversible: every object created by the up migration is dropped.

DROP TABLE IF EXISTS receipts;
DROP TABLE IF EXISTS usage_events;
