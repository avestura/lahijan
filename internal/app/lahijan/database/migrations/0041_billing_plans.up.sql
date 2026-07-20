-- 0041_billing_plans: admin-managed subscription plans (WS-27, ADR-0034).
--
-- A plan is a recurring billing product: a monthly (or yearly) fee that
-- credits the user's balance with an "included quota" amount up-front +
-- unlocks PAYG overage at a (possibly discounted) rate. Plans are
-- tenant-scoped (per ADR-0002) so each deployer curates their own catalog;
-- the Stripe-side Product + Price ids are stored alongside so the gateway
-- can map a Lahijan subscription to its Stripe counterpart.
--
-- Plan rows are mutable (admin can update the price, the included quota,
-- the active flag) but the metering + ledger pipeline never reads from
-- this table at minute-resolution; the active subscription row carries
-- the *frozen* price + quota snapshot at subscription time so an admin
-- price change does not retroactively alter in-flight subscriptions.

CREATE TABLE billing_plans (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,

    -- slug is the URL-safe name the API references (e.g. "starter",
    -- "pro", "enterprise"). Unique within the tenant.
    slug                TEXT        NOT NULL,
    -- name is the user-facing display name ("Starter", "Pro").
    name                TEXT        NOT NULL,
    -- optional longer description for the pricing page / dashboard.
    description         TEXT        NOT NULL DEFAULT '',

    -- Interval the plan bills at. "monthly" or "yearly". Free-form text
    -- today (a CHECK could be added once the set stabilises); the
    -- service layer asserts one of these.
    interval            TEXT        NOT NULL,

    -- Recurring price in integer centimals (1.00 = 100). The amount
    -- charged to the user's card each interval via Stripe.
    price_cents         BIGINT      NOT NULL CHECK (price_cents >= 0),
    currency            TEXT        NOT NULL DEFAULT 'USD',

    -- Included quota credited to the user's ledger each interval, in
    -- integer centimals. The webhook handler that processes
    -- `invoice.paid` posts a credit row of this amount with
    -- source='stripe_subscription'.
    included_quota_cents BIGINT     NOT NULL DEFAULT 0,

    -- Optional discount applied to metered usage while the subscription
    -- is active. Stored as percent (0..100); 0 = no discount. The
    -- metering rollup job reads this off the active subscription row at
    -- rollup time and applies it to the charge.
    overage_discount_percent INTEGER NOT NULL DEFAULT 0 CHECK (overage_discount_percent BETWEEN 0 AND 100),

    -- The Stripe Product + Price ids. NULL until the admin pushes the
    -- plan to Stripe (a one-button action that calls the gateway to
    -- create the upstream Product + Price). When NULL, the plan exists
    -- only on the Lahijan side and cannot be subscribed to.
    stripe_product_id   TEXT,
    stripe_price_id     TEXT,

    -- Active plans appear in the pricing page + the dashboard picker.
    -- Inactive plans are kept for historical ledger references but
    -- cannot be newly subscribed to.
    active              BOOLEAN     NOT NULL DEFAULT true,

    -- Optional ordering hint for the pricing page UI.
    sort_order          INTEGER     NOT NULL DEFAULT 0,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Slug is unique within the tenant.
CREATE UNIQUE INDEX uq_billing_plans_tenant_slug ON billing_plans (tenant_id, slug);
-- Hot lookups: by tenant, by active.
CREATE INDEX        idx_billing_plans_tenant      ON billing_plans (tenant_id, active, sort_order);

COMMENT ON TABLE  billing_plans                       IS 'Admin-managed subscription plans (WS-27, ADR-0034). One row per recurring billing product.';
COMMENT ON COLUMN billing_plans.slug                 IS 'URL-safe name; unique within the tenant.';
COMMENT ON COLUMN billing_plans.interval             IS 'Billing interval: monthly | yearly.';
COMMENT ON COLUMN billing_plans.price_cents          IS 'Recurring price in integer centimals.';
COMMENT ON COLUMN billing_plans.included_quota_cents IS 'Quota credited each interval on invoice.paid.';
COMMENT ON COLUMN billing_plans.overage_discount_percent IS 'Percent discount on metered usage while active (0..100).';
COMMENT ON COLUMN billing_plans.stripe_product_id    IS 'Stripe Product id; NULL until the plan is pushed to Stripe.';
COMMENT ON COLUMN billing_plans.stripe_price_id      IS 'Stripe Price id; NULL until the plan is pushed to Stripe.';
