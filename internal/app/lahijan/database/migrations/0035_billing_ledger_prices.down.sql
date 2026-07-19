-- 0035_billing_ledger_prices: down — drops the prices, ledger_entries, and
-- user_balances tables introduced by the up migration. Reversible: every
-- object created by the up migration is dropped. Drop the trigger-bound
-- tables last so the block_mutation function drops cleanly.

DROP TABLE IF EXISTS user_balances;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS prices;
