// Package billing: catalog.go implements the admin-managed price catalog
// (per-resource unit prices). The catalog is per-tenant + time-ranged so
// an admin can stage a future price change without invalidating history.
//
// Every privileged action emits an audit row before the side effect
// (status=pending) and marks the outcome after. Every change also emits
// into the WASM event bus so plugins can react.
package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// PriceUpsertParams carries the user-controlled fields of an upsert
// (set-current-price) call. ResourceType + Unit identify the price row;
// PriceCents is the new unit price in centimals; EffectiveFrom is when
// the new price takes effect (defaults to now).
type PriceUpsertParams struct {
	ResourceType  string
	Unit          string
	PriceCents    int64
	Currency      string
	EffectiveFrom time.Time
}

// UpsertPrice atomically closes the currently-in-effect price for
// (resource, unit) and inserts a new one starting at effectiveFrom. The
// caller MUST hold RequirePerm("billing.price_catalog.update"). Returns
// the new price row.
//
// Audit: emits ActionBillingPriceUpsert before the swap and marks the
// outcome after. Event bus: emits "billing.price.upserted" so plugins
// can recompute projections.
func (s *Service) UpsertPrice(
	ctx context.Context,
	tenantID, actorID uuid.UUID,
	params PriceUpsertParams,
) (database.Price, error) {
	if err := validateResource(params.ResourceType, params.Unit); err != nil {
		return database.Price{}, err
	}
	if params.PriceCents < 0 {
		return database.Price{}, ErrInvalidAmount
	}
	currency := s.currencyOrDefault(params.Currency)
	if err := validateCurrency(currency); err != nil {
		return database.Price{}, err
	}
	from := params.EffectiveFrom
	if from.IsZero() {
		from = s.config.DefaultEffectiveFrom
	}

	auditID := s.auditEmit(ctx, AuditPriceUpsert, ResourcePrice, tenantID, actorID, uuid.Nil, map[string]any{
		"resource_type":  params.ResourceType,
		"unit":           params.Unit,
		"price_cents":    params.PriceCents,
		"currency":       currency,
		"effective_from": from,
	})

	row, err := s.repos.BillingPrices.SetCurrentPrice(ctx, params.ResourceType, params.Unit, params.PriceCents, currency, from)
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return database.Price{}, fmt.Errorf("billing.price.upsert: %w", err)
	}

	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"new_price_id": row.ID})
	s.emitEvent(ctx, eventbus.BillingCharge, tenantID, actorID, row.ID, map[string]any{
		"topic":          "billing.price.upserted",
		"resource_type":  row.ResourceType,
		"unit":           row.Unit,
		"price_cents":    row.PriceCents,
		"effective_from": row.EffectiveFrom,
	})
	return row, nil
}

// GetPrice returns the prices row with id within the tenant in ctx.
// Returns ErrPriceNotFound when the row does not exist.
func (s *Service) GetPrice(
	ctx context.Context,
	id uuid.UUID,
) (database.Price, error) {
	row, err := s.repos.BillingPrices.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return database.Price{}, ErrPriceNotFound
		}
		return database.Price{}, fmt.Errorf("billing.price.get: %w", err)
	}
	return row, nil
}

// GetCurrentPrice returns the currently-in-effect price for the
// (resource, unit) tuple within the tenant in ctx, or ErrPriceNotFound
// when no row is currently in effect.
func (s *Service) GetCurrentPrice(
	ctx context.Context,
	resourceType, unit string,
) (database.Price, error) {
	row, err := s.repos.BillingPrices.GetCurrentPrice(ctx, resourceType, unit)
	if err != nil {
		if database.IsNoRows(err) {
			return database.Price{}, ErrPriceNotFound
		}
		return database.Price{}, fmt.Errorf("billing.price.current: %w", err)
	}
	return row, nil
}

// ListPrices returns a page of prices within the tenant in ctx. Ordered
// by resource_type then unit so the catalog reads as a stable grouped
// list.
func (s *Service) ListPrices(
	ctx context.Context,
	limit, offset int32,
) ([]database.Price, error) {
	rows, err := s.repos.BillingPrices.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.price.list: %w", err)
	}
	return rows, nil
}

// CountPrices returns the number of prices rows within the tenant in ctx.
func (s *Service) CountPrices(ctx context.Context) (int64, error) {
	n, err := s.repos.BillingPrices.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("billing.price.count: %w", err)
	}
	return n, nil
}

// GetPriceAt returns the price row effective at ts for the (resource,
// unit) tuple within the tenant in ctx. Used by the rollup job to pick
// the right price for a historical minute. Returns ErrPriceNotFound
// when no row covers ts.
func (s *Service) GetPriceAt(
	ctx context.Context,
	resourceType, unit string,
	ts time.Time,
) (database.Price, error) {
	row, err := s.repos.BillingPrices.GetPriceAt(ctx, resourceType, unit, ts)
	if err != nil {
		if database.IsNoRows(err) {
			return database.Price{}, ErrPriceNotFound
		}
		return database.Price{}, fmt.Errorf("billing.price.at: %w", err)
	}
	return row, nil
}
