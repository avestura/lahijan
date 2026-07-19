// Package api: billing_handlers.go implements the OpenAPI-derived
// billing endpoints (WS-17). Each handler is thin: parse request,
// resolve IDs from the request scope, call the billing service,
// render the response. Every privileged route is gated by
// RequirePerm via the audit gate middleware (extended in router.go
// to cover /api/v1/me/{balance,usage,ledger,receipts}* and
// /api/v1/admin/billing/* and /api/v1/admin/users/{id}/{topup,
// refund,ledger,balance}*).
//
// Error mapping follows the WS-17 DoD:
//
//   - 400 bad_request          -> malformed body / params / invalid content
//   - 401 unauthorized         -> no session (auth middleware)
//   - 403 forbidden            -> missing permission (rbac middleware)
//   - 402 payment_required     -> ErrInsufficientBalance
//   - 404 not_found            -> ledger / receipt / price missing
//   - 409 conflict             -> idempotency key collision (reserved)
//   - 501 not_implemented      -> billing subsystem disabled
//
// Per ADR-0013 the data plane is direct: balance computations go through
// the cached user_balances row; the ledger is the source of truth and
// is reconciled into the cache within 60s of any change.
package api

import (
	"errors"
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultBillingPageSize is the page size for billing list endpoints
// when the caller does not pass one. Mirrors the compute / dns /
// storage defaults so the four areas render consistently.
const DefaultBillingPageSize = 50

// MaxBillingPageSize caps a single page so a misbehaving client cannot
// request millions of rows in one call.
const MaxBillingPageSize = 200

// billingPageParams clamps + defaults limit/offset for the billing list
// endpoints.
func billingPageParams(limit, offset *int) (int32, int32) {
	lim := DefaultBillingPageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxBillingPageSize {
		lim = MaxBillingPageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return int32(lim), int32(off)
}

// billingDisabledMsg is the localised "feature disabled" message the
// handlers return when the billing subsystem is not wired.
func billingDisabledMsg(c *fiber.Ctx) string {
	return i18n.T(c.UserContext(), "billing.err_disabled", nil)
}

// billingTenantAndUser resolves the tenant + user id pair from the
// request scope. Mirrors computeTenantAndUser / dnsTenantAndUser /
// storageTenantAndUser.
func (s *Server) billingTenantAndUser(c *fiber.Ctx) (uuid.UUID, uuid.UUID, bool) {
	uid, ok := currentUserID(c)
	if !ok {
		_ = SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
		return uuid.Nil, uuid.Nil, false
	}
	tid, err := database.TenantFromContext(c.UserContext())
	if err != nil {
		_ = SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
		return uuid.Nil, uuid.Nil, false
	}
	return tid, uid, true
}

// mapBillingError translates a billing service error to the right
// envelope.
func mapBillingError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, billing.ErrLedgerEntryNotFound),
		errors.Is(err, billing.ErrUsageEventNotFound),
		errors.Is(err, billing.ErrReceiptNotFound),
		errors.Is(err, billing.ErrPriceNotFound),
		errors.Is(err, billing.ErrUserNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "billing.err_not_found", nil))
	case errors.Is(err, billing.ErrInsufficientBalance):
		return SendPaymentRequired(c, i18n.T(c.UserContext(), "billing.err_insufficient_balance", nil))
	case errors.Is(err, billing.ErrDuplicateIdempotencyKey):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "billing.err_duplicate", nil), nil)
	case errors.Is(err, billing.ErrInvalidAmount):
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_amount", nil), nil)
	case errors.Is(err, billing.ErrInvalidCurrency):
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_currency", nil), nil)
	case errors.Is(err, billing.ErrInvalidPeriod),
		errors.Is(err, billing.ErrInvalidResource):
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_bad_request", nil), nil)
	}
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// ---------------------------------------------------------------------------
// User-facing: /api/v1/me/*
// ---------------------------------------------------------------------------

// GetMyBalance handles GET /api/v1/me/balance.
func (s *Server) GetMyBalance(c *fiber.Ctx) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	bal, err := s.billingSvc.GetBalance(c.UserContext(), uid)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(toBillingBalanceDTO(bal))
}

// ListMyUsage handles GET /api/v1/me/usage.
func (s *Server) ListMyUsage(c *fiber.Ctx, params apigen.ListMyUsageParams) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	filter := billing.UsageListFilter{
		ResourceType: params.ResourceType,
	}
	if params.From != nil {
		filter.FromTS = params.From
	}
	if params.To != nil {
		filter.ToTS = params.To
	}
	rows, err := s.billingSvc.ListUsage(c.UserContext(), uid, filter, limit, offset)
	if err != nil {
		return mapBillingError(c, err)
	}
	items := make([]apigen.BillingUsageEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingUsageDTO(row))
	}
	total, err := s.billingSvc.CountUsage(c.UserContext(), uid, filter)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(apigen.BillingUsagePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// ListMyLedger handles GET /api/v1/me/ledger.
func (s *Server) ListMyLedger(c *fiber.Ctx, params apigen.ListMyLedgerParams) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.billingSvc.ListLedger(c.UserContext(), uid, limit, offset)
	if err != nil {
		return mapBillingError(c, err)
	}
	items := make([]apigen.BillingLedgerEntry, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingLedgerDTO(row))
	}
	total, err := s.billingSvc.CountLedger(c.UserContext(), uid)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(apigen.BillingLedgerPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// ListMyReceipts handles GET /api/v1/me/receipts.
func (s *Server) ListMyReceipts(c *fiber.Ctx, params apigen.ListMyReceiptsParams) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.billingSvc.ListReceipts(c.UserContext(), uid, limit, offset)
	if err != nil {
		return mapBillingError(c, err)
	}
	items := make([]apigen.BillingReceipt, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingReceiptDTO(row))
	}
	total, err := s.billingSvc.CountReceipts(c.UserContext(), uid)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(apigen.BillingReceiptPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// GenerateMyReceipt handles POST /api/v1/me/receipts.
func (s *Server) GenerateMyReceipt(c *fiber.Ctx) error {
	var req apigen.BillingReceiptGenerateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	if !req.PeriodEnd.After(req.PeriodStart) {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_period", nil), nil)
	}
	row, err := s.billingSvc.GenerateReceipt(c.UserContext(), tid, uid, billing.GenerateReceiptParams{
		UserID:      uid,
		PeriodStart: req.PeriodStart,
		PeriodEnd:   req.PeriodEnd,
	})
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingReceiptDTO(row))
}

// GetMyReceipt handles GET /api/v1/me/receipts/{receiptId}.
func (s *Server) GetMyReceipt(c *fiber.Ctx, receiptID openapi_types.UUID) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.billingSvc.GetReceipt(c.UserContext(), receiptID)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(toBillingReceiptDTO(row))
}

// GetMyReceiptPDF handles GET /api/v1/me/receipts/{receiptId}.pdf.
func (s *Server) GetMyReceiptPDF(c *fiber.Ctx, receiptID openapi_types.UUID) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.billingSvc.GetReceipt(c.UserContext(), receiptID)
	if err != nil {
		return mapBillingError(c, err)
	}
	c.Set("Content-Type", "application/pdf")
	c.Set("Content-Disposition", "attachment; filename=\"receipt-"+row.ID.String()+".pdf\"")
	return c.Send(row.PdfBytes)
}

// ---------------------------------------------------------------------------
// Admin: /api/v1/admin/billing/prices
// ---------------------------------------------------------------------------

// ListAdminBillingPrices handles GET /api/v1/admin/billing/prices.
func (s *Server) ListAdminBillingPrices(c *fiber.Ctx, params apigen.ListAdminBillingPricesParams) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.billingSvc.ListPrices(c.UserContext(), limit, offset)
	if err != nil {
		return mapBillingError(c, err)
	}
	items := make([]apigen.BillingPrice, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingPriceDTO(row))
	}
	total, err := s.billingSvc.CountPrices(c.UserContext())
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(apigen.BillingPricePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// UpsertAdminBillingPrice handles POST /api/v1/admin/billing/prices.
func (s *Server) UpsertAdminBillingPrice(c *fiber.Ctx) error {
	var req apigen.BillingPriceUpsertRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.ResourceType == "" || req.Unit == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_bad_request", nil), nil)
	}
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	tid, uid, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	var effectiveFrom time.Time
	if req.EffectiveFrom != nil {
		effectiveFrom = *req.EffectiveFrom
	}
	row, err := s.billingSvc.UpsertPrice(c.UserContext(), tid, uid, billing.PriceUpsertParams{
		ResourceType:  req.ResourceType,
		Unit:          req.Unit,
		PriceCents:    req.PriceCents,
		Currency:      ptrString(req.Currency),
		EffectiveFrom: effectiveFrom,
	})
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingPriceDTO(row))
}

// ---------------------------------------------------------------------------
// Admin: /api/v1/admin/users/{userId}/{topup,refund,ledger,balance}
// ---------------------------------------------------------------------------

// TopupAdminUser handles POST /api/v1/admin/users/{userId}/topup.
func (s *Server) TopupAdminUser(c *fiber.Ctx, userID openapi_types.UUID) error {
	var req apigen.BillingTopupRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.AmountCents <= 0 {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_amount", nil), nil)
	}
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	tid, actorID, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.billingSvc.Topup(c.UserContext(), tid, actorID, billing.TopupParams{
		UserID:      userID,
		AmountCents: req.AmountCents,
		Currency:    ptrString(req.Currency),
		Reference:   ptrString(req.Reference),
	})
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingLedgerDTO(row))
}

// RefundAdminUser handles POST /api/v1/admin/users/{userId}/refund.
func (s *Server) RefundAdminUser(c *fiber.Ctx, userID openapi_types.UUID) error {
	var req apigen.BillingRefundRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.AmountCents <= 0 {
		return SendBadRequest(c, i18n.T(c.UserContext(), "billing.err_invalid_amount", nil), nil)
	}
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	tid, actorID, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	var chargeID *uuid.UUID
	if req.ChargeLedgerId != nil {
		c := *req.ChargeLedgerId
		chargeID = &c
	}
	row, err := s.billingSvc.Refund(c.UserContext(), tid, actorID, billing.RefundParams{
		UserID:         userID,
		AmountCents:    req.AmountCents,
		Currency:       ptrString(req.Currency),
		Reference:      ptrString(req.Reference),
		ChargeLedgerID: chargeID,
	})
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toBillingLedgerDTO(row))
}

// ListAdminUserLedger handles GET /api/v1/admin/users/{userId}/ledger.
func (s *Server) ListAdminUserLedger(
	c *fiber.Ctx,
	userID openapi_types.UUID,
	params apigen.ListAdminUserLedgerParams,
) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := billingPageParams(params.Limit, params.Offset)
	rows, err := s.billingSvc.ListLedger(c.UserContext(), userID, limit, offset)
	if err != nil {
		return mapBillingError(c, err)
	}
	items := make([]apigen.BillingLedgerEntry, 0, len(rows))
	for _, row := range rows {
		items = append(items, toBillingLedgerDTO(row))
	}
	total, err := s.billingSvc.CountLedger(c.UserContext(), userID)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(apigen.BillingLedgerPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// GetAdminUserBalance handles GET /api/v1/admin/users/{userId}/balance.
func (s *Server) GetAdminUserBalance(c *fiber.Ctx, userID openapi_types.UUID) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	_, _, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	bal, err := s.billingSvc.GetBalance(c.UserContext(), userID)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(toBillingBalanceDTO(bal))
}

// RebuildAdminUserBalance handles POST /api/v1/admin/users/{userId}/balance.
func (s *Server) RebuildAdminUserBalance(c *fiber.Ctx, userID openapi_types.UUID) error {
	if s.billingSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, billingDisabledMsg(c), nil)
	}
	tid, actorID, ok := s.billingTenantAndUser(c)
	if !ok {
		return nil
	}
	bal, err := s.billingSvc.RebuildBalance(c.UserContext(), tid, actorID, userID)
	if err != nil {
		return mapBillingError(c, err)
	}
	return c.JSON(toBillingBalanceDTO(bal))
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

func toBillingBalanceDTO(row database.UserBalance) apigen.BillingBalance {
	return apigen.BillingBalance{
		TenantId:     row.TenantID,
		UserId:       row.UserID,
		BalanceCents: row.BalanceCents,
		Currency:     row.Currency,
		LastEntryAt:  row.LastEntryAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func toBillingLedgerDTO(row database.LedgerEntry) apigen.BillingLedgerEntry {
	out := apigen.BillingLedgerEntry{
		Id:             row.ID,
		TenantId:       row.TenantID,
		UserId:         row.UserID,
		Type:           apigen.BillingLedgerEntryType(row.Type),
		AmountCents:    row.AmountCents,
		Currency:       row.Currency,
		Source:         apigen.BillingLedgerEntrySource(row.Source),
		IdempotencyKey: row.IdempotencyKey,
		CreatedAt:      row.CreatedAt,
	}
	if row.Reference != "" {
		ref := row.Reference
		out.Reference = &ref
	}
	return out
}

func toBillingUsageDTO(row database.UsageEvent) apigen.BillingUsageEvent {
	out := apigen.BillingUsageEvent{
		Id:             row.ID,
		TenantId:       row.TenantID,
		UserId:         row.UserID,
		ResourceType:   row.ResourceType,
		Qty:            row.Qty,
		Unit:           row.Unit,
		StartedAt:      row.StartedAt,
		EndedAt:        row.EndedAt,
		IdempotencyKey: row.IdempotencyKey,
	}
	created := row.CreatedAt
	out.CreatedAt = &created
	return out
}

func toBillingPriceDTO(row database.Price) apigen.BillingPrice {
	out := apigen.BillingPrice{
		Id:            row.ID,
		TenantId:      row.TenantID,
		ResourceType:  row.ResourceType,
		Unit:          row.Unit,
		PriceCents:    row.PriceCents,
		Currency:      row.Currency,
		EffectiveFrom: row.EffectiveFrom,
		EffectiveTo:   row.EffectiveTo,
	}
	created := row.CreatedAt
	out.CreatedAt = &created
	updated := row.UpdatedAt
	out.UpdatedAt = &updated
	return out
}

func toBillingReceiptDTO(row database.Receipt) apigen.BillingReceipt {
	out := apigen.BillingReceipt{
		Id:          row.ID,
		TenantId:    row.TenantID,
		UserId:      row.UserID,
		PeriodStart: row.PeriodStart,
		PeriodEnd:   row.PeriodEnd,
		TotalCents:  row.TotalCents,
		Currency:    row.Currency,
		Status:      apigen.BillingReceiptStatus(row.Status),
		CreatedAt:   row.CreatedAt,
	}
	updated := row.UpdatedAt
	out.UpdatedAt = &updated
	return out
}
