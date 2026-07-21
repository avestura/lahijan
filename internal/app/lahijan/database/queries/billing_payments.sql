-- Billing queries (WS-27, ADR-0034). Payment gateway (Stripe) + plans +
-- subscriptions + promo codes + webhook events. Five new tables, each
-- tenant-scoped via WithTenant at the repository seam. The
-- billing_webhook_events table is the idempotency boundary for Stripe
-- webhook delivery; the UNIQUE on stripe_event_id is what makes a
-- duplicate Stripe delivery a no-op rather than a double-credit.

-- ===========================================================================
-- billing_plans: admin-managed subscription catalog.
-- ===========================================================================

-- name: CreateBillingPlan :one
--: tenant-scoped
INSERT INTO billing_plans (
    tenant_id, slug, name, description, interval,
    price_cents, currency, included_quota_cents, overage_discount_percent,
    stripe_product_id, stripe_price_id, active, sort_order
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: GetBillingPlanByID :one
--: tenant-scoped
SELECT * FROM billing_plans
WHERE tenant_id = $1 AND id = $2;

-- name: GetBillingPlanBySlug :one
--: tenant-scoped
SELECT * FROM billing_plans
WHERE tenant_id = $1 AND slug = $2;

-- name: ListBillingPlans :many
--: tenant-scoped; ordered by sort_order then name so the pricing
--: page + dashboard render a stable list.
SELECT * FROM billing_plans
WHERE tenant_id = $1
ORDER BY sort_order ASC, name ASC
LIMIT $2 OFFSET $3;

-- name: ListActiveBillingPlans :many
--: tenant-scoped; only active plans, for the public pricing page.
SELECT * FROM billing_plans
WHERE tenant_id = $1 AND active = true
ORDER BY sort_order ASC, name ASC
LIMIT $2 OFFSET $3;

-- name: CountBillingPlans :one
--: tenant-scoped.
SELECT count(*) FROM billing_plans WHERE tenant_id = $1;

-- name: CountActiveBillingPlans :one
--: tenant-scoped.
SELECT count(*) FROM billing_plans WHERE tenant_id = $1 AND active = true;

-- name: UpdateBillingPlan :exec
--: tenant-scoped; replaces the user-editable fields. The Stripe
--: ids columns are updated separately via SetBillingPlanStripeIDs
--: so a config edit does not require re-pushing the plan to Stripe.
UPDATE billing_plans
SET
    name                    = $3,
    description             = $4,
    interval                = $5,
    price_cents             = $6,
    currency                = $7,
    included_quota_cents    = $8,
    overage_discount_percent = $9,
    active                  = $10,
    sort_order              = $11,
    updated_at              = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetBillingPlanStripeIDs :exec
--: tenant-scoped; records the Stripe Product + Price ids after the
--: admin pushes the plan to Stripe.
UPDATE billing_plans
SET stripe_product_id = $3, stripe_price_id = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteBillingPlan :exec
--: tenant-scoped; hard delete. Plans referenced by an existing
--: subscription row cannot be deleted (FK ON DELETE NO ACTION on
--: billing_subscriptions.plan_id); the service layer should
--: deactivate instead.
DELETE FROM billing_plans
WHERE tenant_id = $1 AND id = $2;

-- ===========================================================================
-- billing_payment_methods: per-user Stripe PaymentMethod cache.
-- ===========================================================================

-- name: CreateBillingPaymentMethod :one
--: tenant-scoped
INSERT INTO billing_payment_methods (
    tenant_id, user_id, stripe_payment_method_id,
    encrypted_stripe_customer_id,
    brand, last4, fingerprint,
    exp_month, exp_year, is_default, active, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetBillingPaymentMethodByID :one
--: tenant-scoped
SELECT * FROM billing_payment_methods
WHERE tenant_id = $1 AND id = $2;

-- name: GetBillingPaymentMethodByStripeID :one
--: tenant-scoped
SELECT * FROM billing_payment_methods
WHERE tenant_id = $1 AND stripe_payment_method_id = $2;

-- name: ListBillingPaymentMethodsForUser :many
--: tenant-scoped; only active methods, newest first.
SELECT * FROM billing_payment_methods
WHERE tenant_id = $1 AND user_id = $2 AND active = true
ORDER BY is_default DESC, created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountBillingPaymentMethodsForUser :one
--: tenant-scoped
SELECT count(*) FROM billing_payment_methods
WHERE tenant_id = $1 AND user_id = $2 AND active = true;

-- name: ListBillingPaymentMethodsByFingerprint :many
--: tenant-scoped; used by the dedup check before attaching a new card.
SELECT * FROM billing_payment_methods
WHERE tenant_id = $1 AND user_id = $2 AND fingerprint = $3 AND active = true;

-- name: SetDefaultBillingPaymentMethod :exec
--: tenant-scoped; clears any prior default for the user + sets the
--: new default. Two statements inside a tx; the service layer wraps
--: both in a single transaction.
UPDATE billing_payment_methods
SET is_default = false, updated_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND is_default = true;

-- name: MarkDefaultBillingPaymentMethod :exec
--: tenant-scoped; sets is_default=true on the supplied id. Pair with
--: ClearDefaultBillingPaymentMethod inside a tx.
UPDATE billing_payment_methods
SET is_default = true, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: DeactivateBillingPaymentMethod :exec
--: tenant-scoped; soft-delete. The row stays for audit history; the
--: Stripe PaymentMethod itself is detached at the gateway layer.
UPDATE billing_payment_methods
SET active = false, is_default = false, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- ===========================================================================
-- billing_subscriptions: per-user recurring subscriptions.
-- ===========================================================================

-- name: CreateBillingSubscription :one
--: tenant-scoped
INSERT INTO billing_subscriptions (
    tenant_id, user_id, plan_id, stripe_subscription_id,
    interval, price_cents, currency,
    included_quota_cents, overage_discount_percent,
    status, current_period_end, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetBillingSubscriptionByID :one
--: tenant-scoped
SELECT * FROM billing_subscriptions
WHERE tenant_id = $1 AND id = $2;

-- name: GetBillingSubscriptionByStripeID :one
--: tenant-scoped; used by the webhook handler to reconcile.
SELECT * FROM billing_subscriptions
WHERE tenant_id = $1 AND stripe_subscription_id = $2;

-- name: ListBillingSubscriptionsForUser :many
--: tenant-scoped; only active + canceled (expired ones drop off the
--: dashboard after a configurable retention period).
SELECT * FROM billing_subscriptions
WHERE tenant_id = $1 AND user_id = $2 AND status != 'expired'
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountBillingSubscriptionsForUser :one
--: tenant-scoped
SELECT count(*) FROM billing_subscriptions
WHERE tenant_id = $1 AND user_id = $2 AND status != 'expired';

-- name: ListActiveBillingSubscriptions :many
--: tenant-scoped; used by the metering rollup to find users with
--: an overage discount.
SELECT * FROM billing_subscriptions
WHERE tenant_id = $1 AND status = 'active';

-- name: SetBillingSubscriptionStatus :exec
--: tenant-scoped; used by the webhook handler to flip status +
--: current_period_end + canceled_at.
UPDATE billing_subscriptions
SET
    status              = $3,
    current_period_end  = $4,
    canceled_at         = $5,
    updated_at          = now()
WHERE tenant_id = $1 AND id = $2;

-- ===========================================================================
-- billing_promo_codes: admin-issued prepaid / promo codes.
-- ===========================================================================

-- name: CreateBillingPromoCode :one
--: tenant-scoped
INSERT INTO billing_promo_codes (
    tenant_id, code, note, credit_cents, currency,
    applies_to_plan_id, max_uses, expires_at, created_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetBillingPromoCodeByID :one
--: tenant-scoped
SELECT * FROM billing_promo_codes
WHERE tenant_id = $1 AND id = $2;

-- name: GetBillingPromoCodeByCode :one
--: tenant-scoped; used by the redeem path. Case-sensitive on the code
--: (admin normalises to uppercase at create time).
SELECT * FROM billing_promo_codes
WHERE tenant_id = $1 AND code = $2;

-- name: ListBillingPromoCodes :many
--: tenant-scoped; ordered by created_at desc so the admin dashboard
--: shows recent codes first.
SELECT * FROM billing_promo_codes
WHERE tenant_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountBillingPromoCodes :one
--: tenant-scoped
SELECT count(*) FROM billing_promo_codes
WHERE tenant_id = $1 AND revoked_at IS NULL;

-- name: IncrementBillingPromoCodeUse :exec
--: tenant-scoped; atomically bumps times_used. The unique index on
--: (tenant, code) + the WHERE on this UPDATE guards the redeem race
--: (two concurrent redeems both pass the read check; only one UPDATE
--: finds the row not-yet-bumped past max_uses... actually both could
--: still bump, so the service layer wraps the read + the bump in a
--: tx with SELECT ... FOR UPDATE).
UPDATE billing_promo_codes
SET times_used = times_used + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2
  AND revoked_at IS NULL
  AND (max_uses IS NULL OR times_used < max_uses);

-- name: RevokeBillingPromoCode :exec
--: tenant-scoped; soft-delete. The code stays for audit; redeem refuses.
UPDATE billing_promo_codes
SET revoked_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL;

-- ===========================================================================
-- billing_webhook_events: idempotent Stripe webhook ingestion log.
-- ===========================================================================

-- name: CreateBillingWebhookEvent :one
--: NOT tenant-scoped at insert time — the webhook receiver has no
--: tenant in ctx (the Stripe call is unsigned user-context). The
--: tenant_id is resolved from the event payload + set explicitly.
INSERT INTO billing_webhook_events (
    tenant_id, stripe_event_id, stripe_event_type,
    stripe_api_version, payload, status
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetBillingWebhookEventByStripeID :one
--: NOT tenant-scoped — the idempotency check at receive time happens
--: before the tenant is known. The lookup is by the global Stripe id.
SELECT * FROM billing_webhook_events
WHERE stripe_event_id = $1;

-- name: MarkBillingWebhookEventApplied :exec
--: tenant-scoped; flips status to applied + records the ledger ids.
UPDATE billing_webhook_events
SET
    status            = 'applied',
    ledger_entry_ids  = $3,
    processed_at      = now()
WHERE tenant_id = $1 AND id = $2;

-- name: MarkBillingWebhookEventDuplicate :exec
--: tenant-scoped; the event was already applied; record the dup.
UPDATE billing_webhook_events
SET status = 'duplicate', processed_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: MarkBillingWebhookEventFailed :exec
--: tenant-scoped; records the error so the operator can investigate.
--: The webhook handler returns 5xx so Stripe retries.
UPDATE billing_webhook_events
SET status = 'failed', error_message = $3, processed_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: ListBillingWebhookEvents :many
--: tenant-scoped; admin ops + dashboard. Newest first.
SELECT * FROM billing_webhook_events
WHERE tenant_id = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- name: CountBillingWebhookEvents :one
--: tenant-scoped.
SELECT count(*) FROM billing_webhook_events
WHERE tenant_id = $1;
