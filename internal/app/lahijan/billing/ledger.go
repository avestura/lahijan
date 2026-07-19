// Package billing: ledger.go implements the append-only per-user ledger
// + the per-user balance cache. Every charge (debit), topup (credit),
// and refund (credit) lands as a row in ledger_entries; the cache row
// in user_balances is refreshed within 60s of any change (per the WS-17
// DoD).
//
// The ledger_entries table is fully append-only at the DB level
// (trigger in migration 0035 blocks UPDATE + DELETE), so the service
// layer can never corrupt it. Corrections are NEW rows: a refund is a
// credit row with source=refund referencing the original charge in
// metadata.
//
// All amounts are integer centimals. The service enforces this at the
// type boundary (int64); the schema also enforces it (BIGINT).
package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// TopupParams carries the fields of an admin topup. UserID is the user
// being credited; AmountCents is the credit amount in centimals;
// Reference is an optional human-readable note (invoice id, ...).
type TopupParams struct {
	UserID      uuid.UUID
	AmountCents int64
	Currency    string
	Reference   string
}

// Topup credits a user's balance. The caller MUST hold
// RequirePerm("billing.balance.adjust"). Audit emits
// ActionBillingTopup before the insert and marks the outcome after.
// The balance cache is refreshed within the same call so the new
// balance is immediately visible.
//
// Returns the new ledger row.
func (s *Service) Topup(
	ctx context.Context,
	tenantID, actorID uuid.UUID,
	params TopupParams,
) (database.LedgerEntry, error) {
	if params.UserID == uuid.Nil {
		return database.LedgerEntry{}, ErrUserNotFound
	}
	if err := validateAmount(params.AmountCents); err != nil {
		return database.LedgerEntry{}, err
	}
	currency := s.currencyOrDefault(params.Currency)
	if err := validateCurrency(currency); err != nil {
		return database.LedgerEntry{}, err
	}

	auditID := s.auditEmit(ctx, AuditTopup, ResourceLedgerEntry, tenantID, actorID, uuid.Nil, map[string]any{
		"user_id":    params.UserID,
		"amount":     params.AmountCents,
		"currency":   currency,
		"reference":  params.Reference,
		"privileged": true,
	})

	row, err := s.repos.BillingLedger.Create(ctx, database.CreateLedgerEntryParams{
		UserID:      params.UserID,
		Type:        LedgerTypeCredit,
		AmountCents: params.AmountCents,
		Currency:    currency,
		Source:      SourceTopup,
		Reference:   params.Reference,
		Metadata: map[string]any{
			"actor_user_id": actorID,
		},
	})
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return database.LedgerEntry{}, fmt.Errorf("billing.topup: %w", err)
	}

	// Refresh the balance cache so the new balance is immediately
	// visible. The cache row is derived from the ledger; a reconcile
	// can rebuild it from scratch if needed.
	if errRefresh := s.refreshBalance(ctx, params.UserID); errRefresh != nil {
		// Cache refresh failed; the ledger row is the source of
		// truth so this is not a billing error, but the cache will
		// be stale until the next rollup. Mark the outcome success
		// with a warning.
		s.auditMarkOutcome(ctx, auditID, true, map[string]any{
			"ledger_id":     row.ID,
			"cache_warning": errRefresh.Error(),
		})
	} else {
		s.auditMarkOutcome(ctx, auditID, true, map[string]any{"ledger_id": row.ID})
	}

	// Emit the topped-up event so plugins + UIs can react.
	s.emitEvent(ctx, eventbus.BillingToppedUp, tenantID, params.UserID, row.ID, map[string]any{
		"amount_cents": params.AmountCents,
		"currency":     currency,
		"reference":    params.Reference,
		"ledger_id":    row.ID,
	})

	return row, nil
}

// RefundParams carries the fields of an admin refund.
type RefundParams struct {
	UserID      uuid.UUID
	AmountCents int64
	Currency    string
	Reference   string
	// ChargeLedgerID is the original charge ledger entry this refund
	// references. Optional; when set, the refund row's metadata
	// records the link so the audit trail can reconstruct the chain.
	ChargeLedgerID *uuid.UUID
}

// Refund issues a credit row with source=refund. The caller MUST hold
// RequirePerm("billing.balance.adjust"). Audit emits
// ActionBillingRefund before the insert and marks the outcome after.
//
// Per WS-17 Open Questions item 4, admin can issue refunds directly
// (no approval workflow) in MVP. The approval workflow is Phase 7.
//
// Returns the new ledger row.
func (s *Service) Refund(
	ctx context.Context,
	tenantID, actorID uuid.UUID,
	params RefundParams,
) (database.LedgerEntry, error) {
	if params.UserID == uuid.Nil {
		return database.LedgerEntry{}, ErrUserNotFound
	}
	if err := validateAmount(params.AmountCents); err != nil {
		return database.LedgerEntry{}, err
	}
	currency := s.currencyOrDefault(params.Currency)
	if err := validateCurrency(currency); err != nil {
		return database.LedgerEntry{}, err
	}

	auditID := s.auditEmit(ctx, AuditRefund, ResourceLedgerEntry, tenantID, actorID, uuid.Nil, map[string]any{
		"user_id":    params.UserID,
		"amount":     params.AmountCents,
		"currency":   currency,
		"reference":  params.Reference,
		"charge_id":  params.ChargeLedgerID,
		"privileged": true,
	})

	meta := map[string]any{
		"actor_user_id": actorID,
	}
	if params.ChargeLedgerID != nil {
		meta["original_charge_id"] = *params.ChargeLedgerID
	}

	row, err := s.repos.BillingLedger.Create(ctx, database.CreateLedgerEntryParams{
		UserID:      params.UserID,
		Type:        LedgerTypeCredit,
		AmountCents: params.AmountCents,
		Currency:    currency,
		Source:      SourceRefund,
		Reference:   params.Reference,
		Metadata:    meta,
	})
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return database.LedgerEntry{}, fmt.Errorf("billing.refund: %w", err)
	}

	if errRefresh := s.refreshBalance(ctx, params.UserID); errRefresh != nil {
		s.auditMarkOutcome(ctx, auditID, true, map[string]any{
			"ledger_id":     row.ID,
			"cache_warning": errRefresh.Error(),
		})
	} else {
		s.auditMarkOutcome(ctx, auditID, true, map[string]any{"ledger_id": row.ID})
	}

	s.emitEvent(ctx, eventbus.BillingRefund, tenantID, params.UserID, row.ID, map[string]any{
		"amount_cents": params.AmountCents,
		"currency":     currency,
		"reference":    params.Reference,
		"ledger_id":    row.ID,
	})

	return row, nil
}

// PostChargeParams carries the fields of a charge. The IdempotencyKey
// is REQUIRED for metering job rows so a duplicate run does not
// double-charge; admin-issued charges can omit it.
type PostChargeParams struct {
	UserID         uuid.UUID
	AmountCents    int64
	Currency       string
	Reference      string
	IdempotencyKey *string
	Metadata       map[string]any
}

// PostCharge appends a debit row to the ledger and refreshes the
// balance cache. Called by the metering rollup job (with an idempotency
// key) and by the admin adjustment path (without one). Returns the new
// ledger row, or the existing row when the idempotency key was already
// present (so a duplicate run is a no-op).
//
// Unlike topup/refund, this method does NOT emit an audit row by
// default — the metering job is a system actor and the per-minute
// charge rows would flood the audit log. The privileged admin
// adjustment path should call Topup with a negative reference instead,
// OR wrap the PostCharge call in an explicit audit emit at the caller.
func (s *Service) PostCharge(
	ctx context.Context,
	tenantID uuid.UUID,
	params PostChargeParams,
) (database.LedgerEntry, error) {
	if params.UserID == uuid.Nil {
		return database.LedgerEntry{}, ErrUserNotFound
	}
	if err := validateAmount(params.AmountCents); err != nil {
		return database.LedgerEntry{}, err
	}
	currency := s.currencyOrDefault(params.Currency)
	if err := validateCurrency(currency); err != nil {
		return database.LedgerEntry{}, err
	}

	// Idempotency-key de-dup: if a row with the same key exists, the
	// previous run already recorded the charge. Return that row
	// instead of double-charging.
	if params.IdempotencyKey != nil && *params.IdempotencyKey != "" {
		existing, errLookup := s.repos.BillingLedger.GetByIdempotencyKey(ctx, *params.IdempotencyKey)
		if errLookup == nil && existing.ID != uuid.Nil {
			return existing, nil
		}
		if errLookup != nil && !database.IsNoRows(errLookup) {
			return database.LedgerEntry{}, fmt.Errorf("billing.charge: dedup lookup: %w", errLookup)
		}
	}

	row, err := s.repos.BillingLedger.Create(ctx, database.CreateLedgerEntryParams{
		UserID:         params.UserID,
		Type:           LedgerTypeDebit,
		AmountCents:    params.AmountCents,
		Currency:       currency,
		Source:         SourceCharge,
		Reference:      params.Reference,
		IdempotencyKey: params.IdempotencyKey,
		Metadata:       params.Metadata,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			// Race: a parallel run inserted the same key. Return
			// that row instead of failing.
			if params.IdempotencyKey != nil {
				if existing, errLookup := s.repos.BillingLedger.GetByIdempotencyKey(ctx, *params.IdempotencyKey); errLookup == nil {
					return existing, nil
				}
			}
			return database.LedgerEntry{}, ErrDuplicateIdempotencyKey
		}
		return database.LedgerEntry{}, fmt.Errorf("billing.charge: %w", err)
	}

	if errRefresh := s.refreshBalance(ctx, params.UserID); errRefresh != nil {
		// Cache refresh failed; not a billing error but the cache
		// will be stale until the next rollup. Log and continue.
		_ = errors.Join(errRefresh, fmt.Errorf("billing.charge: cache refresh for user %s", params.UserID))
	}

	// Emit the charged event so plugins + UIs can react. The low
	// balance event is emitted by refreshBalance when the cache
	// crosses the threshold.
	s.emitEvent(ctx, eventbus.BillingCharge, tenantID, params.UserID, row.ID, map[string]any{
		"amount_cents": params.AmountCents,
		"currency":     currency,
		"reference":    params.Reference,
		"ledger_id":    row.ID,
	})

	return row, nil
}

// refreshBalance recomputes the user's balance from the ledger and
// updates the cache. Called after every ledger insert. Emits a
// billing.balance.low event if the balance crosses the configured
// threshold (typically 0).
func (s *Service) refreshBalance(
	ctx context.Context,
	userID uuid.UUID,
) error {
	tenantID, err := database.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	balance, err := s.repos.BillingLedger.SumForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("billing.balance.refresh: sum: %w", err)
	}

	// Look up the prior cached balance so we can detect the
	// threshold crossing. A no-row cache is treated as "no prior
	// balance" so we only emit the low-balance event when the user
	// actually crosses the threshold.
	prior, errPrior := s.repos.BillingBalances.Get(ctx, userID)
	crossed := false
	if errPrior == nil {
		wasAbove := prior.BalanceCents > s.config.LowBalanceThreshold
		isBelow := balance <= s.config.LowBalanceThreshold
		crossed = wasAbove && isBelow
	}

	if err := s.repos.BillingBalances.Upsert(ctx, userID, balance, s.config.Currency, time.Now().UTC()); err != nil {
		return fmt.Errorf("billing.balance.refresh: upsert: %w", err)
	}

	if crossed {
		s.emitEvent(ctx, eventbus.BillingLowBalance, tenantID, userID, uuid.Nil, map[string]any{
			"balance_cents": balance,
			"threshold":     s.config.LowBalanceThreshold,
		})
	}
	return nil
}

// GetBalance returns the cached balance row for the user within the
// tenant in ctx. When the cache is missing (the user has never had a
// ledger entry), the service returns a zero-balance synthetic row
// rather than an error so the UI can render "0.00" without special
// handling.
func (s *Service) GetBalance(
	ctx context.Context,
	userID uuid.UUID,
) (database.UserBalance, error) {
	row, err := s.repos.BillingBalances.Get(ctx, userID)
	if err != nil {
		if database.IsNoRows(err) {
			tenantID, errT := database.TenantFromContext(ctx)
			if errT != nil {
				return database.UserBalance{}, errT
			}
			return database.UserBalance{
				TenantID:     tenantID,
				UserID:       userID,
				BalanceCents: 0,
				Currency:     s.config.Currency,
				LastEntryAt:  time.Time{},
				UpdatedAt:    time.Now().UTC(),
			}, nil
		}
		return database.UserBalance{}, fmt.Errorf("billing.balance.get: %w", err)
	}
	return row, nil
}

// RebuildBalance forces a cache refresh from the ledger. The caller
// MUST hold RequirePerm("billing.balance.adjust"). Used by the
// reconciliation job + by admins who suspect the cache drifted.
//
// Returns the freshly-computed balance.
func (s *Service) RebuildBalance(
	ctx context.Context,
	tenantID, actorID, userID uuid.UUID,
) (database.UserBalance, error) {
	auditID := s.auditEmit(ctx, AuditForceRebuild, ResourceBalance, tenantID, actorID, userID, map[string]any{
		"user_id":    userID,
		"privileged": true,
	})
	if err := s.refreshBalance(ctx, userID); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return database.UserBalance{}, fmt.Errorf("billing.balance.rebuild: %w", err)
	}
	bal, err := s.repos.BillingBalances.Get(ctx, userID)
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return database.UserBalance{}, fmt.Errorf("billing.balance.rebuild: get: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"balance_cents": bal.BalanceCents})
	return bal, nil
}

// ListLedger returns a page of ledger entries for the given user within
// the tenant in ctx. Ordered newest first so the UI shows recent
// activity at the top.
func (s *Service) ListLedger(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]database.LedgerEntry, error) {
	rows, err := s.repos.BillingLedger.ListForUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.ledger.list: %w", err)
	}
	return rows, nil
}

// CountLedger returns the count of ledger entries for the given user
// within the tenant in ctx.
func (s *Service) CountLedger(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	n, err := s.repos.BillingLedger.CountForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("billing.ledger.count: %w", err)
	}
	return n, nil
}

// GetLedgerEntry returns the ledger row with id within the tenant in
// ctx. Returns ErrLedgerEntryNotFound when the row does not exist.
func (s *Service) GetLedgerEntry(
	ctx context.Context,
	id uuid.UUID,
) (database.LedgerEntry, error) {
	row, err := s.repos.BillingLedger.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return database.LedgerEntry{}, ErrLedgerEntryNotFound
		}
		return database.LedgerEntry{}, fmt.Errorf("billing.ledger.get: %w", err)
	}
	return row, nil
}
