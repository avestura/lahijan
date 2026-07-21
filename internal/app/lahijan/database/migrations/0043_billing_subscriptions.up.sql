-- 0043_billing_subscriptions: per-user recurring subscriptions (WS-27).
--
-- A subscription links a user to a plan; the Stripe-side Subscription id
-- is stored alongside so webhook events can be reconciled. The row
-- captures a *frozen* snapshot of the plan's price + included quota +
-- overage discount at subscription time so admin edits to the plan do
-- not retroactively alter in-flight subscriptions.
--
-- Lifecycle: active -> canceled (admin or user) -> expired (Stripe says
-- it ended). The webhook handler flips the row to `expired` when the
-- `customer.subscription.deleted` event lands.

CREATE TABLE billing_subscriptions (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    user_id                     UUID        NOT NULL REFERENCES users (id)   ON DELETE NO ACTION,
    plan_id                     UUID        NOT NULL REFERENCES billing_plans (id) ON DELETE NO ACTION,

    -- The Stripe Subscription id (e.g. "sub_1AbCdE..."). Unique across
    -- the platform because Stripe's namespace is per-merchant but a
    -- subscription id is never reused.
    stripe_subscription_id      TEXT        NOT NULL,

    -- Frozen snapshot of the plan's pricing at subscription time. The
    -- metering rollup + the webhook handler read these columns (NOT the
    -- billing_plans row) so admin edits to the plan do not affect
    -- in-flight subscriptions.
    interval                    TEXT        NOT NULL,
    price_cents                 BIGINT      NOT NULL,
    currency                    TEXT        NOT NULL DEFAULT 'USD',
    included_quota_cents        BIGINT      NOT NULL DEFAULT 0,
    overage_discount_percent    INTEGER     NOT NULL DEFAULT 0,

    -- Lifecycle: active | canceled | expired.
    status                      TEXT        NOT NULL DEFAULT 'active',

    -- When the current billing period ends (Stripe's
    -- current_period_end). The webhook handler updates this on every
    -- `customer.subscription.updated` event.
    current_period_end          TIMESTAMPTZ,

    -- When the subscription was canceled (admin/user) or expired
    -- (Stripe). NULL while active.
    canceled_at                 TIMESTAMPTZ,

    -- Free-form metadata.
    metadata                    JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot lookups: by tenant, by user, by stripe id.
CREATE INDEX        idx_billing_subscriptions_tenant   ON billing_subscriptions (tenant_id, status);
CREATE INDEX        idx_billing_subscriptions_user     ON billing_subscriptions (tenant_id, user_id, status);
CREATE UNIQUE INDEX uq_billing_subscriptions_stripe_id ON billing_subscriptions (stripe_subscription_id);

COMMENT ON TABLE  billing_subscriptions                          IS 'Per-user recurring subscriptions (WS-27). Lifecycle: active -> canceled -> expired.';
COMMENT ON COLUMN billing_subscriptions.stripe_subscription_id   IS 'Stripe Subscription id (sub_*). Unique platform-wide.';
COMMENT ON COLUMN billing_subscriptions.price_cents              IS 'Frozen price snapshot at subscription time; admin plan edits do not alter this.';
COMMENT ON COLUMN billing_subscriptions.included_quota_cents     IS 'Frozen included-quota snapshot at subscription time.';
COMMENT ON COLUMN billing_subscriptions.overage_discount_percent IS 'Frozen overage-discount snapshot at subscription time.';
