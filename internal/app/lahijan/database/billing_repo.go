// Package database: billing_repo.go wraps the sqlc-generated billing queries
// (WS-17). Every query is tenant-scoped via WithTenant at the repository
// seam; callers cannot pass a tenant id directly. The ledger_entries +
// usage_events tables are append-only at the DB level (trigger in migration
// 0035 / 0036), so the repository intentionally exposes only INSERT and
// SELECT on either; user_balances + receipts are mutable because they are
// caches / status flips.
//
// Five sub-repositories (Prices, Ledger, Usage, Receipts, Balances) live in
// this file. They are wired into *Repos at NewRepos time so services can
// reach them via repos.BillingPrices, repos.BillingLedger, etc.
package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ===========================================================================
// Prices: admin-managed catalog.
// ===========================================================================

// BillingPricesRepository is the persistence boundary for the prices table.
type BillingPricesRepository struct {
	q *gen.Queries
}

// NewBillingPricesRepository wraps the given sqlc queries.
func NewBillingPricesRepository(q *gen.Queries) *BillingPricesRepository {
	return &BillingPricesRepository{q: q}
}

// CreatePriceParams carries the user-controlled fields of a new prices row.
// TenantID is taken from the request context, NOT from the caller.
type CreatePriceParams struct {
	ResourceType  string
	Unit          string
	PriceCents    int64
	Currency      string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
}

// Create inserts a new prices row scoped to the tenant in ctx.
func (r *BillingPricesRepository) Create(
	ctx context.Context,
	arg CreatePriceParams,
) (gen.Price, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Price{}, err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	from := arg.EffectiveFrom
	if from.IsZero() {
		from = time.Now().UTC()
	}
	return r.q.CreatePrice(ctx, gen.CreatePriceParams{
		TenantID:      tenantID,
		ResourceType:  arg.ResourceType,
		Unit:          arg.Unit,
		PriceCents:    arg.PriceCents,
		Currency:      currency,
		EffectiveFrom: from,
		EffectiveTo:   arg.EffectiveTo,
	})
}

// Get returns the prices row with id within the tenant in ctx.
func (r *BillingPricesRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.Price, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Price{}, err
	}
	return r.q.GetPriceByID(ctx, gen.GetPriceByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetCurrentPrice returns the currently-in-effect price for the
// (resource, unit) tuple within the tenant in ctx, or pgx.ErrNoRows if none.
func (r *BillingPricesRepository) GetCurrentPrice(
	ctx context.Context,
	resourceType, unit string,
) (gen.Price, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Price{}, err
	}
	return r.q.GetCurrentPrice(ctx, gen.GetCurrentPriceParams{
		TenantID: tenantID, ResourceType: resourceType, Unit: unit,
	})
}

// GetPriceAt returns the price row effective at ts for the (resource, unit)
// tuple within the tenant in ctx. Used by the rollup job to pick the right
// price for a historical minute.
func (r *BillingPricesRepository) GetPriceAt(
	ctx context.Context,
	resourceType, unit string,
	ts time.Time,
) (gen.Price, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Price{}, err
	}
	return r.q.GetPriceAt(ctx, gen.GetPriceAtParams{
		TenantID: tenantID, ResourceType: resourceType, Unit: unit, EffectiveFrom: ts,
	})
}

// List returns a page of prices within the tenant in ctx. Ordered by
// resource_type then unit so the catalog reads as a stable grouped list.
func (r *BillingPricesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.Price, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListPrices(ctx, gen.ListPricesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of prices rows within the tenant in ctx.
func (r *BillingPricesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountPrices(ctx, tenantID)
}

// SetCurrentPrice atomically expires the currently-in-effect price for
// (resource, unit) and inserts a new one starting at effectiveFrom. The
// caller MUST hold RequirePerm("billing.price_catalog.update"). Returns
// the new price row.
func (r *BillingPricesRepository) SetCurrentPrice(
	ctx context.Context,
	resourceType, unit string,
	priceCents int64,
	currency string,
	effectiveFrom time.Time,
) (gen.Price, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Price{}, err
	}
	if currency == "" {
		currency = "USD"
	}
	if effectiveFrom.IsZero() {
		effectiveFrom = time.Now().UTC()
	}
	effectiveTo := effectiveFrom // close the prior price at the same instant the new one starts
	if err := r.q.ExpireCurrentPrice(ctx, gen.ExpireCurrentPriceParams{
		TenantID:     tenantID,
		ResourceType: resourceType,
		Unit:         unit,
		EffectiveTo:  &effectiveTo,
	}); err != nil {
		return gen.Price{}, fmt.Errorf("billing.prices: expire current: %w", err)
	}
	return r.q.CreatePrice(ctx, gen.CreatePriceParams{
		TenantID:      tenantID,
		ResourceType:  resourceType,
		Unit:          unit,
		PriceCents:    priceCents,
		Currency:      currency,
		EffectiveFrom: effectiveFrom,
	})
}

// ===========================================================================
// Ledger: append-only per-user ledger.
// ===========================================================================

// BillingLedgerRepository is the persistence boundary for the ledger_entries
// table. The table is append-only via trigger (migration 0035), so this
// repository intentionally exposes only INSERT and SELECT.
type BillingLedgerRepository struct {
	q *gen.Queries
}

// NewBillingLedgerRepository wraps the given sqlc queries.
func NewBillingLedgerRepository(q *gen.Queries) *BillingLedgerRepository {
	return &BillingLedgerRepository{q: q}
}

// CreateLedgerEntryParams carries the fields a service supplies when
// appending a row to ledger_entries. Type must be "credit" or "debit";
// AmountCents must be non-negative; IdempotencyKey is optional (NULLable for
// admin-issued rows, REQUIRED for metering job rows so a duplicate run does
// not double-charge).
type CreateLedgerEntryParams struct {
	UserID         uuid.UUID
	Type           string
	AmountCents    int64
	Currency       string
	Source         string
	Reference      string
	IdempotencyKey *string
	Metadata       map[string]any
}

// Create appends a row to ledger_entries. The trigger on the table makes
// this the only mutating operation permitted.
func (r *BillingLedgerRepository) Create(
	ctx context.Context,
	arg CreateLedgerEntryParams,
) (gen.LedgerEntry, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.LedgerEntry{}, err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	var meta json.RawMessage = json.RawMessage("{}")
	if arg.Metadata != nil {
		raw, errM := json.Marshal(arg.Metadata)
		if errM != nil {
			return gen.LedgerEntry{}, fmt.Errorf("billing.ledger: marshal metadata: %w", errM)
		}
		meta = raw
	}
	return r.q.CreateLedgerEntry(ctx, gen.CreateLedgerEntryParams{
		TenantID:       tenantID,
		UserID:         arg.UserID,
		Type:           arg.Type,
		AmountCents:    arg.AmountCents,
		Currency:       currency,
		Source:         arg.Source,
		Reference:      arg.Reference,
		IdempotencyKey: arg.IdempotencyKey,
		Metadata:       meta,
	})
}

// Get returns the ledger row with id within the tenant in ctx.
func (r *BillingLedgerRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.LedgerEntry, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.LedgerEntry{}, err
	}
	return r.q.GetLedgerEntryByID(ctx, gen.GetLedgerEntryByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByIdempotencyKey returns the ledger row with idempotency key within
// the tenant in ctx, or pgx.ErrNoRows if no such row exists. Used by the
// metering de-dup check before insert.
func (r *BillingLedgerRepository) GetByIdempotencyKey(
	ctx context.Context,
	key string,
) (gen.LedgerEntry, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.LedgerEntry{}, err
	}
	k := key
	return r.q.GetLedgerEntryByIdempotencyKey(ctx, gen.GetLedgerEntryByIdempotencyKeyParams{
		TenantID: tenantID, IdempotencyKey: &k,
	})
}

// ListForUser returns a page of ledger entries for the given user within
// the tenant in ctx. Ordered newest first so the UI shows recent activity
// at the top.
func (r *BillingLedgerRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.LedgerEntry, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListLedgerEntriesForUser(ctx, gen.ListLedgerEntriesForUserParams{
		TenantID: tenantID, UserID: userID, Limit: limit, Offset: offset,
	})
}

// CountForUser returns the count of ledger entries for the given user
// within the tenant in ctx.
func (r *BillingLedgerRepository) CountForUser(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountLedgerEntriesForUser(ctx, gen.CountLedgerEntriesForUserParams{
		TenantID: tenantID, UserID: userID,
	})
}

// SumForUser returns the authoritative balance for the given user within
// the tenant in ctx: the sum of credits - debits in integer centimals.
// This is the value the user_balances cache holds; callers that need
// "current balance" should read the cache (faster) and fall back to this
// when the cache is stale.
func (r *BillingLedgerRepository) SumForUser(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.SumLedgerEntriesForUser(ctx, gen.SumLedgerEntriesForUserParams{
		TenantID: tenantID, UserID: userID,
	})
}

// SumForUserUpTo returns the balance as-of ts for the given user within
// the tenant in ctx. Used by the receipt generator to compute the
// period-end balance.
func (r *BillingLedgerRepository) SumForUserUpTo(
	ctx context.Context,
	userID uuid.UUID,
	ts time.Time,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.SumLedgerEntriesForUserUpTo(ctx, gen.SumLedgerEntriesForUserUpToParams{
		TenantID: tenantID, UserID: userID, AsOf: ts,
	})
}

// SumChargesInPeriod returns the total amount_cents of debit entries for
// the given user in the [from, to] period within the tenant in ctx. The
// receipt generator uses this for the period's "total charged" line.
func (r *BillingLedgerRepository) SumChargesInPeriod(
	ctx context.Context,
	userID uuid.UUID,
	from, to time.Time,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.SumChargesInPeriod(ctx, gen.SumChargesInPeriodParams{
		TenantID: tenantID, UserID: userID, FromTs: from, ToTs: to,
	})
}

// ===========================================================================
// Balances: per-user cache.
// ===========================================================================

// BillingBalancesRepository is the persistence boundary for the
// user_balances cache table.
type BillingBalancesRepository struct {
	q *gen.Queries
}

// NewBillingBalancesRepository wraps the given sqlc queries.
func NewBillingBalancesRepository(q *gen.Queries) *BillingBalancesRepository {
	return &BillingBalancesRepository{q: q}
}

// Get returns the cached balance row for the user within the tenant in
// ctx, or pgx.ErrNoRows if no ledger entry has ever been written.
func (r *BillingBalancesRepository) Get(
	ctx context.Context,
	userID uuid.UUID,
) (gen.UserBalance, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.UserBalance{}, err
	}
	return r.q.GetUserBalance(ctx, gen.GetUserBalanceParams{
		TenantID: tenantID, UserID: userID,
	})
}

// Upsert atomically refreshes the cached balance for the user within the
// tenant in ctx. amountCents is the freshly-computed balance; lastEntryAt
// is the timestamp of the ledger entry that produced it.
func (r *BillingBalancesRepository) Upsert(
	ctx context.Context,
	userID uuid.UUID,
	amountCents int64,
	currency string,
	lastEntryAt time.Time,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if currency == "" {
		currency = "USD"
	}
	return r.q.UpsertUserBalance(ctx, gen.UpsertUserBalanceParams{
		TenantID:     tenantID,
		UserID:       userID,
		BalanceCents: amountCents,
		Currency:     currency,
		LastEntryAt:  lastEntryAt,
	})
}

// ListZeroBalances returns every user in the tenant whose balance is
// <= 0 and whose last_entry_at is older than the supplied cutoff. The
// enforcement job uses this to find users past their grace period.
func (r *BillingBalancesRepository) ListZeroBalances(
	ctx context.Context,
	cutoff time.Time,
) ([]gen.UserBalance, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListZeroBalances(ctx, gen.ListZeroBalancesParams{
		TenantID: tenantID, LastEntryAt: cutoff,
	})
}

// ===========================================================================
// Usage events: raw metering stream.
// ===========================================================================

// BillingUsageRepository is the persistence boundary for the usage_events
// table. The table is append-only (it's a metering stream — corrections
// are new offsetting rows), so this repository intentionally exposes only
// INSERT and SELECT.
type BillingUsageRepository struct {
	q *gen.Queries
}

// NewBillingUsageRepository wraps the given sqlc queries.
func NewBillingUsageRepository(q *gen.Queries) *BillingUsageRepository {
	return &BillingUsageRepository{q: q}
}

// CreateUsageEventParams carries the fields of a new usage_events row.
// IdempotencyKey is optional but strongly recommended for metering job
// rows so a duplicate run does not double-count.
type CreateUsageEventParams struct {
	UserID         uuid.UUID
	ResourceType   string
	Qty            int64
	Unit           string
	StartedAt      time.Time
	EndedAt        time.Time
	IdempotencyKey *string
}

// Create appends a row to usage_events.
func (r *BillingUsageRepository) Create(
	ctx context.Context,
	arg CreateUsageEventParams,
) (gen.UsageEvent, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.UsageEvent{}, err
	}
	return r.q.CreateUsageEvent(ctx, gen.CreateUsageEventParams{
		TenantID:       tenantID,
		UserID:         arg.UserID,
		ResourceType:   arg.ResourceType,
		Qty:            arg.Qty,
		Unit:           arg.Unit,
		StartedAt:      arg.StartedAt,
		EndedAt:        arg.EndedAt,
		IdempotencyKey: arg.IdempotencyKey,
	})
}

// GetByIdempotencyKey returns the usage row with the given idempotency key
// within the tenant in ctx, or pgx.ErrNoRows if no such row exists. Used
// by the metering de-dup check before insert.
func (r *BillingUsageRepository) GetByIdempotencyKey(
	ctx context.Context,
	key string,
) (gen.UsageEvent, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.UsageEvent{}, err
	}
	k := key
	return r.q.GetUsageEventByIdempotencyKey(ctx, gen.GetUsageEventByIdempotencyKeyParams{
		TenantID: tenantID, IdempotencyKey: &k,
	})
}

// UsageListFilter carries the optional filters for the user-facing
// /me/usage list endpoint.
type UsageListFilter struct {
	ResourceType *string
	FromTS       *time.Time
	ToTS         *time.Time
}

// ListForUser returns a page of usage events for the given user within
// the tenant in ctx, filtered by the supplied filter (nil fields skip
// that filter). Ordered newest first.
func (r *BillingUsageRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	f UsageListFilter,
	limit, offset int32,
) ([]gen.UsageEvent, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListUsageEventsForUser(ctx, gen.ListUsageEventsForUserParams{
		TenantID:     tenantID,
		UserID:       userID,
		ResourceType: f.ResourceType,
		FromTs:       f.FromTS,
		ToTs:         f.ToTS,
		Limit:        limit,
		Offset:       offset,
	})
}

// CountForUser returns the count of usage events for the given user
// matching the same filter used by ListForUser.
func (r *BillingUsageRepository) CountForUser(
	ctx context.Context,
	userID uuid.UUID,
	f UsageListFilter,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountUsageEventsForUser(ctx, gen.CountUsageEventsForUserParams{
		TenantID:     tenantID,
		UserID:       userID,
		ResourceType: f.ResourceType,
		FromTs:       f.FromTS,
		ToTs:         f.ToTS,
	})
}

// UsageRollupRow is one line of a usage rollup: the total qty consumed
// for a (resource_type, unit) pair in the period.
type UsageRollupRow = gen.SumUsageEventsForUserInPeriodRow

// SumForUserInPeriod returns one row per (resource_type, unit) consumed
// by the user in the [from, to] window within the tenant in ctx. The
// rollup job joins this with prices to compute the charge.
func (r *BillingUsageRepository) SumForUserInPeriod(
	ctx context.Context,
	userID uuid.UUID,
	from, to time.Time,
) ([]UsageRollupRow, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.SumUsageEventsForUserInPeriod(ctx, gen.SumUsageEventsForUserInPeriodParams{
		TenantID: tenantID, UserID: userID, FromTs: from, ToTs: to,
	})
}

// ===========================================================================
// Receipts: per-user-per-period billing summary.
// ===========================================================================

// BillingReceiptsRepository is the persistence boundary for the receipts
// table.
type BillingReceiptsRepository struct {
	q *gen.Queries
}

// NewBillingReceiptsRepository wraps the given sqlc queries.
func NewBillingReceiptsRepository(q *gen.Queries) *BillingReceiptsRepository {
	return &BillingReceiptsRepository{q: q}
}

// CreateReceiptParams carries the fields of a new receipts row.
type CreateReceiptParams struct {
	UserID       uuid.UUID
	PeriodStart  time.Time
	PeriodEnd    time.Time
	TotalCents   int64
	Currency     string
	PdfBytes     []byte
	Status       string
}

// Create inserts a new receipts row scoped to the tenant in ctx.
func (r *BillingReceiptsRepository) Create(
	ctx context.Context,
	arg CreateReceiptParams,
) (gen.Receipt, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Receipt{}, err
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	status := arg.Status
	if status == "" {
		status = "ready"
	}
	pdf := arg.PdfBytes
	if pdf == nil {
		pdf = []byte{}
	}
	return r.q.CreateReceipt(ctx, gen.CreateReceiptParams{
		TenantID:    tenantID,
		UserID:      arg.UserID,
		PeriodStart: arg.PeriodStart,
		PeriodEnd:   arg.PeriodEnd,
		TotalCents:  arg.TotalCents,
		Currency:    currency,
		PdfBytes:    pdf,
		Status:      status,
	})
}

// Get returns the receipt with id within the tenant in ctx.
func (r *BillingReceiptsRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.Receipt, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Receipt{}, err
	}
	return r.q.GetReceiptByID(ctx, gen.GetReceiptByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetForPeriod returns the existing receipt for the (user, period) tuple
// within the tenant in ctx, or pgx.ErrNoRows if none. Used by the
// generator to upsert.
func (r *BillingReceiptsRepository) GetForPeriod(
	ctx context.Context,
	userID uuid.UUID,
	periodStart, periodEnd time.Time,
) (gen.Receipt, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Receipt{}, err
	}
	return r.q.GetReceiptForPeriod(ctx, gen.GetReceiptForPeriodParams{
		TenantID: tenantID, UserID: userID,
		PeriodStart: periodStart, PeriodEnd: periodEnd,
	})
}

// ListForUser returns a page of receipts for the given user within the
// tenant in ctx. Ordered newest first (latest period first).
func (r *BillingReceiptsRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.Receipt, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListReceiptsForUser(ctx, gen.ListReceiptsForUserParams{
		TenantID: tenantID, UserID: userID, Limit: limit, Offset: offset,
	})
}

// CountForUser returns the count of receipts for the given user within
// the tenant in ctx.
func (r *BillingReceiptsRepository) CountForUser(
	ctx context.Context,
	userID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountReceiptsForUser(ctx, gen.CountReceiptsForUserParams{
		TenantID: tenantID, UserID: userID,
	})
}

// UpdatePDF attaches the generated PDF body to an existing receipt and
// flips its status to "ready".
func (r *BillingReceiptsRepository) UpdatePDF(
	ctx context.Context,
	id uuid.UUID,
	pdfBytes []byte,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.UpdateReceiptPDF(ctx, gen.UpdateReceiptPDFParams{
		TenantID: tenantID, ID: id, PdfBytes: pdfBytes,
	})
}
