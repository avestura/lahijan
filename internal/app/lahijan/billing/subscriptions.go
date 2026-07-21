// Package billing: subscriptions.go is the entrypoint for the
// recurring subscription surface (WS-27, ADR-0034). Includes plan CRUD
// (admin) + subscription create/list/cancel (user) + the metering
// rollup's read of active subscriptions for the overage discount.
package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// ===========================================================================
// Plans.
// ===========================================================================

// IntervalMonthly is the canonical monthly interval string.
const IntervalMonthly = "monthly"

// IntervalYearly is the canonical yearly interval string.
const IntervalYearly = "yearly"

// CreatePlanParams carries the user-controlled fields of a new plan.
type CreatePlanParams struct {
	Slug                   string
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

// CreatePlan creates a billing_plan row scoped to the tenant in ctx.
// The Stripe Product + Price ids are NOT set here; the admin uses
// PushPlanToStripe to create them after the plan exists. Audit +
// event-bus emission.
//
// The caller MUST hold RequirePerm("billing.plan.manage").
func (s *PaymentsService) CreatePlan(
	ctx context.Context,
	tenantID, actorID uuid.UUID,
	params CreatePlanParams,
) (gen.BillingPlan, error) {
	if err := validatePlanSlug(params.Slug); err != nil {
		return gen.BillingPlan{}, err
	}
	if params.Name == "" {
		return gen.BillingPlan{}, ErrInvalidPlanName
	}
	if err := validatePlanInterval(params.Interval); err != nil {
		return gen.BillingPlan{}, err
	}
	if params.PriceCents < 0 {
		return gen.BillingPlan{}, ErrInvalidAmount
	}
	if params.IncludedQuotaCents < 0 {
		return gen.BillingPlan{}, ErrInvalidAmount
	}
	if params.OverageDiscountPercent < 0 || params.OverageDiscountPercent > 100 {
		return gen.BillingPlan{}, ErrInvalidAmount
	}
	currency := strings.ToUpper(params.Currency)
	if currency == "" {
		currency = "USD"
	}
	if err := validateCurrency(currency); err != nil {
		return gen.BillingPlan{}, err
	}

	auditID := s.auditEmit(ctx, audit.ActionBillingPlanCreate, audit.ResourceBillingPlan, tenantID, actorID, uuid.Nil, map[string]any{
		"slug":        params.Slug,
		"name":        params.Name,
		"interval":    params.Interval,
		"price_cents": params.PriceCents,
	})
	row, err := s.repos.BillingPlans.Create(ctx, database.CreateBillingPlanParams{
		Slug:                   params.Slug,
		Name:                   params.Name,
		Description:            params.Description,
		Interval:               params.Interval,
		PriceCents:             params.PriceCents,
		Currency:               currency,
		IncludedQuotaCents:     params.IncludedQuotaCents,
		OverageDiscountPercent: params.OverageDiscountPercent,
		Active:                 params.Active,
		SortOrder:              params.SortOrder,
	})
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.create: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"plan_id": row.ID})
	s.emitEvent(ctx, eventbus.BillingPlanCreated, tenantID, actorID, row.ID, map[string]any{
		"slug":        row.Slug,
		"name":        row.Name,
		"price_cents": row.PriceCents,
		"interval":    row.Interval,
	})
	return row, nil
}

// GetPlan returns the billing_plan row with id within the tenant in
// ctx. Returns ErrPlanNotFound when absent.
func (s *PaymentsService) GetPlan(ctx context.Context, id uuid.UUID) (gen.BillingPlan, error) {
	row, err := s.repos.BillingPlans.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.BillingPlan{}, ErrPlanNotFound
		}
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.get: %w", err)
	}
	return row, nil
}

// GetPlanBySlug returns the billing_plan row with slug within the
// tenant in ctx.
func (s *PaymentsService) GetPlanBySlug(ctx context.Context, slug string) (gen.BillingPlan, error) {
	row, err := s.repos.BillingPlans.GetBySlug(ctx, slug)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.BillingPlan{}, ErrPlanNotFound
		}
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.by_slug: %w", err)
	}
	return row, nil
}

// ListPlans returns a page of billing_plans within the tenant in ctx.
func (s *PaymentsService) ListPlans(ctx context.Context, limit, offset int32) ([]gen.BillingPlan, error) {
	rows, err := s.repos.BillingPlans.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.plans.list: %w", err)
	}
	return rows, nil
}

// ListActivePlans returns a page of active billing_plans within the
// tenant in ctx.
func (s *PaymentsService) ListActivePlans(ctx context.Context, limit, offset int32) ([]gen.BillingPlan, error) {
	rows, err := s.repos.BillingPlans.ListActive(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.plans.list_active: %w", err)
	}
	return rows, nil
}

// CountPlans returns the count of billing_plans within the tenant in
// ctx.
func (s *PaymentsService) CountPlans(ctx context.Context) (int64, error) {
	n, err := s.repos.BillingPlans.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("billing.plans.count: %w", err)
	}
	return n, nil
}

// UpdatePlanParams carries the user-editable fields of an update.
type UpdatePlanParams struct {
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

// UpdatePlan replaces the user-editable fields of a billing_plan row.
// Audit + event emission.
func (s *PaymentsService) UpdatePlan(
	ctx context.Context,
	tenantID, actorID, planID uuid.UUID,
	params UpdatePlanParams,
) error {
	if params.Name == "" {
		return ErrInvalidPlanName
	}
	if err := validatePlanInterval(params.Interval); err != nil {
		return err
	}
	if params.PriceCents < 0 || params.IncludedQuotaCents < 0 {
		return ErrInvalidAmount
	}
	if params.OverageDiscountPercent < 0 || params.OverageDiscountPercent > 100 {
		return ErrInvalidAmount
	}
	currency := strings.ToUpper(params.Currency)
	if currency == "" {
		currency = "USD"
	}
	if err := validateCurrency(currency); err != nil {
		return err
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingPlanUpdate, audit.ResourceBillingPlan, tenantID, actorID, planID, map[string]any{
		"name":        params.Name,
		"price_cents": params.PriceCents,
		"interval":    params.Interval,
		"active":      params.Active,
	})
	if err := s.repos.BillingPlans.Update(ctx, planID, database.UpdateBillingPlanParams{
		Name:                   params.Name,
		Description:            params.Description,
		Interval:               params.Interval,
		PriceCents:             params.PriceCents,
		Currency:               currency,
		IncludedQuotaCents:     params.IncludedQuotaCents,
		OverageDiscountPercent: params.OverageDiscountPercent,
		Active:                 params.Active,
		SortOrder:              params.SortOrder,
	}); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return fmt.Errorf("billing.plans.update: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, nil)
	s.emitEvent(ctx, eventbus.BillingPlanUpdated, tenantID, actorID, planID, map[string]any{
		"name":        params.Name,
		"price_cents": params.PriceCents,
		"interval":    params.Interval,
	})
	return nil
}

// DeletePlan removes a billing_plan row. Plans referenced by an
// existing subscription row cannot be deleted; the service should
// call Update with Active=false instead. Audit emission.
func (s *PaymentsService) DeletePlan(
	ctx context.Context,
	tenantID, actorID, planID uuid.UUID,
) error {
	auditID := s.auditEmit(ctx, audit.ActionBillingPlanDelete, audit.ResourceBillingPlan, tenantID, actorID, planID, nil)
	if err := s.repos.BillingPlans.Delete(ctx, planID); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return fmt.Errorf("billing.plans.delete: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, nil)
	return nil
}

// PushPlanToStripe creates the upstream Stripe Product + Price and
// records the ids on the billing_plan row. Idempotent: re-running with
// the same idempotency key returns the same Product + Price.
func (s *PaymentsService) PushPlanToStripe(
	ctx context.Context,
	tenantID, actorID, planID uuid.UUID,
) (gen.BillingPlan, error) {
	row, err := s.repos.BillingPlans.Get(ctx, planID)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.BillingPlan{}, ErrPlanNotFound
		}
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.push: get: %w", err)
	}
	if row.StripePriceID != nil && *row.StripePriceID != "" {
		// Already pushed; idempotent return.
		return row, nil
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingPlanUpdate, audit.ResourceBillingPlan, tenantID, actorID, planID, map[string]any{
		"op": "push_to_stripe",
	})
	idempProd := "prod:" + tenantID.String() + ":" + row.Slug
	prod, err := s.gw.CreateProduct(ctx, stripe.CreateProductRequest{
		Name:        row.Name,
		Description: row.Description,
		Metadata: map[string]any{
			"tenant_id": tenantID.String(),
			"plan_id":   row.ID.String(),
			"slug":      row.Slug,
		},
	}, idempProd)
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.push: create product: %w", err)
	}
	idempPrice := "price:" + tenantID.String() + ":" + row.Slug
	price, err := s.gw.CreatePrice(ctx, stripe.CreatePriceRequest{
		Currency:      strings.ToLower(row.Currency),
		UnitAmount:    row.PriceCents,
		Product:       prod.ID,
		Interval:      planIntervalToStripe(row.Interval),
		IntervalCount: 1,
		Metadata: map[string]any{
			"tenant_id": tenantID.String(),
			"plan_id":   row.ID.String(),
			"slug":      row.Slug,
		},
	}, idempPrice)
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.push: create price: %w", err)
	}
	prodID := prod.ID
	priceID := price.ID
	if err := s.repos.BillingPlans.SetStripeIDs(ctx, planID, &prodID, &priceID); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingPlan{}, fmt.Errorf("billing.plans.push: set ids: %w", err)
	}
	row.StripeProductID = &prodID
	row.StripePriceID = &priceID
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{
		"stripe_product_id": prodID,
		"stripe_price_id":   priceID,
	})
	return row, nil
}

// validatePlanSlug rejects empty + whitespace + non-printable slugs.
func validatePlanSlug(slug string) error {
	if slug == "" {
		return ErrInvalidPlanName
	}
	for _, r := range slug {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' || r == '_' {
			continue
		}
		return ErrInvalidPlanName
	}
	return nil
}

// validatePlanInterval asserts one of the canonical intervals.
func validatePlanInterval(interval string) error {
	switch interval {
	case IntervalMonthly, IntervalYearly:
		return nil
	}
	return ErrInvalidPlanInterval
}

// planIntervalToStripe maps Lahijan's interval string to Stripe's.
func planIntervalToStripe(interval string) string {
	switch interval {
	case IntervalYearly:
		return "year"
	}
	return "month"
}

// ===========================================================================
// Subscriptions.
// ===========================================================================

// CreateSubscription creates a Stripe Subscription for the user +
// caches the local row. The webhook handler posts the included-quota
// credit when `invoice.paid` lands.
func (s *PaymentsService) CreateSubscription(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	email, displayName string,
	planID uuid.UUID,
	paymentMethodRowID *uuid.UUID,
) (gen.BillingSubscription, error) {
	plan, err := s.repos.BillingPlans.Get(ctx, planID)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.BillingSubscription{}, ErrPlanNotFound
		}
		return gen.BillingSubscription{}, fmt.Errorf("billing.subscriptions.create: get plan: %w", err)
	}
	if !plan.Active {
		return gen.BillingSubscription{}, ErrPlanNotFound
	}
	if plan.StripePriceID == nil || *plan.StripePriceID == "" {
		return gen.BillingSubscription{}, ErrPlanNotPushed
	}
	customerID, err := s.getOrCreateStripeCustomer(ctx, tenantID, userID, email, displayName)
	if err != nil {
		return gen.BillingSubscription{}, err
	}
	// Optional default payment method.
	var defaultPM string
	if paymentMethodRowID != nil {
		pm, errGet := s.repos.BillingPaymentMethods.Get(ctx, *paymentMethodRowID)
		if errGet == nil && pm.UserID == userID && pm.Active {
			defaultPM = pm.StripePaymentMethodID
		}
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingSubscriptionCreate, audit.ResourceBillingSubscription, tenantID, userID, uuid.Nil, map[string]any{
		"plan_id":     plan.ID,
		"plan_slug":   plan.Slug,
		"price_cents": plan.PriceCents,
	})
	idemp := "sub:" + tenantID.String() + ":" + userID.String() + ":" + plan.ID.String()
	sub, err := s.gw.CreateSubscription(ctx, stripe.CreateSubscriptionRequest{
		Customer:             customerID,
		Price:                *plan.StripePriceID,
		DefaultPaymentMethod: defaultPM,
		Metadata: map[string]any{
			"tenant_id": tenantID.String(),
			"user_id":   userID.String(),
			"plan_id":   plan.ID.String(),
		},
	}, idemp)
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingSubscription{}, fmt.Errorf("billing.subscriptions.create: gateway: %w", err)
	}
	periodEnd := time.Unix(sub.CurrentPeriodEnd, 0)
	row, err := s.repos.BillingSubscriptions.Create(ctx, database.CreateBillingSubscriptionParams{
		UserID:                 userID,
		PlanID:                 plan.ID,
		StripeSubscriptionID:   sub.ID,
		Interval:               plan.Interval,
		PriceCents:             plan.PriceCents,
		Currency:               plan.Currency,
		IncludedQuotaCents:     plan.IncludedQuotaCents,
		OverageDiscountPercent: plan.OverageDiscountPercent,
		Status:                 "active",
		CurrentPeriodEnd:       &periodEnd,
		Metadata: map[string]any{
			"stripe_subscription_id": sub.ID,
		},
	})
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingSubscription{}, fmt.Errorf("billing.subscriptions.create: cache: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"subscription_id": row.ID})
	s.emitEvent(ctx, eventbus.BillingSubscriptionActivated, tenantID, userID, row.ID, map[string]any{
		"plan_slug":            plan.Slug,
		"price_cents":          plan.PriceCents,
		"included_quota_cents": plan.IncludedQuotaCents,
		"interval":             plan.Interval,
	})
	return row, nil
}

// ListSubscriptions returns the user's active + canceled subscriptions
// (expired ones drop off the dashboard after a configurable retention
// period).
func (s *PaymentsService) ListSubscriptions(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.BillingSubscription, error) {
	rows, err := s.repos.BillingSubscriptions.ListForUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.subscriptions.list: %w", err)
	}
	return rows, nil
}

// CountSubscriptions returns the count of the user's active + canceled
// subscriptions.
func (s *PaymentsService) CountSubscriptions(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := s.repos.BillingSubscriptions.CountForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("billing.subscriptions.count: %w", err)
	}
	return n, nil
}

// CancelSubscription cancels the user's subscription. cancelAtPeriodEnd
// true keeps it active until the current period ends; false cancels
// immediately. Audit + event emission.
func (s *PaymentsService) CancelSubscription(
	ctx context.Context,
	tenantID, userID, subID uuid.UUID,
	cancelAtPeriodEnd bool,
) error {
	row, err := s.repos.BillingSubscriptions.Get(ctx, subID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrSubscriptionNotFound
		}
		return fmt.Errorf("billing.subscriptions.cancel: get: %w", err)
	}
	if row.UserID != userID {
		return ErrSubscriptionNotFound
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingSubscriptionCancel, audit.ResourceBillingSubscription, tenantID, userID, row.ID, map[string]any{
		"cancel_at_period_end": cancelAtPeriodEnd,
		"plan_id":              row.PlanID,
	})
	if _, err := s.gw.CancelSubscription(ctx, row.StripeSubscriptionID, cancelAtPeriodEnd); err != nil {
		// NotFound upstream means the user already canceled via Stripe;
		// fall through to the local flip.
		if !errors.Is(err, stripe.ErrNotFound) {
			s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
			return fmt.Errorf("billing.subscriptions.cancel: gateway: %w", err)
		}
	}
	status := "canceled"
	var canceledAt *time.Time
	if !cancelAtPeriodEnd {
		now := time.Now().UTC()
		canceledAt = &now
		status = "expired"
	}
	if err := s.repos.BillingSubscriptions.SetStatus(ctx, subID, status, row.CurrentPeriodEnd, canceledAt); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return fmt.Errorf("billing.subscriptions.cancel: cache: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, nil)
	s.emitEvent(ctx, eventbus.BillingSubscriptionCanceled, tenantID, userID, row.ID, map[string]any{
		"cancel_at_period_end": cancelAtPeriodEnd,
	})
	return nil
}

// ErrInvalidPlanName is returned when a plan slug or name is empty or
// contains characters outside the safe set (lowercase ASCII letters,
// digits, dash, underscore).
var ErrInvalidPlanName = errors.New("billing: plan slug or name is invalid")

// ErrInvalidPlanInterval is returned when the supplied plan interval
// is not one of the canonical values (monthly, yearly).
var ErrInvalidPlanInterval = errors.New("billing: plan interval must be monthly or yearly")

// ErrPlanNotPushed is returned when a subscription is attempted
// against a plan that has not been pushed to Stripe yet.
var ErrPlanNotPushed = errors.New("billing: plan has not been pushed to Stripe")
