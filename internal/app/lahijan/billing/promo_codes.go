// Package billing: promo_codes.go is the entrypoint for the
// admin-issued promo / prepaid-code surface (WS-27). Includes admin
// CRUD + user redeem + the ledger credit applied on redeem.
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
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// CreatePromoCodeParams carries the user-controlled fields of a new
// promo code.
type CreatePromoCodeParams struct {
	Code            string
	Note            string
	CreditCents     int64
	Currency        string
	AppliesToPlanID *uuid.UUID
	MaxUses         *int32
	ExpiresAt       *time.Time
}

// CreatePromoCode creates a billing_promo_codes row scoped to the
// tenant in ctx. Audit emission. The caller MUST hold
// RequirePerm("billing.promo_code.manage").
func (s *PaymentsService) CreatePromoCode(
	ctx context.Context,
	tenantID, actorID uuid.UUID,
	params CreatePromoCodeParams,
) (gen.BillingPromoCode, error) {
	code := strings.ToUpper(strings.TrimSpace(params.Code))
	if err := validatePromoCode(code); err != nil {
		return gen.BillingPromoCode{}, err
	}
	if params.CreditCents <= 0 {
		return gen.BillingPromoCode{}, ErrInvalidAmount
	}
	currency := strings.ToUpper(params.Currency)
	if currency == "" {
		currency = "USD"
	}
	if err := validateCurrency(currency); err != nil {
		return gen.BillingPromoCode{}, err
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingPromoCodeCreate, audit.ResourceBillingPromoCode, tenantID, actorID, uuid.Nil, map[string]any{
		"code":         code,
		"credit_cents": params.CreditCents,
		"currency":     currency,
		"note":         params.Note,
	})
	row, err := s.repos.BillingPromoCodes.Create(ctx, database.CreateBillingPromoCodeParams{
		Code:            code,
		Note:            params.Note,
		CreditCents:     params.CreditCents,
		Currency:        currency,
		AppliesToPlanID: params.AppliesToPlanID,
		MaxUses:         params.MaxUses,
		ExpiresAt:       params.ExpiresAt,
		CreatedBy:       actorID,
	})
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingPromoCode{}, fmt.Errorf("billing.promo_codes.create: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"promo_code_id": row.ID})
	return row, nil
}

// GetPromoCode returns the billing_promo_codes row with id within the
// tenant in ctx.
func (s *PaymentsService) GetPromoCode(ctx context.Context, id uuid.UUID) (gen.BillingPromoCode, error) {
	row, err := s.repos.BillingPromoCodes.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.BillingPromoCode{}, ErrPromoCodeNotFound
		}
		return gen.BillingPromoCode{}, fmt.Errorf("billing.promo_codes.get: %w", err)
	}
	return row, nil
}

// ListPromoCodes returns a page of non-revoked billing_promo_codes
// within the tenant in ctx.
func (s *PaymentsService) ListPromoCodes(ctx context.Context, limit, offset int32) ([]gen.BillingPromoCode, error) {
	rows, err := s.repos.BillingPromoCodes.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.promo_codes.list: %w", err)
	}
	return rows, nil
}

// CountPromoCodes returns the count of non-revoked billing_promo_codes
// within the tenant in ctx.
func (s *PaymentsService) CountPromoCodes(ctx context.Context) (int64, error) {
	n, err := s.repos.BillingPromoCodes.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("billing.promo_codes.count: %w", err)
	}
	return n, nil
}

// RevokePromoCode soft-deletes a billing_promo_codes row. Audit
// emission.
func (s *PaymentsService) RevokePromoCode(
	ctx context.Context,
	tenantID, actorID, promoID uuid.UUID,
) error {
	auditID := s.auditEmit(ctx, audit.ActionBillingPromoCodeRevoke, audit.ResourceBillingPromoCode, tenantID, actorID, promoID, nil)
	if err := s.repos.BillingPromoCodes.Revoke(ctx, promoID); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return fmt.Errorf("billing.promo_codes.revoke: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, nil)
	return nil
}

// RedeemPromoCode credits the caller's ledger for the code's
// credit_cents. Idempotent on (tenant, user, code) via the ledger
// idempotency key. Returns the ledger row.
//
// The caller is the user redeeming the code; no actorID is needed.
func (s *PaymentsService) RedeemPromoCode(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	code string,
) (gen.LedgerEntry, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if err := validatePromoCode(code); err != nil {
		return gen.LedgerEntry{}, err
	}
	row, err := s.repos.BillingPromoCodes.GetByCode(ctx, code)
	if err != nil {
		if database.IsNoRows(err) {
			return gen.LedgerEntry{}, ErrPromoCodeNotFound
		}
		return gen.LedgerEntry{}, fmt.Errorf("billing.promo_codes.redeem: get: %w", err)
	}
	if row.RevokedAt != nil {
		return gen.LedgerEntry{}, ErrPromoCodeExhausted
	}
	if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) {
		return gen.LedgerEntry{}, ErrPromoCodeExhausted
	}
	if row.MaxUses != nil && row.TimesUsed >= *row.MaxUses {
		return gen.LedgerEntry{}, ErrPromoCodeExhausted
	}
	// Plan-restricted codes: caller must already be subscribed to the
	// applies_to_plan_id. We don't enforce here today — a follow-up WS
	// will add the check; the field is captured for audit.

	// Increment the use counter atomically.
	if errInc := s.repos.BillingPromoCodes.IncrementUse(ctx, row.ID); errInc != nil {
		if errors.Is(errInc, database.ErrPromoCodeExhausted) {
			return gen.LedgerEntry{}, ErrPromoCodeExhausted
		}
		return gen.LedgerEntry{}, fmt.Errorf("billing.promo_codes.redeem: increment: %w", errInc)
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingPromoCodeRedeem, audit.ResourceBillingPromoCode, tenantID, userID, row.ID, map[string]any{
		"code":         code,
		"credit_cents": row.CreditCents,
		"currency":     row.Currency,
	})
	credit, err := s.billing.Topup(ctx, tenantID, userID, TopupParams{
		UserID:      userID,
		AmountCents: row.CreditCents,
		Currency:    row.Currency,
		Reference:   "promo_code:" + code,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateIdempotencyKey) {
			s.auditMarkOutcome(ctx, auditID, true, map[string]any{"deduplicated": true})
			return gen.LedgerEntry{}, ErrPromoCodeExhausted
		}
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.LedgerEntry{}, fmt.Errorf("billing.promo_codes.redeem: topup: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"ledger_id": credit.ID})
	s.emitEvent(ctx, eventbus.BillingPromoCodeRedeemed, tenantID, userID, credit.ID, map[string]any{
		"code":          code,
		"amount_cents":  row.CreditCents,
		"ledger_id":     credit.ID,
		"promo_code_id": row.ID.String(),
	})
	return credit, nil
}

// validatePromoCode rejects empty + whitespace + non-printable codes.
// Allows ASCII letters, digits, and dash; rejects everything else.
func validatePromoCode(code string) error {
	if code == "" {
		return ErrInvalidPromoCode
	}
	for _, r := range code {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return ErrInvalidPromoCode
	}
	return nil
}

// ErrInvalidPromoCode is returned when a promo code is empty or
// contains characters outside the safe set (uppercase ASCII letters,
// digits, dash).
var ErrInvalidPromoCode = errors.New("billing: promo code is invalid")
