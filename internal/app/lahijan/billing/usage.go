// Package billing: usage.go implements the user-facing usage query API +
// the metering ingestion entry point. The raw metering stream
// (usage_events table) is the source of truth for "what did this user
// consume"; the rollup job turns it into ledger charges.
//
// Every InsertUsage call is idempotent on (tenant, user, resource,
// started_at): a duplicate insert with the same idempotency key returns
// the existing row instead of double-counting. This is the WS-17 DoD
// item "idempotency keys on every metering job".
package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// RecordUsageParams carries the fields of a new usage_events row. The
// IdempotencyKey is REQUIRED for metering job rows so a duplicate run
// does not double-count; admin back-fill rows can omit it.
type RecordUsageParams struct {
	UserID         uuid.UUID
	ResourceType   string
	Qty            int64
	Unit           string
	StartedAt      time.Time
	EndedAt        time.Time
	IdempotencyKey *string
}

// RecordUsage appends a row to usage_events. Returns the new row, or
// the existing row when the idempotency key was already present.
//
// Unlike PostCharge, this method does NOT emit an audit row by default
// — the metering job is a system actor and the per-minute usage rows
// would flood the audit log. Privileged admin adjustments go through
// the ledger path (Topup / Refund).
func (s *Service) RecordUsage(
	ctx context.Context,
	tenantID uuid.UUID,
	params RecordUsageParams,
) (database.UsageEvent, error) {
	if params.UserID == uuid.Nil {
		return database.UsageEvent{}, ErrUserNotFound
	}
	if err := validateResource(params.ResourceType, params.Unit); err != nil {
		return database.UsageEvent{}, err
	}
	if params.Qty < 0 {
		return database.UsageEvent{}, ErrInvalidAmount
	}
	if !params.EndedAt.After(params.StartedAt) {
		return database.UsageEvent{}, ErrInvalidPeriod
	}

	// Idempotency-key de-dup: if a row with the same key exists, the
	// previous run already recorded the usage. Return that row
	// instead of double-counting.
	if params.IdempotencyKey != nil && *params.IdempotencyKey != "" {
		existing, errLookup := s.repos.BillingUsage.GetByIdempotencyKey(ctx, *params.IdempotencyKey)
		if errLookup == nil && existing.ID != uuid.Nil {
			return existing, nil
		}
		if errLookup != nil && !database.IsNoRows(errLookup) {
			return database.UsageEvent{}, fmt.Errorf("billing.usage: dedup lookup: %w", errLookup)
		}
	}

	row, err := s.repos.BillingUsage.Create(ctx, database.CreateUsageEventParams{
		UserID:         params.UserID,
		ResourceType:   params.ResourceType,
		Qty:            params.Qty,
		Unit:           params.Unit,
		StartedAt:      params.StartedAt,
		EndedAt:        params.EndedAt,
		IdempotencyKey: params.IdempotencyKey,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			if params.IdempotencyKey != nil {
				if existing, errLookup := s.repos.BillingUsage.GetByIdempotencyKey(ctx, *params.IdempotencyKey); errLookup == nil {
					return existing, nil
				}
			}
			return database.UsageEvent{}, ErrDuplicateIdempotencyKey
		}
		return database.UsageEvent{}, fmt.Errorf("billing.usage.create: %w", err)
	}
	return row, nil
}

// UsageListFilter mirrors database.UsageListFilter but is exported so
// callers (handlers) do not need to import the database package for
// the filter shape. Nil fields mean "do not filter on this column".
type UsageListFilter struct {
	ResourceType *string
	FromTS       *time.Time
	ToTS         *time.Time
}

// ListUsage returns a page of usage events for the given user within
// the tenant in ctx. Filtered by the supplied filter (nil fields skip
// that filter). Ordered newest first.
func (s *Service) ListUsage(
	ctx context.Context,
	userID uuid.UUID,
	f UsageListFilter,
	limit, offset int32,
) ([]database.UsageEvent, error) {
	rows, err := s.repos.BillingUsage.ListForUser(ctx, userID, database.UsageListFilter(f), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.usage.list: %w", err)
	}
	return rows, nil
}

// CountUsage returns the count of usage events matching the supplied
// filter for the user within the tenant in ctx.
func (s *Service) CountUsage(
	ctx context.Context,
	userID uuid.UUID,
	f UsageListFilter,
) (int64, error) {
	n, err := s.repos.BillingUsage.CountForUser(ctx, userID, database.UsageListFilter(f))
	if err != nil {
		return 0, fmt.Errorf("billing.usage.count: %w", err)
	}
	return n, nil
}

// UsageRollupRow is one line of a usage rollup: the total qty consumed
// for a (resource_type, unit) pair in the period.
type UsageRollupRow = database.UsageRollupRow

// RollupUsage returns one row per (resource_type, unit) consumed by
// the user in the [from, to] window within the tenant in ctx. The
// rollup job joins this with prices to compute the charge. Returns an
// empty slice when no usage events exist in the window.
func (s *Service) RollupUsage(
	ctx context.Context,
	userID uuid.UUID,
	from, to time.Time,
) ([]UsageRollupRow, error) {
	if !to.After(from) {
		return nil, ErrInvalidPeriod
	}
	rows, err := s.repos.BillingUsage.SumForUserInPeriod(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("billing.usage.rollup: %w", err)
	}
	return rows, nil
}

// ChargeForRollup multiplies each rollup row by the matching price at
// the rollup's "as-of" timestamp (the from of the window) and returns
// the total charge in integer centimals. A missing price row skips
// that resource (the metering is recorded but not billed); the audit
// trail records the missing price as a warning.
//
// This is the function the rollup job calls to turn a usage window
// into a single charge amount.
func (s *Service) ChargeForRollup(
	ctx context.Context,
	userID uuid.UUID,
	from, to time.Time,
) (int64, error) {
	rows, err := s.RollupUsage(ctx, userID, from, to)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, row := range rows {
		price, errPrice := s.GetPriceAt(ctx, row.ResourceType, row.Unit, from)
		if errPrice != nil {
			if errors.Is(errPrice, ErrPriceNotFound) {
				// No price configured for this resource in the
				// period — metering is recorded but not billed.
				// This is a soft error: the admin has not yet
				// configured a price for the resource. A
				// production deployment should alert on this.
				continue
			}
			return 0, errPrice
		}
		// total += qty * unit_price_cents. All integer math; no
		// floating point.
		total += row.TotalQty * price.PriceCents
	}
	return total, nil
}
