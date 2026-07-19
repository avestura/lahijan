-- 0035_billing_ledger_prices: the prices catalog + the append-only ledger +
-- the per-user balance cache (WS-17, ADR-0013).
--
-- Per ADR-0013 Lahijan MVP ships ledger-only billing:
--
--   * Admins top up user balances (manual credits).
--   * Users consume services; usage is metered per-minute (rounded up) and
--     debited from their balance via an append-only ledger row.
--   * Balances are cached in user_balances and reconciled from the ledger
--     within 60s of any change (the ledger is the source of truth).
--   * Corrections are NEW ledger rows (type=credit, source=refund); the
--     ledger itself is fully append-only (UPDATE + DELETE rejected by
--     trigger, same shape as audit_log).
--
-- All amounts are INTEGER CENTIMALS (e.g. $1.00 = 100). Float money is a
-- hard rule from the WS-17 DoD; we enforce at the schema level by using
-- INTEGER (no NUMERIC, no REAL).
--
-- Currency: single-currency MVP per WS-17 Open Questions item 1 (default
-- USD; multi-currency is Phase 7). The currency column is retained on
-- each row so the future multi-currency migration only adds enforcement,
-- not new columns.

-- ===========================================================================
-- prices: admin-managed price catalog. One row per (resource_type, unit)
-- combination, time-ranged via effective_from / effective_to so an admin
-- can stage a future price change without invalidating history.
-- ===========================================================================

CREATE TABLE prices (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- The metering resource this price applies to ("compute.cpu",
    -- "compute.ram", "compute.disk", "storage.size", "storage.requests",
    -- "dns.queries", "network.egress"). Free-form text today; a CHECK
    -- could be added once the set stabilises.
    resource_type   TEXT        NOT NULL,
    -- The unit the qty is measured in ("core-hours", "gb-hours",
    -- "gb-month", "count", "requests"). Free-form text today.
    unit            TEXT        NOT NULL,
    -- The unit price in integer centimals. Multiplied by qty (integer) at
    -- charge time; the result is the debit amount in centimals.
    price_cents     BIGINT      NOT NULL,
    -- ISO 4217 currency code. "USD" today; the column exists so a future
    -- WS can add multi-currency without a migration.
    currency        TEXT        NOT NULL DEFAULT 'USD',
    -- Time range this price is effective for. effective_to IS NULL means
    -- "currently in effect"; the repository's GetForPeriod helper picks
    -- the right row for a given metering timestamp.
    effective_from  TIMESTAMPTZ NOT NULL DEFAULT now(),
    effective_to    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot lookups: by tenant + resource (current + historical); uniqueness
-- guarantees at most one "current" price per (tenant, resource, unit).
CREATE INDEX idx_prices_tenant_resource_unit
    ON prices (tenant_id, resource_type, unit);
CREATE UNIQUE INDEX uq_prices_tenant_resource_unit_current
    ON prices (tenant_id, resource_type, unit)
    WHERE effective_to IS NULL;

COMMENT ON TABLE  prices                   IS 'Per-tenant admin-managed price catalog (WS-17). Time-ranged via effective_from/to.';
COMMENT ON COLUMN prices.resource_type     IS 'Metering resource key (compute.cpu, storage.size, dns.queries, ...).';
COMMENT ON COLUMN prices.unit              IS 'Unit the qty is measured in (core-hours, gb-month, count, ...).';
COMMENT ON COLUMN prices.price_cents       IS 'Unit price in integer centimals (1.00 = 100). Multiplied by qty at charge time.';
COMMENT ON COLUMN prices.currency          IS 'ISO 4217 currency code. USD today; multi-currency is Phase 7.';
COMMENT ON COLUMN prices.effective_from    IS 'When this price takes effect. Defaults to now() at insert.';
COMMENT ON COLUMN prices.effective_to      IS 'When this price stops being effective. NULL = currently in effect.';

-- ===========================================================================
-- ledger_entries: append-only per-user ledger. One row per credit (topup /
-- refund) or debit (charge). The sum of (type=credit) - (type=debit) is the
-- user's balance; the user_balances table caches that for fast reads.
--
-- The trigger below rejects UPDATE and DELETE the same way audit_log does
-- (migration 0005) so the ledger is tamper-evident without application
-- discipline. Corrections are NEW rows.
-- ===========================================================================

CREATE TABLE ledger_entries (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE NO ACTION,
    user_id         UUID        NOT NULL REFERENCES users (id)   ON DELETE NO ACTION,
    -- "credit" or "debit". Credits increase the balance (topup, refund);
    -- debits decrease it (charge).
    type            TEXT        NOT NULL,
    -- Integer centimals. Always positive; the sign is encoded in `type`.
    amount_cents    BIGINT      NOT NULL CHECK (amount_cents >= 0),
    currency        TEXT        NOT NULL DEFAULT 'USD',
    -- "topup", "charge", "refund", "adjustment". Free-form today; the
    -- service layer asserts one of these.
    source          TEXT        NOT NULL,
    -- Optional human-readable reference (invoice id, job id, ...).
    reference       TEXT        NOT NULL DEFAULT '',
    -- Idempotency key for metering job de-duplication. The unique index
    -- below makes a duplicate insert fail with a clear error rather than
    -- double-charging. NULLable for admin-issued rows (topup, refund)
    -- that are not idempotent-keyed.
    idempotency_key TEXT,
    -- Free-form structured details about this entry (resource type,
    -- period start/end, breakdown, ...).
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot lookups: per-tenant list, per-user list, by idempotency key.
CREATE INDEX        idx_ledger_tenant_id        ON ledger_entries (tenant_id);
CREATE INDEX        idx_ledger_user_id          ON ledger_entries (tenant_id, user_id, created_at DESC);
CREATE INDEX        idx_ledger_source           ON ledger_entries (source);
CREATE UNIQUE INDEX uq_ledger_idempotency_key   ON ledger_entries (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

COMMENT ON TABLE  ledger_entries                  IS 'Per-user append-only ledger (WS-17, ADR-0013). The sum of credits - debits is the balance.';
COMMENT ON COLUMN ledger_entries.type             IS 'credit | debit. Credits (topup, refund) increase the balance; debits (charge) decrease it.';
COMMENT ON COLUMN ledger_entries.amount_cents     IS 'Amount in integer centimals; always positive. Sign is encoded in type.';
COMMENT ON COLUMN ledger_entries.currency         IS 'ISO 4217 currency code. USD today; multi-currency is Phase 7.';
COMMENT ON COLUMN ledger_entries.source           IS 'topup | charge | refund | adjustment.';
COMMENT ON COLUMN ledger_entries.idempotency_key  IS 'Idempotency key for metering de-duplication; NULL for admin rows.';
COMMENT ON COLUMN ledger_entries.metadata         IS 'Structured details: resource type, period, breakdown, etc.';

-- Block any UPDATE or DELETE on ledger_entries rows. The trigger raises with
-- the same clear "append-only" message shape used by audit_log so the
-- integration test can assert on it.
CREATE OR REPLACE FUNCTION ledger_entries_block_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only: % is not allowed', TG_OP
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER ledger_entries_no_update BEFORE UPDATE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_block_mutation();

CREATE TRIGGER ledger_entries_no_delete BEFORE DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_block_mutation();

-- ===========================================================================
-- user_balances: cached per-user balance (WS-17 DoD item "balance refreshed
-- within 60s of any ledger change"). Updated by the ledger post-processor
-- job after every ledger insert. The cache is *derived*; the ledger is the
-- source of truth, and a reconcile function can rebuild it from scratch.
-- ===========================================================================

CREATE TABLE user_balances (
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id         UUID        NOT NULL REFERENCES users (id)   ON DELETE CASCADE,
    -- Cached balance in integer centimals. Can be negative (a user can
    -- dip slightly below zero before the enforcement job stops their
    -- instances). The reconcile job recomputes from the ledger.
    balance_cents   BIGINT      NOT NULL DEFAULT 0,
    currency        TEXT        NOT NULL DEFAULT 'USD',
    -- Timestamp of the last ledger entry incorporated into balance_cents.
    -- The enforcement job uses this to decide whether a user is in the
    -- zero-balance grace period.
    last_entry_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Timestamp the cache row was last refreshed. Used by the "stale
    -- cache" diagnostic; the WS-17 DoD requires balance < 60s stale.
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);

COMMENT ON TABLE  user_balances                IS 'Cached per-user balance (WS-17). Derived from ledger_entries; reconcile-able from scratch.';
COMMENT ON COLUMN user_balances.balance_cents  IS 'Cached balance in integer centimals; can be slightly negative during the grace window.';
COMMENT ON COLUMN user_balances.last_entry_at  IS 'Timestamp of the latest ledger entry incorporated into balance_cents.';
COMMENT ON COLUMN user_balances.updated_at     IS 'Cache refresh timestamp; must be < 60s stale per the WS-17 DoD.';
