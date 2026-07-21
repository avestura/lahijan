// Package api: billing_payments_handlers.go implements the
// OpenAPI-derived WS-27 payment gateway endpoints. Each handler is
// thin: parse request, resolve IDs from the request scope, call the
// payments service, render the response.
//
// The user-tree (/api/v1/billing/*) endpoints require a session but
// gate per-route via RequirePerm in router.go (AuditGate). The
// admin-tree (/api/v1/admin/billing/{plans,promo-codes,webhook-events}/*)
// endpoints require admin perms. The webhook receiver
// (/api/v1/webhooks/stripe) is UNAUTHENTICATED at the rbac layer —
// signature verification is the auth.
//
// Error mapping follows the WS-27 DoD:
//
//   - 400 bad_request        -> malformed body / params
//   - 401 unauthorized       -> no session
//   - 403 forbidden          -> missing permission
//   - 404 not_found          -> resource missing
//   - 409 conflict           -> promo code exhausted / plan in use
//   - 501 not_implemented    -> Stripe gateway disabled
package api

import (
	"errors"
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var _ = database.ErrNoTenantInContext

// paymentsDisabledMsg is the localised "feature disabled" message the
// handlers return when the Stripe gateway is not wired.
func paymentsDisabledMsg(c *fiber.Ctx) string {
	return i18n.T(c.UserContext(), "billing.err_payments_disabled", nil)
}

// mapPaymentsError translates a payments service error to the right
// envelope. Mirrors mapBillingError but for the WS-27 surface.
func mapPaymentsError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, billing.ErrPaymentMethodNotFound),
		errors.Is(err, billing.ErrPlanNotFound),
		errors.Is(err, billing.ErrSubscriptionNotFound),
		errors.Is(err, billing.ErrPromoCodeNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "billing.err_not_found", nil))
	case errors.Is(err, billing.ErrInvalidAmount),
		errors.Is(err, billing.ErrInvalidPaymentMethod),
		errors.Is(err, billing.ErrInvalidPlanName),
		errors.Is(err, billing.ErrInvalidPlanInterval),
		errors.Is(err, billing.ErrInvalidPromoCode),
		errors.Is(err, billing.ErrWebhookSignatureInvalid):
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_bad_request", nil), nil)
	case errors.Is(err, billing.ErrPromoCodeExhausted),
		errors.Is(err, billing.ErrPlanNotPushed),
		errors.Is(err, billing.ErrDuplicateIdempotencyKey):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "billing.err_conflict", nil), nil)
	case errors.Is(err, billing.ErrInvalidCurrency):
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_currency", nil), nil)
	}
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// userMeta resolves the caller's email + display name for the
// Stripe Customer create path. The auth middleware sets these in
// Locals; when missing we fall back to a placeholder so the gateway
// create still works.
func (s *Server) userMeta(c *fiber.Ctx) (string, string) {
	email, _ := c.Locals("user_email").(string)
	name, _ := c.Locals("user_display_name").(string)
	if email == "" {
		email = "user@example.test"
	}
	if name == "" {
		name = "User " + uuid.NewString()[:8]
	}
	return email, name
}

// ---------------------------------------------------------------------------
// User-facing: /api/v1/billing/*
// ---------------------------------------------------------------------------

// GetBillingConfig handles GET /api/v1/billing/config.
func (s *Server) GetBillingConfig(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return c.JSON(apigen.BillingConfig{Enabled: false})
	}
	cfg := s.paymentsSvc.Config()
	gw := s.paymentsSvc.Gateway()
	live := false
	type livemodeProvider interface{ LiveMode() bool }
	if lp, ok := gw.(livemodeProvider); ok {
		live = lp.LiveMode()
	}
	return c.JSON(apigen.BillingConfig{
		Enabled:        true,
		PublishableKey: &cfg.PublishableKey,
		LiveMode:       &live,
	})
}

// ListMyPaymentMethods handles GET /api/v1/billing/payment-methods.
func (s *Server) ListMyPaymentMethods(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	_, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	rows, err := s.paymentsSvc.ListPaymentMethods(c.UserContext(), uid, MaxBillingPageSize, 0)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	items := make([]apigen.BillingPaymentMethod, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingPaymentMethodDTO(row))
	}
	return c.JSON(apigen.BillingPaymentMethodPage{
		Items: items, Total: len(items), Limit: int(MaxBillingPageSize), Offset: 0,
	})
}

// AddMyPaymentMethod handles POST /api/v1/billing/payment-methods.
func (s *Server) AddMyPaymentMethod(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingPaymentMethodAttachRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.StripePaymentMethodId == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	email, name := s.userMeta(c)
	setDefault := false
	if req.SetDefault != nil {
		setDefault = *req.SetDefault
	}
	row, err := s.paymentsSvc.AddPaymentMethod(c.UserContext(), tid, uid, email, name, req.StripePaymentMethodId, setDefault)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingPaymentMethodDTO(row))
}

// RemoveMyPaymentMethod handles DELETE /api/v1/billing/payment-methods/{paymentMethodId}.
func (s *Server) RemoveMyPaymentMethod(c *fiber.Ctx, paymentMethodID openapi_types.UUID) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.paymentsSvc.RemovePaymentMethod(c.UserContext(), tid, uid, paymentMethodID); err != nil {
		return mapPaymentsError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// CreateMyTopupIntent handles POST /api/v1/billing/topup.
func (s *Server) CreateMyTopupIntent(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingTopupIntentRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.AmountCents <= 0 {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_amount", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	email, name := s.userMeta(c)
	currency := ""
	if req.Currency != nil {
		currency = *req.Currency
	}
	pi, err := s.paymentsSvc.CreateTopupIntent(c.UserContext(), tid, uid, email, name, req.AmountCents, currency)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(apigen.BillingTopupIntent{
		Id:           pi.ID,
		ClientSecret: pi.ClientSecret,
		AmountCents:  pi.AmountCents,
		Currency:     pi.Currency,
	})
}

// ListMySubscriptions handles GET /api/v1/billing/subscriptions.
func (s *Server) ListMySubscriptions(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	_, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	rows, err := s.paymentsSvc.ListSubscriptions(c.UserContext(), uid, MaxBillingPageSize, 0)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	items := make([]apigen.BillingSubscription, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingSubscriptionDTO(row))
	}
	total, err := s.paymentsSvc.CountSubscriptions(c.UserContext(), uid)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(apigen.BillingSubscriptionPage{
		Items: items, Total: int(total), Limit: int(MaxBillingPageSize), Offset: 0,
	})
}

// CreateMySubscription handles POST /api/v1/billing/subscriptions.
func (s *Server) CreateMySubscription(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingSubscriptionCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	email, name := s.userMeta(c)
	var pmID *uuid.UUID
	if req.PaymentMethodId != nil {
		id := *req.PaymentMethodId
		pmID = &id
	}
	row, err := s.paymentsSvc.CreateSubscription(c.UserContext(), tid, uid, email, name, req.PlanId, pmID)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingSubscriptionDTO(row))
}

// CancelMySubscription handles DELETE /api/v1/billing/subscriptions/{subscriptionId}.
func (s *Server) CancelMySubscription(c *fiber.Ctx, subscriptionID openapi_types.UUID, params apigen.CancelMySubscriptionParams) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	cancelAtPeriodEnd := true
	if params.CancelAtPeriodEnd != nil {
		cancelAtPeriodEnd = *params.CancelAtPeriodEnd
	}
	if err := s.paymentsSvc.CancelSubscription(c.UserContext(), tid, uid, subscriptionID, cancelAtPeriodEnd); err != nil {
		return mapPaymentsError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// RedeemMyPromoCode handles POST /api/v1/billing/redeem.
func (s *Server) RedeemMyPromoCode(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		// Promo codes are a billing-only feature; they don't need the
		// Stripe gateway. The handler still needs a payments service
		// today (the redeem path is implemented there); fall back to
		// 501 when the payments service is not wired.
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingPromoCodeRedeemRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Code == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.paymentsSvc.RedeemPromoCode(c.UserContext(), tid, uid, req.Code)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingLedgerDTO(database.LedgerEntry{
		ID: row.ID, TenantID: row.TenantID, UserID: row.UserID, Type: row.Type,
		AmountCents: row.AmountCents, Currency: row.Currency, Source: row.Source,
		Reference: row.Reference, IdempotencyKey: row.IdempotencyKey, CreatedAt: row.CreatedAt,
	}))
}

// ListBillingPlans handles GET /api/v1/billing/plans (public).
func (s *Server) ListBillingPlans(c *fiber.Ctx, params apigen.ListBillingPlansParams) error {
	if s.paymentsSvc == nil {
		return c.JSON(apigen.BillingPlanPage{Items: []apigen.BillingPlan{}, Total: 0, Limit: 0, Offset: 0})
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.paymentsSvc.ListActivePlans(c.UserContext(), limit, offset)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	items := make([]apigen.BillingPlan, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingPlanDTO(row))
	}
	return c.JSON(apigen.BillingPlanPage{
		Items: items, Total: len(items), Limit: int(limit), Offset: int(offset),
	})
}

// ---------------------------------------------------------------------------
// Admin: /api/v1/admin/billing/plans + promo-codes + webhook-events
// ---------------------------------------------------------------------------

// ListAdminBillingPlans handles GET /api/v1/admin/billing/plans.
func (s *Server) ListAdminBillingPlans(c *fiber.Ctx, params apigen.ListAdminBillingPlansParams) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.paymentsSvc.ListPlans(c.UserContext(), limit, offset)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	items := make([]apigen.BillingPlan, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingPlanDTO(row))
	}
	total, err := s.paymentsSvc.CountPlans(c.UserContext())
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(apigen.BillingPlanPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateAdminBillingPlan handles POST /api/v1/admin/billing/plans.
func (s *Server) CreateAdminBillingPlan(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingPlanCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.paymentsSvc.CreatePlan(c.UserContext(), tid, uid, billing.CreatePlanParams{
		Slug:                   req.Slug,
		Name:                   req.Name,
		Description:            ptrString(req.Description),
		Interval:               string(req.Interval),
		PriceCents:             req.PriceCents,
		Currency:               ptrString(req.Currency),
		IncludedQuotaCents:     ptrInt64(req.IncludedQuotaCents),
		OverageDiscountPercent: int32(ptrIntDefault(req.OverageDiscountPercent, 0)),
		Active:                 ptrBoolDefault(req.Active, true),
		SortOrder:              int32(ptrIntDefault(req.SortOrder, 0)),
	})
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingPlanDTO(row))
}

// GetAdminBillingPlan handles GET /api/v1/admin/billing/plans/{planId}.
func (s *Server) GetAdminBillingPlan(c *fiber.Ctx, planID openapi_types.UUID) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.paymentsSvc.GetPlan(c.UserContext(), planID)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(toBillingPlanDTO(row))
}

// UpdateAdminBillingPlan handles PATCH /api/v1/admin/billing/plans/{planId}.
func (s *Server) UpdateAdminBillingPlan(c *fiber.Ctx, planID openapi_types.UUID) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingPlanUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.paymentsSvc.UpdatePlan(c.UserContext(), tid, uid, planID, billing.UpdatePlanParams{
		Name:                   req.Name,
		Description:            ptrString(req.Description),
		Interval:               string(req.Interval),
		PriceCents:             req.PriceCents,
		Currency:               ptrString(req.Currency),
		IncludedQuotaCents:     ptrInt64(req.IncludedQuotaCents),
		OverageDiscountPercent: int32(ptrIntDefault(req.OverageDiscountPercent, 0)),
		Active:                 ptrBoolDefault(req.Active, true),
		SortOrder:              int32(ptrIntDefault(req.SortOrder, 0)),
	}); err != nil {
		return mapPaymentsError(c, err)
	}
	row, err := s.paymentsSvc.GetPlan(c.UserContext(), planID)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(toBillingPlanDTO(row))
}

// DeleteAdminBillingPlan handles DELETE /api/v1/admin/billing/plans/{planId}.
func (s *Server) DeleteAdminBillingPlan(c *fiber.Ctx, planID openapi_types.UUID) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.paymentsSvc.DeletePlan(c.UserContext(), tid, uid, planID); err != nil {
		return mapPaymentsError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// PushAdminBillingPlanToStripe handles POST /api/v1/admin/billing/plans/{planId}/push.
func (s *Server) PushAdminBillingPlanToStripe(c *fiber.Ctx, planID openapi_types.UUID) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.paymentsSvc.PushPlanToStripe(c.UserContext(), tid, uid, planID)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(toBillingPlanDTO(row))
}

// ListAdminBillingPromoCodes handles GET /api/v1/admin/billing/promo-codes.
func (s *Server) ListAdminBillingPromoCodes(c *fiber.Ctx, params apigen.ListAdminBillingPromoCodesParams) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.paymentsSvc.ListPromoCodes(c.UserContext(), limit, offset)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	items := make([]apigen.BillingPromoCode, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingPromoCodeDTO(row))
	}
	total, err := s.paymentsSvc.CountPromoCodes(c.UserContext())
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(apigen.BillingPromoCodePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateAdminBillingPromoCode handles POST /api/v1/admin/billing/promo-codes.
func (s *Server) CreateAdminBillingPromoCode(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	var req apigen.BillingPromoCodeCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := *req.ExpiresAt
		expiresAt = &t
	}
	var appliesTo *uuid.UUID
	if req.AppliesToPlanId != nil {
		id := *req.AppliesToPlanId
		appliesTo = &id
	}
	var maxUses *int32
	if req.MaxUses != nil {
		v := int32(*req.MaxUses)
		maxUses = &v
	}
	row, err := s.paymentsSvc.CreatePromoCode(c.UserContext(), tid, uid, billing.CreatePromoCodeParams{
		Code:            req.Code,
		Note:            ptrString(req.Note),
		CreditCents:     req.CreditCents,
		Currency:        ptrString(req.Currency),
		AppliesToPlanID: appliesTo,
		MaxUses:         maxUses,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingPromoCodeDTO(row))
}

// RevokeAdminBillingPromoCode handles POST /api/v1/admin/billing/promo-codes/{promoCodeId}/revoke.
func (s *Server) RevokeAdminBillingPromoCode(c *fiber.Ctx, promoCodeID openapi_types.UUID) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.paymentsSvc.RevokePromoCode(c.UserContext(), tid, uid, promoCodeID); err != nil {
		return mapPaymentsError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListAdminBillingWebhookEvents handles GET /api/v1/admin/billing/webhook-events.
func (s *Server) ListAdminBillingWebhookEvents(c *fiber.Ctx, params apigen.ListAdminBillingWebhookEventsParams) error {
	if s.paymentsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, paymentsDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.paymentsSvc.Repos().BillingWebhookEvents.List(c.UserContext(), limit, offset)
	if err != nil {
		return mapPaymentsError(c, err)
	}
	items := make([]apigen.BillingWebhookEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingWebhookEventDTO(row))
	}
	total, err := s.paymentsSvc.Repos().BillingWebhookEvents.Count(c.UserContext())
	if err != nil {
		return mapPaymentsError(c, err)
	}
	return c.JSON(apigen.BillingWebhookEventPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// StripeWebhook handles POST /api/v1/webhooks/stripe.
// Unauthenticated at the rbac layer — signature verification is the auth.
func (s *Server) StripeWebhook(c *fiber.Ctx) error {
	if s.paymentsSvc == nil {
		// Stripe is disabled — still return 200 so Stripe doesn't retry.
		// The body is discarded.
		return c.SendStatus(fiber.StatusOK)
	}
	sig := c.Get("Stripe-Signature")
	body := c.Body()
	if err := s.paymentsSvc.HandleWebhook(c.UserContext(), sig, body); err != nil {
		// Return the verifier's error in the response details so the
		// operator (and tests) can see WHY verification failed. The
		// outer envelope still uses the localised message.
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_webhook_signature", nil), map[string]any{
			"detail":       err.Error(),
			"body_len":     len(body),
			"sig_present":  sig != "",
		})
	}
	return c.SendStatus(fiber.StatusOK)
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

func toBillingPaymentMethodDTO(row gen.BillingPaymentMethod) apigen.BillingPaymentMethod {
	out := apigen.BillingPaymentMethod{
		Id:                    row.ID,
		TenantId:              row.TenantID,
		UserId:                row.UserID,
		StripePaymentMethodId: row.StripePaymentMethodID,
		Brand:                 row.Brand,
		Last4:                 row.Last4,
		Fingerprint:           &row.Fingerprint,
		Active:                row.Active,
		IsDefault:             row.IsDefault,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	if row.ExpMonth != nil {
		em := int(*row.ExpMonth)
		out.ExpMonth = &em
	}
	if row.ExpYear != nil {
		ey := int(*row.ExpYear)
		out.ExpYear = &ey
	}
	return out
}

func toBillingPlanDTO(row gen.BillingPlan) apigen.BillingPlan {
	out := apigen.BillingPlan{
		Id:                     row.ID,
		TenantId:               row.TenantID,
		Slug:                   row.Slug,
		Name:                   row.Name,
		Description:            &row.Description,
		Interval:               apigen.BillingPlanInterval(row.Interval),
		PriceCents:             row.PriceCents,
		Currency:               row.Currency,
		IncludedQuotaCents:     row.IncludedQuotaCents,
		OverageDiscountPercent: int(row.OverageDiscountPercent),
		Active:                 row.Active,
		SortOrder:              int(row.SortOrder),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
	if row.StripeProductID != nil {
		v := *row.StripeProductID
		out.StripeProductId = &v
	}
	if row.StripePriceID != nil {
		v := *row.StripePriceID
		out.StripePriceId = &v
	}
	return out
}

func toBillingSubscriptionDTO(row gen.BillingSubscription) apigen.BillingSubscription {
	out := apigen.BillingSubscription{
		Id:                     row.ID,
		TenantId:               row.TenantID,
		UserId:                 row.UserID,
		PlanId:                 row.PlanID,
		StripeSubscriptionId:   row.StripeSubscriptionID,
		Interval:               row.Interval,
		PriceCents:             row.PriceCents,
		Currency:               row.Currency,
		IncludedQuotaCents:     row.IncludedQuotaCents,
		OverageDiscountPercent: int(row.OverageDiscountPercent),
		Status:                 apigen.BillingSubscriptionStatus(row.Status),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
	if row.CurrentPeriodEnd != nil {
		v := *row.CurrentPeriodEnd
		out.CurrentPeriodEnd = &v
	}
	if row.CanceledAt != nil {
		v := *row.CanceledAt
		out.CanceledAt = &v
	}
	return out
}

func toBillingPromoCodeDTO(row gen.BillingPromoCode) apigen.BillingPromoCode {
	out := apigen.BillingPromoCode{
		Id:          row.ID,
		TenantId:    row.TenantID,
		Code:        row.Code,
		Note:        &row.Note,
		CreditCents: row.CreditCents,
		Currency:    row.Currency,
		TimesUsed:   int(row.TimesUsed),
		CreatedBy:   row.CreatedBy,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.AppliesToPlanID != nil {
		v := *row.AppliesToPlanID
		out.AppliesToPlanId = &v
	}
	if row.MaxUses != nil {
		v := int(*row.MaxUses)
		out.MaxUses = &v
	}
	if row.ExpiresAt != nil {
		v := *row.ExpiresAt
		out.ExpiresAt = &v
	}
	if row.RevokedAt != nil {
		v := *row.RevokedAt
		out.RevokedAt = &v
	}
	return out
}

func toBillingWebhookEventDTO(row gen.BillingWebhookEvent) apigen.BillingWebhookEvent {
	out := apigen.BillingWebhookEvent{
		Id:              row.ID,
		StripeEventId:   row.StripeEventID,
		StripeEventType: row.StripeEventType,
		Status:          apigen.BillingWebhookEventStatus(row.Status),
		ReceivedAt:      row.ReceivedAt,
	}
	if row.TenantID != nil {
		v := *row.TenantID
		out.TenantId = &v
	}
	if row.StripeApiVersion != nil {
		v := *row.StripeApiVersion
		out.StripeApiVersion = &v
	}
	if row.ErrorMessage != nil {
		v := *row.ErrorMessage
		out.ErrorMessage = &v
	}
	if row.ProcessedAt != nil {
		v := *row.ProcessedAt
		out.ProcessedAt = &v
	}
	ids := make([]openapi_types.UUID, 0, len(row.LedgerEntryIds))
	ids = append(ids, row.LedgerEntryIds...)
	out.LedgerEntryIds = &ids
	return out
}

// ptrIntDefault, ptrBoolDefault are local helpers; the package-wide
// ptrString + ptrInt64 live in compute_handlers.go.

func ptrIntDefault(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}

func ptrBoolDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
