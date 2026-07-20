-- 0042_billing_payment_methods: per-user Stripe payment methods (WS-27).
--
-- Stores the Lahijan-side record of a Stripe PaymentMethod attached to a
-- user's Stripe Customer object. The card number / CVC / etc. NEVER live
-- here (Stripe hosts the card input via Payment Element; Lahijan only
-- ever sees the resulting PaymentMethod id). We cache the brand + last4
-- for dashboard rendering + the fingerprint for de-dup so a user cannot
-- attach the same card twice.
--
-- stripe_customer_id is AES-GCM-encrypted at the application layer (the
-- column is TEXT; the value is base64(nonce||ct||tag) per ADR-0018 /
-- ADR-0032). The same Crypto envelope that protects IdP tokens protects
-- the customer id. The fingerprint + brand + last4 are NOT encrypted
-- (they are non-PII display hints).

CREATE TABLE billing_payment_methods (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id                     UUID        NOT NULL REFERENCES users (id)   ON DELETE NO ACTION,

    -- The Stripe PaymentMethod id (e.g. "pm_1AbCdE..."). Stored in the
    -- clear: it is useless without the merchant's API key.
    stripe_payment_method_id    TEXT        NOT NULL,

    -- The Stripe Customer id this PaymentMethod is attached to (e.g.
    -- "cus_1AbCdE..."). AES-GCM-encrypted at the application layer so
    -- a DB-only exfiltration cannot be used to query the Stripe API.
    encrypted_stripe_customer_id TEXT       NOT NULL,

    -- Card brand + last4 cached for the dashboard. Non-PII.
    brand                       TEXT        NOT NULL DEFAULT '',
    last4                       TEXT        NOT NULL DEFAULT '',
    -- The Stripe fingerprint ("fingerprint" field on the Card object).
    -- Used to detect "this card is already attached" without storing
    -- anything sensitive.
    fingerprint                 TEXT        NOT NULL DEFAULT '',

    -- Expiry (cached). When the gateway reports the card is expired we
    -- flip this to NULL and mark the method inactive.
    exp_month                   INTEGER,
    exp_year                    INTEGER,

    -- Whether this is the user's default payment method for one-click
    -- checkout. At most one row per (tenant, user) should have this
    -- set; a partial UNIQUE enforces it.
    is_default                  BOOLEAN     NOT NULL DEFAULT false,

    -- Inactive methods are kept for audit history but cannot be charged.
    active                      BOOLEAN     NOT NULL DEFAULT true,

    -- Free-form structured metadata (gateway-specific hints).
    metadata                    JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot lookups: by tenant, by user, by stripe id.
CREATE INDEX        idx_billing_payment_methods_tenant    ON billing_payment_methods (tenant_id);
CREATE INDEX        idx_billing_payment_methods_user      ON billing_payment_methods (tenant_id, user_id, active);
CREATE UNIQUE INDEX uq_billing_payment_methods_pm_id      ON billing_payment_methods (stripe_payment_method_id);
-- De-dup: at most one row per (tenant, user, fingerprint) when active.
CREATE UNIQUE INDEX uq_billing_payment_methods_user_fp_active
    ON billing_payment_methods (tenant_id, user_id, fingerprint)
    WHERE active AND fingerprint != '';
-- At most one default per user.
CREATE UNIQUE INDEX uq_billing_payment_methods_user_default
    ON billing_payment_methods (tenant_id, user_id)
    WHERE is_default;

COMMENT ON TABLE  billing_payment_methods                            IS 'Per-user Stripe PaymentMethod cache (WS-27). Card data is hosted by Stripe.';
COMMENT ON COLUMN billing_payment_methods.stripe_payment_method_id   IS 'Stripe PaymentMethod id (pm_*). Useless without the merchant key.';
COMMENT ON COLUMN billing_payment_methods.encrypted_stripe_customer_id IS 'AES-GCM-encrypted Stripe Customer id (cus_*).';
COMMENT ON COLUMN billing_payment_methods.fingerprint                IS 'Stripe card fingerprint; used to detect duplicates without storing PII.';
