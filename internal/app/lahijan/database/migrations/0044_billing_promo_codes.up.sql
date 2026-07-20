-- 0044_billing_promo_codes: admin-issued prepaid / promo codes (WS-27).
--
-- A promo code is a one-time-use (or N-time-use) code the admin mints and
-- gives to a user out-of-band. When the user redeems it, a credit row of
-- `credit_cents` lands in the user's ledger with source='topup' and a
-- reference of 'promo_code:<code>'.
--
-- Promo codes are the WS-27 fallback for "prepaid cards" / "voucher
-- codes" — operators who cannot or will not use Stripe can still issue
-- credit vouchers via this surface. The Stripe gateway is not involved.

CREATE TABLE billing_promo_codes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,

    -- The code the user types. URL-safe, uppercase-by-convention.
    -- Unique within the tenant.
    code            TEXT        NOT NULL,

    -- Optional human-readable note for the admin dashboard ("Black Friday
    -- 2026", "User credit for incident #1234", ...).
    note            TEXT        NOT NULL DEFAULT '',

    -- The credit amount in integer centimals applied on redeem.
    credit_cents    BIGINT      NOT NULL CHECK (credit_cents > 0),
    currency        TEXT        NOT NULL DEFAULT 'USD',

    -- Optional plan the code is tied to. When set, redeeming the code
    -- requires the user to be subscribed to (or to subscribe to) that
    -- plan; the credit only applies then. NULL = applies to any user.
    applies_to_plan_id UUID    REFERENCES billing_plans (id) ON DELETE SET NULL,

    -- Maximum number of times the code can be redeemed platform-wide.
    -- NULL = unlimited. 1 = single-use. The redeem service atomically
    -- increments times_used and refuses when times_used >= max_uses.
    max_uses        INTEGER,
    times_used      INTEGER     NOT NULL DEFAULT 0,

    -- Optional expiry. After this timestamp, redeem refuses.
    expires_at      TIMESTAMPTZ,

    -- Soft-delete for the admin UI. Revoked codes cannot be redeemed.
    revoked_at      TIMESTAMPTZ,

    -- Who created the code (audit). The admin user_id; the audit row is
    -- emitted separately via the standard auditEmit pattern.
    created_by      UUID        NOT NULL REFERENCES users (id) ON DELETE NO ACTION,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Code is unique within the tenant.
CREATE UNIQUE INDEX uq_billing_promo_codes_tenant_code ON billing_promo_codes (tenant_id, code);
-- Hot lookups: by tenant, by active.
CREATE INDEX        idx_billing_promo_codes_tenant     ON billing_promo_codes (tenant_id, revoked_at);

COMMENT ON TABLE  billing_promo_codes                IS 'Admin-issued prepaid / promo codes (WS-27). Redeem credits the user''s ledger.';
COMMENT ON COLUMN billing_promo_codes.code           IS 'URL-safe code; unique within the tenant. Uppercase by convention.';
COMMENT ON COLUMN billing_promo_codes.credit_cents   IS 'Credit applied on redeem, in integer centimals.';
COMMENT ON COLUMN billing_promo_codes.max_uses       IS 'Max redeems platform-wide. NULL = unlimited; 1 = single-use.';
COMMENT ON COLUMN billing_promo_codes.times_used     IS 'Current redeem count. Atomically incremented on redeem.';
