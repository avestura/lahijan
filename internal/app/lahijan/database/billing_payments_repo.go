// Package database: billing_payments_repo.go wraps the sqlc-generated
// WS-27 billing queries (plans, payment_methods, subscriptions,
// promo_codes, webhook_events). Every query is tenant-scoped via
// WithTenant at the repository seam EXCEPT the webhook event lookup at
// receive time (the idempotency check happens before the tenant id is
// resolved from the payload).
//
// The five sub-repositories follow the same shape as the WS-17 ones
// (billing_repo.go): the service never constructs a *gen.Queries
// directly, every method takes a context that carries the tenant id,
// and the stripe-side identifiers (stripe_event_id, etc.) are passed
// through verbatim.
package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ===========================================================================
// Plans: admin-managed subscription catalog.
// ===========================================================================

// BillingPlansRepository is the persistence boundary for billing_plans.
type BillingPlansRepository struct {
	q *gen.Queries
}

// NewBillingPlansRepository wraps the given sqlc queries.
func NewBillingPlansRepository(q *gen.Queries) *BillingPlansRepository {
	return &BillingPlansRepository{q: q}
}

// CreateBillingPlanParams carries the user-controlled fields of a new
// billing_plans row. TenantID is taken from the request context, NOT
// from the caller.
type CreateBillingPlanParams struct {
	Slug                   string
	Name                   string
	Description            string
	Interval               string
	PriceCents             int64
	Currency               string
	IncludedQuotaCents     int64
	OverageDiscountPercent int32
	StripeProductID        *string
	StripePriceID          *string
	Active                 bool
	SortOrder              int32
}

// Create inserts a new billing_plans row scoped to the tenant in ctx.
func (r *BillingPlansRepository) Create(
	ctx context.Context,
	arg CreateBillingPlanParams,
) (gen.BillingPlan, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPlan{}, err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	return r.q.CreateBillingPlan(ctx, gen.CreateBillingPlanParams{
		TenantID:               tenantID,
		Slug:                   arg.Slug,
		Name:                   arg.Name,
		Description:            arg.Description,
		Interval:               arg.Interval,
		PriceCents:             arg.PriceCents,
		Currency:               currency,
		IncludedQuotaCents:     arg.IncludedQuotaCents,
		OverageDiscountPercent: arg.OverageDiscountPercent,
		StripeProductID:        arg.StripeProductID,
		StripePriceID:          arg.StripePriceID,
		Active:                 arg.Active,
		SortOrder:              arg.SortOrder,
	})
}

// Get returns the billing_plans row with id within the tenant in ctx.
func (r *BillingPlansRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.BillingPlan, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPlan{}, err
	}
	return r.q.GetBillingPlanByID(ctx, gen.GetBillingPlanByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetBySlug returns the billing_plans row with slug within the tenant in
// ctx.
func (r *BillingPlansRepository) GetBySlug(
	ctx context.Context,
	slug string,
) (gen.BillingPlan, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPlan{}, err
	}
	return r.q.GetBillingPlanBySlug(ctx, gen.GetBillingPlanBySlugParams{
		TenantID: tenantID, Slug: slug,
	})
}

// List returns a page of billing_plans within the tenant in ctx.
// Ordered by sort_order then name so the pricing page + dashboard render
// a stable list.
func (r *BillingPlansRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.BillingPlan, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListBillingPlans(ctx, gen.ListBillingPlansParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// ListActive returns a page of active billing_plans within the tenant
// in ctx. Used by the public pricing page.
func (r *BillingPlansRepository) ListActive(
	ctx context.Context,
	limit, offset int32,
) ([]gen.BillingPlan, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListActiveBillingPlans(ctx, gen.ListActiveBillingPlansParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of billing_plans rows within the tenant in
// ctx.
func (r *BillingPlansRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountBillingPlans(ctx, tenantID)
}

// CountActive returns the number of active billing_plans rows within
// the tenant in ctx.
func (r *BillingPlansRepository) CountActive(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountActiveBillingPlans(ctx, tenantID)
}

// UpdateBillingPlanParams carries the user-editable fields for an
// update. The Stripe ids columns are updated separately via
// SetStripeIDs so a config edit does not require re-pushing the plan
// to Stripe.
type UpdateBillingPlanParams struct {
	Name                   string
	Description            string
	Interval               string
	PriceCents             int64
	Currency               string
	IncludedQuotaCents     int64
	OverageDiscountPercent int32
	Active                 bool
	SortOrder              int32
}

// Update replaces the user-editable fields of a billing_plans row.
func (r *BillingPlansRepository) Update(
	ctx context.Context,
	id uuid.UUID,
	arg UpdateBillingPlanParams,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	return r.q.UpdateBillingPlan(ctx, gen.UpdateBillingPlanParams{
		TenantID:               tenantID,
		ID:                     id,
		Name:                   arg.Name,
		Description:            arg.Description,
		Interval:               arg.Interval,
		PriceCents:             arg.PriceCents,
		Currency:               currency,
		IncludedQuotaCents:     arg.IncludedQuotaCents,
		OverageDiscountPercent: arg.OverageDiscountPercent,
		Active:                 arg.Active,
		SortOrder:              arg.SortOrder,
	})
}

// SetStripeIDs records the Stripe Product + Price ids after the admin
// pushes the plan to Stripe.
func (r *BillingPlansRepository) SetStripeIDs(
	ctx context.Context,
	id uuid.UUID,
	stripeProductID, stripePriceID *string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetBillingPlanStripeIDs(ctx, gen.SetBillingPlanStripeIDsParams{
		TenantID:        tenantID,
		ID:              id,
		StripeProductID: stripeProductID,
		StripePriceID:   stripePriceID,
	})
}

// Delete removes a billing_plans row. Plans referenced by an existing
// subscription row cannot be deleted (FK ON DELETE NO ACTION); the
// service layer should call Update with Active=false instead.
func (r *BillingPlansRepository) Delete(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteBillingPlan(ctx, gen.DeleteBillingPlanParams{
		TenantID: tenantID, ID: id,
	})
}

// ===========================================================================
// Payment methods: per-user Stripe PaymentMethod cache.
// ===========================================================================

// BillingPaymentMethodsRepository is the persistence boundary for
// billing_payment_methods.
type BillingPaymentMethodsRepository struct {
	q *gen.Queries
}

// NewBillingPaymentMethodsRepository wraps the given sqlc queries.
func NewBillingPaymentMethodsRepository(q *gen.Queries) *BillingPaymentMethodsRepository {
	return &BillingPaymentMethodsRepository{q: q}
}

// CreateBillingPaymentMethodParams carries the user-controlled fields
// of a new billing_payment_methods row.
type CreateBillingPaymentMethodParams struct {
	UserID                    uuid.UUID
	StripePaymentMethodID     string
	EncryptedStripeCustomerID string
	Brand                     string
	Last4                     string
	Fingerprint               string
	ExpMonth                  *int32
	ExpYear                   *int32
	IsDefault                 bool
	Active                    bool
	Metadata                  map[string]any
}

// Create inserts a new billing_payment_methods row scoped to the tenant
// in ctx.
func (r *BillingPaymentMethodsRepository) Create(
	ctx context.Context,
	arg CreateBillingPaymentMethodParams,
) (gen.BillingPaymentMethod, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPaymentMethod{}, err
	}
	meta := json.RawMessage("{}")
	if arg.Metadata != nil {
		raw, errM := json.Marshal(arg.Metadata)
		if errM != nil {
			return gen.BillingPaymentMethod{}, fmt.Errorf("billing.payment_methods: marshal metadata: %w", errM)
		}
		meta = raw
	}
	return r.q.CreateBillingPaymentMethod(ctx, gen.CreateBillingPaymentMethodParams{
		TenantID:                  tenantID,
		UserID:                    arg.UserID,
		StripePaymentMethodID:     arg.StripePaymentMethodID,
		EncryptedStripeCustomerID: arg.EncryptedStripeCustomerID,
		Brand:                     arg.Brand,
		Last4:                     arg.Last4,
		Fingerprint:               arg.Fingerprint,
		ExpMonth:                  arg.ExpMonth,
		ExpYear:                   arg.ExpYear,
		IsDefault:                 arg.IsDefault,
		Active:                    arg.Active,
		Metadata:                  meta,
	})
}

// Get returns the billing_payment_methods row with id within the tenant
// in ctx.
func (r *BillingPaymentMethodsRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.BillingPaymentMethod, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPaymentMethod{}, err
	}
	return r.q.GetBillingPaymentMethodByID(ctx, gen.GetBillingPaymentMethodByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByStripeID returns the billing_payment_methods row with the given
// Stripe PaymentMethod id within the tenant in ctx.
func (r *BillingPaymentMethodsRepository) GetByStripeID(
	ctx context.Context,
	stripePaymentMethodID string,
) (gen.BillingPaymentMethod, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPaymentMethod{}, err
	}
	return r.q.GetBillingPaymentMethodByStripeID(ctx, gen.GetBillingPaymentMethodByStripeIDParams{
		TenantID: tenantID, StripePaymentMethodID: stripePaymentMethodID,
	})
}

// ListForUser returns a page of active billing_payment_methods rows
// for the given user within the tenant in ctx. Ordered by is_default
// then created_at so the default card appears first.
func (r *BillingPaymentMethodsRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.BillingPaymentMethod, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListBillingPaymentMethodsForUser(ctx, gen.ListBillingPaymentMethodsForUserParams{
		TenantID: tenantID, UserID: userID, Limit: limit, Offset: offset,
	})
}

// CountForUser returns the count of active billing_payment_methods
// rows for the given user within the tenant in ctx.
func (r *BillingPaymentMethodsRepository) CountForUser(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountBillingPaymentMethodsForUser(ctx, gen.CountBillingPaymentMethodsForUserParams{
		TenantID: tenantID, UserID: userID,
	})
}

// ListByFingerprint returns the active billing_payment_methods rows
// for the user with the given fingerprint. Used by the dedup check
// before attaching a new card.
func (r *BillingPaymentMethodsRepository) ListByFingerprint(
	ctx context.Context,
	userID uuid.UUID,
	fingerprint string,
) ([]gen.BillingPaymentMethod, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListBillingPaymentMethodsByFingerprint(ctx, gen.ListBillingPaymentMethodsByFingerprintParams{
		TenantID: tenantID, UserID: userID, Fingerprint: fingerprint,
	})
}

// SetDefault clears any prior default for the user + sets the new
// default. Two statements inside a tx; the caller is responsible for
// wrapping in a tx via Repos.WithTx.
func (r *BillingPaymentMethodsRepository) SetDefault(
	ctx context.Context,
	userID,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if err := r.q.SetDefaultBillingPaymentMethod(ctx, gen.SetDefaultBillingPaymentMethodParams{
		TenantID: tenantID, UserID: userID,
	}); err != nil {
		return fmt.Errorf("billing.payment_methods.set_default: clear: %w", err)
	}
	return r.q.MarkDefaultBillingPaymentMethod(ctx, gen.MarkDefaultBillingPaymentMethodParams{
		TenantID: tenantID, ID: id,
	})
}

// Deactivate soft-deletes a billing_payment_methods row. The row stays
// for audit history; the Stripe PaymentMethod itself is detached at
// the gateway layer.
func (r *BillingPaymentMethodsRepository) Deactivate(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeactivateBillingPaymentMethod(ctx, gen.DeactivateBillingPaymentMethodParams{
		TenantID: tenantID, ID: id,
	})
}

// ===========================================================================
// Subscriptions: per-user recurring subscriptions.
// ===========================================================================

// BillingSubscriptionsRepository is the persistence boundary for
// billing_subscriptions.
type BillingSubscriptionsRepository struct {
	q *gen.Queries
}

// NewBillingSubscriptionsRepository wraps the given sqlc queries.
func NewBillingSubscriptionsRepository(q *gen.Queries) *BillingSubscriptionsRepository {
	return &BillingSubscriptionsRepository{q: q}
}

// CreateBillingSubscriptionParams carries the user-controlled fields
// of a new billing_subscriptions row.
type CreateBillingSubscriptionParams struct {
	UserID                 uuid.UUID
	PlanID                 uuid.UUID
	StripeSubscriptionID   string
	Interval               string
	PriceCents             int64
	Currency               string
	IncludedQuotaCents     int64
	OverageDiscountPercent int32
	Status                 string
	CurrentPeriodEnd       *time.Time
	Metadata               map[string]any
}

// Create inserts a new billing_subscriptions row scoped to the tenant
// in ctx.
func (r *BillingSubscriptionsRepository) Create(
	ctx context.Context,
	arg CreateBillingSubscriptionParams,
) (gen.BillingSubscription, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingSubscription{}, err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	meta := json.RawMessage("{}")
	if arg.Metadata != nil {
		raw, errM := json.Marshal(arg.Metadata)
		if errM != nil {
			return gen.BillingSubscription{}, fmt.Errorf("billing.subscriptions: marshal metadata: %w", errM)
		}
		meta = raw
	}
	status := arg.Status
	if status == "" {
		status = "active"
	}
	return r.q.CreateBillingSubscription(ctx, gen.CreateBillingSubscriptionParams{
		TenantID:               tenantID,
		UserID:                 arg.UserID,
		PlanID:                 arg.PlanID,
		StripeSubscriptionID:   arg.StripeSubscriptionID,
		Interval:               arg.Interval,
		PriceCents:             arg.PriceCents,
		Currency:               currency,
		IncludedQuotaCents:     arg.IncludedQuotaCents,
		OverageDiscountPercent: arg.OverageDiscountPercent,
		Status:                 status,
		CurrentPeriodEnd:       arg.CurrentPeriodEnd,
		Metadata:               meta,
	})
}

// Get returns the billing_subscriptions row with id within the tenant
// in ctx.
func (r *BillingSubscriptionsRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.BillingSubscription, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingSubscription{}, err
	}
	return r.q.GetBillingSubscriptionByID(ctx, gen.GetBillingSubscriptionByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByStripeID returns the billing_subscriptions row with the given
// Stripe Subscription id within the tenant in ctx. Used by the webhook
// handler to reconcile.
func (r *BillingSubscriptionsRepository) GetByStripeID(
	ctx context.Context,
	stripeSubscriptionID string,
) (gen.BillingSubscription, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingSubscription{}, err
	}
	return r.q.GetBillingSubscriptionByStripeID(ctx, gen.GetBillingSubscriptionByStripeIDParams{
		TenantID:             tenantID,
		StripeSubscriptionID: stripeSubscriptionID,
	})
}

// ListForUser returns a page of billing_subscriptions for the given
// user within the tenant in ctx. Active + canceled only (expired ones
// drop off the dashboard after a configurable retention period).
func (r *BillingSubscriptionsRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.BillingSubscription, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListBillingSubscriptionsForUser(ctx, gen.ListBillingSubscriptionsForUserParams{
		TenantID: tenantID, UserID: userID, Limit: limit, Offset: offset,
	})
}

// CountForUser returns the count of billing_subscriptions for the
// given user within the tenant in ctx (active + canceled only).
func (r *BillingSubscriptionsRepository) CountForUser(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountBillingSubscriptionsForUser(ctx, gen.CountBillingSubscriptionsForUserParams{
		TenantID: tenantID, UserID: userID,
	})
}

// ListActive returns every active billing_subscriptions row in the
// tenant in ctx. Used by the metering rollup to find users with an
// overage discount.
func (r *BillingSubscriptionsRepository) ListActive(
	ctx context.Context,
) ([]gen.BillingSubscription, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListActiveBillingSubscriptions(ctx, tenantID)
}

// SetStatus flips a billing_subscriptions row's status + the
// accompanying current_period_end / canceled_at timestamps. Used by
// the webhook handler.
func (r *BillingSubscriptionsRepository) SetStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
	currentPeriodEnd, canceledAt *time.Time,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetBillingSubscriptionStatus(ctx, gen.SetBillingSubscriptionStatusParams{
		TenantID:         tenantID,
		ID:               id,
		Status:           status,
		CurrentPeriodEnd: currentPeriodEnd,
		CanceledAt:       canceledAt,
	})
}

// ===========================================================================
// Promo codes: admin-issued prepaid / promo codes.
// ===========================================================================

// BillingPromoCodesRepository is the persistence boundary for
// billing_promo_codes.
type BillingPromoCodesRepository struct {
	q *gen.Queries
}

// NewBillingPromoCodesRepository wraps the given sqlc queries.
func NewBillingPromoCodesRepository(q *gen.Queries) *BillingPromoCodesRepository {
	return &BillingPromoCodesRepository{q: q}
}

// CreateBillingPromoCodeParams carries the user-controlled fields of a
// new billing_promo_codes row.
type CreateBillingPromoCodeParams struct {
	Code            string
	Note            string
	CreditCents     int64
	Currency        string
	AppliesToPlanID *uuid.UUID
	MaxUses         *int32
	ExpiresAt       *time.Time
	CreatedBy       uuid.UUID
}

// Create inserts a new billing_promo_codes row scoped to the tenant in
// ctx.
func (r *BillingPromoCodesRepository) Create(
	ctx context.Context,
	arg CreateBillingPromoCodeParams,
) (gen.BillingPromoCode, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPromoCode{}, err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	return r.q.CreateBillingPromoCode(ctx, gen.CreateBillingPromoCodeParams{
		TenantID:        tenantID,
		Code:            arg.Code,
		Note:            arg.Note,
		CreditCents:     arg.CreditCents,
		Currency:        currency,
		AppliesToPlanID: arg.AppliesToPlanID,
		MaxUses:         arg.MaxUses,
		ExpiresAt:       arg.ExpiresAt,
		CreatedBy:       arg.CreatedBy,
	})
}

// Get returns the billing_promo_codes row with id within the tenant in
// ctx.
func (r *BillingPromoCodesRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.BillingPromoCode, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPromoCode{}, err
	}
	return r.q.GetBillingPromoCodeByID(ctx, gen.GetBillingPromoCodeByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByCode returns the billing_promo_codes row with code within the
// tenant in ctx. Case-sensitive on the code (admin normalises to
// uppercase at create time).
func (r *BillingPromoCodesRepository) GetByCode(
	ctx context.Context,
	code string,
) (gen.BillingPromoCode, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.BillingPromoCode{}, err
	}
	return r.q.GetBillingPromoCodeByCode(ctx, gen.GetBillingPromoCodeByCodeParams{
		TenantID: tenantID, Code: code,
	})
}

// List returns a page of non-revoked billing_promo_codes within the
// tenant in ctx. Ordered by created_at desc so the admin dashboard
// shows recent codes first.
func (r *BillingPromoCodesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.BillingPromoCode, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListBillingPromoCodes(ctx, gen.ListBillingPromoCodesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-revoked billing_promo_codes within
// the tenant in ctx.
func (r *BillingPromoCodesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountBillingPromoCodes(ctx, tenantID)
}

// ErrPromoCodeExhausted is returned by IncrementUse when the code has
// reached max_uses (or is revoked / expired). The service layer maps
// this to a 409 conflict.
var ErrPromoCodeExhausted = errors.New("billing: promo code is exhausted, revoked, or expired")

// IncrementUse atomically bumps times_used. Returns
// ErrPromoCodeExhausted when the row is revoked, expired, or already
// at max_uses. The unique index on (tenant, code) + this conditional
// UPDATE guard the redeem race; the service layer wraps the read +
// the bump in a tx for the single-use case.
func (r *BillingPromoCodesRepository) IncrementUse(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	// sqlc exec queries don't return RowsAffected; we re-query to
	// detect the no-op case. The redeem path wraps both calls in a tx
	// with SELECT FOR UPDATE so a concurrent redeem cannot interleave.
	if errInc := r.q.IncrementBillingPromoCodeUse(ctx, gen.IncrementBillingPromoCodeUseParams{
		TenantID: tenantID, ID: id,
	}); errInc != nil {
		return fmt.Errorf("billing.promo_codes.increment_use: %w", errInc)
	}
	after, err := r.q.GetBillingPromoCodeByID(ctx, gen.GetBillingPromoCodeByIDParams{
		TenantID: tenantID, ID: id,
	})
	if err != nil {
		return fmt.Errorf("billing.promo_codes.increment_use: re-read: %w", err)
	}
	if after.TimesUsed == 0 {
		return ErrPromoCodeExhausted
	}
	return nil
}

// Revoke soft-deletes a billing_promo_codes row. The code stays for
// audit; redeem refuses.
func (r *BillingPromoCodesRepository) Revoke(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.RevokeBillingPromoCode(ctx, gen.RevokeBillingPromoCodeParams{
		TenantID: tenantID, ID: id,
	})
}

// ===========================================================================
// Webhook events: idempotent Stripe webhook ingestion log.
// ===========================================================================

// BillingWebhookEventsRepository is the persistence boundary for
// billing_webhook_events.
type BillingWebhookEventsRepository struct {
	q *gen.Queries
}

// NewBillingWebhookEventsRepository wraps the given sqlc queries.
func NewBillingWebhookEventsRepository(q *gen.Queries) *BillingWebhookEventsRepository {
	return &BillingWebhookEventsRepository{q: q}
}

// CreateBillingWebhookEventParams carries the fields of a new
// billing_webhook_events row.
type CreateBillingWebhookEventParams struct {
	TenantID         *uuid.UUID
	StripeEventID    string
	StripeEventType  string
	StripeAPIVersion *string
	Payload          json.RawMessage
	Status           string
}

// Create inserts a new billing_webhook_events row. The UNIQUE on
// stripe_event_id is the idempotency boundary: a duplicate Stripe
// delivery hits this constraint and the caller maps the error to
// "duplicate".
func (r *BillingWebhookEventsRepository) Create(
	ctx context.Context,
	arg CreateBillingWebhookEventParams,
) (gen.BillingWebhookEvent, error) {
	status := arg.Status
	if status == "" {
		status = "received"
	}
	payload := arg.Payload
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	return r.q.CreateBillingWebhookEvent(ctx, gen.CreateBillingWebhookEventParams{
		TenantID:         arg.TenantID,
		StripeEventID:    arg.StripeEventID,
		StripeEventType:  arg.StripeEventType,
		StripeApiVersion: arg.StripeAPIVersion,
		Payload:          payload,
		Status:           status,
	})
}

// GetByStripeID returns the billing_webhook_events row with the given
// Stripe event id. NOT tenant-scoped — the idempotency check at
// receive time happens before the tenant is known. Returns
// pgx.ErrNoRows if no such row exists.
func (r *BillingWebhookEventsRepository) GetByStripeID(
	ctx context.Context,
	stripeEventID string,
) (gen.BillingWebhookEvent, error) {
	return r.q.GetBillingWebhookEventByStripeID(ctx, stripeEventID)
}

// MarkApplied flips status to applied + records the ledger ids.
func (r *BillingWebhookEventsRepository) MarkApplied(
	ctx context.Context,
	id uuid.UUID,
	ledgerEntryIDs []uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if ledgerEntryIDs == nil {
		ledgerEntryIDs = []uuid.UUID{}
	}
	tid := tenantID
	return r.q.MarkBillingWebhookEventApplied(ctx, gen.MarkBillingWebhookEventAppliedParams{
		TenantID:       &tid,
		ID:             id,
		LedgerEntryIds: ledgerEntryIDs,
	})
}

// MarkDuplicate records that the event was already applied.
func (r *BillingWebhookEventsRepository) MarkDuplicate(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tid := tenantID
	return r.q.MarkBillingWebhookEventDuplicate(ctx, gen.MarkBillingWebhookEventDuplicateParams{
		TenantID: &tid, ID: id,
	})
}

// MarkFailed records the error so the operator can investigate. The
// webhook handler returns 5xx so Stripe retries.
func (r *BillingWebhookEventsRepository) MarkFailed(
	ctx context.Context,
	id uuid.UUID,
	errMsg string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tid := tenantID
	return r.q.MarkBillingWebhookEventFailed(ctx, gen.MarkBillingWebhookEventFailedParams{
		TenantID:     &tid,
		ID:           id,
		ErrorMessage: &errMsg,
	})
}

// List returns a page of billing_webhook_events within the tenant in
// ctx. Newest first.
func (r *BillingWebhookEventsRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.BillingWebhookEvent, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListBillingWebhookEvents(ctx, gen.ListBillingWebhookEventsParams{
		TenantID: &tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of billing_webhook_events within the tenant
// in ctx.
func (r *BillingWebhookEventsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountBillingWebhookEvents(ctx, &tenantID)
}
