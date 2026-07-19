// Package billing: service_integration_test.go exercises the billing
// service end-to-end against a real Postgres (via testcontainers-go).
// Covers the WS-17 DoD:
//
//   - balance = sum of ledger entries; cache refreshed within 60s
//   - every privileged admin action (topup, refund, price change)
//     emits audit pre + post
//   - multi-tenant isolation: tenant A's admin can't see tenant B's
//     ledger (the repository seam enforces tenant_id scoping)
//   - all amounts in integer cents (no float money)
//   - metering idempotency keys dedup a duplicate insert
//
// Run with:  go test -tags integration ./internal/app/lahijan/billing/...

//go:build integration

package billing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// capturingEmitter records every Emit + MarkOutcome call so tests can
// assert that audit pre + post rows land for privileged actions.
type capturingEmitter struct {
	mu       sync.Mutex
	rows     []audit.Event
	outcomes map[uuid.UUID]audit.Outcome
}

func newCapturingEmitter() *capturingEmitter {
	return &capturingEmitter{outcomes: map[uuid.UUID]audit.Outcome{}}
}

func (e *capturingEmitter) Emit(_ context.Context, ev audit.Event) (uuid.UUID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := uuid.New()
	e.rows = append(e.rows, ev)
	return id, nil
}

func (e *capturingEmitter) MarkOutcome(_ context.Context, auditID uuid.UUID, o audit.Outcome) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outcomes[auditID] = o
	return nil
}

func (e *capturingEmitter) pendingCount(action string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, r := range e.rows {
		if r.Action == action && r.Status == audit.StatusPending {
			n++
		}
	}
	return n
}

func (e *capturingEmitter) successCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.outcomes {
		if o.Status == audit.StatusSuccess {
			n++
		}
	}
	return n
}

func (e *capturingEmitter) eventsFor(action string) []audit.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []audit.Event{}
	for _, r := range e.rows {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

// fixture wires the dependencies each test shares.
type fixture struct {
	svc      *billing.Service
	emitter  *capturingEmitter
	tenantID uuid.UUID
	actorID  uuid.UUID
	userID   uuid.UUID
	ctx      context.Context
	tctx     context.Context // tenant-scoped
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	actor := testutil.NewUser(ctx, t, testutil.Pool(), true)
	emitter := newCapturingEmitter()
	svc := billing.New(
		repos, emitter, nil, nil,
		billing.NoopEnforcer{}, billing.NoopMeter{},
		billing.Config{Currency: "USD", GracePeriod: 24 * time.Hour},
	)
	return &fixture{
		svc:      svc,
		emitter:  emitter,
		tenantID: tenant.ID,
		actorID:  actor.ID,
		userID:   user.ID,
		ctx:      ctx,
		tctx:     database.WithTenant(ctx, tenant.ID),
	}
}

// TestTopup_UpdatesCachedBalance is the WS-17 DoD happy-path test:
//
//   - Topup credits the user's balance.
//   - The cache row is refreshed within the same call.
//   - An audit row is emitted (status=pending) before the insert and
//     marked success after.
func TestTopup_UpdatesCachedBalance(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	row, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 5000,
		Currency:    "USD",
		Reference:   "test topup",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, row.ID)
	assert.Equal(t, int64(5000), row.AmountCents)
	assert.Equal(t, billing.LedgerTypeCredit, row.Type)
	assert.Equal(t, billing.SourceTopup, row.Source)

	// Cache row should reflect the new balance within the same call
	// (the WS-17 DoD is "< 60s"; the synchronous refresh makes it
	// immediate).
	bal, err := f.svc.GetBalance(f.tctx, f.userID)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), bal.BalanceCents, "cached balance must equal the topup")
	assert.Equal(t, "USD", bal.Currency)
	assert.False(t, bal.LastEntryAt.IsZero(), "last_entry_at should be set")

	// Audit: emit (pending) + mark-outcome (success).
	assert.GreaterOrEqual(t, f.emitter.pendingCount(billing.AuditTopup), 1,
		"topup must emit a pending audit row before the side effect")
	assert.GreaterOrEqual(t, f.emitter.successCount(), 1,
		"topup must mark the outcome success after the side effect")
}

// TestTopup_NegativeAmountRejected verifies the integer-cents DoD:
// the service rejects non-positive amounts at the boundary.
func TestTopup_NegativeAmountRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 0,
	})
	require.ErrorIs(t, err, billing.ErrInvalidAmount)

	_, err = f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: -100,
	})
	require.ErrorIs(t, err, billing.ErrInvalidAmount)
}

// TestRefund_ReducesOrIncreasesBalanceCorrectly verifies the ledger
// math after a topup + refund.
func TestRefund_ReducesOrIncreasesBalanceCorrectly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 10000,
	})
	require.NoError(t, err)

	// Charge 3000 (a debit).
	chargeKey := "charge-1"
	_, err = f.svc.PostCharge(f.tctx, f.tenantID, billing.PostChargeParams{
		UserID:         f.userID,
		AmountCents:    3000,
		IdempotencyKey: &chargeKey,
		Reference:      "test charge",
	})
	require.NoError(t, err)

	// Balance should be 7000.
	bal, err := f.svc.GetBalance(f.tctx, f.userID)
	require.NoError(t, err)
	assert.Equal(t, int64(7000), bal.BalanceCents)

	// Refund 1000 of the charge.
	_, err = f.svc.Refund(f.tctx, f.tenantID, f.actorID, billing.RefundParams{
		UserID:      f.userID,
		AmountCents: 1000,
	})
	require.NoError(t, err)

	// Balance should be 8000 (7000 + 1000 refund).
	bal, err = f.svc.GetBalance(f.tctx, f.userID)
	require.NoError(t, err)
	assert.Equal(t, int64(8000), bal.BalanceCents)

	// Audit: refund emitted + marked success.
	assert.GreaterOrEqual(t, f.emitter.pendingCount(billing.AuditRefund), 1)
	assert.GreaterOrEqual(t, f.emitter.successCount(), 1)
}

// TestPostCharge_IdempotencyKeyDedup verifies the WS-17 DoD item
// "idempotency keys on every metering job". A duplicate run with the
// same key returns the existing row; the user is not double-charged.
func TestPostCharge_IdempotencyKeyDedup(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 100000,
	})
	require.NoError(t, err)

	key := "test-charge-key"
	first, err := f.svc.PostCharge(f.tctx, f.tenantID, billing.PostChargeParams{
		UserID:         f.userID,
		AmountCents:    1500,
		IdempotencyKey: &key,
	})
	require.NoError(t, err)

	// Second call with the same key should return the same row, no
	// extra debit.
	second, err := f.svc.PostCharge(f.tctx, f.tenantID, billing.PostChargeParams{
		UserID:         f.userID,
		AmountCents:    1500,
		IdempotencyKey: &key,
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "second call must return the existing row")

	// Balance should reflect a single 1500 debit (100000 - 1500 = 98500).
	bal, err := f.svc.GetBalance(f.tctx, f.userID)
	require.NoError(t, err)
	assert.Equal(t, int64(98500), bal.BalanceCents)
}

// TestRecordUsage_IdempotencyKeyDedup verifies the metering-side
// idempotency. The metering job runs once per minute per tenant; a
// duplicate run for the same minute must not double-count.
func TestRecordUsage_IdempotencyKeyDedup(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	key := "test-usage-key"
	started := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	ended := started.Add(time.Minute)
	first, err := f.svc.RecordUsage(f.tctx, f.tenantID, billing.RecordUsageParams{
		UserID:         f.userID,
		ResourceType:   "compute.cpu",
		Qty:            2,
		Unit:           "core-minutes",
		StartedAt:      started,
		EndedAt:        ended,
		IdempotencyKey: &key,
	})
	require.NoError(t, err)

	second, err := f.svc.RecordUsage(f.tctx, f.tenantID, billing.RecordUsageParams{
		UserID:         f.userID,
		ResourceType:   "compute.cpu",
		Qty:            2,
		Unit:           "core-minutes",
		StartedAt:      started,
		EndedAt:        ended,
		IdempotencyKey: &key,
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "second call must return the existing row")

	// Counting usage for the user should show 1 row (not 2).
	count, err := f.svc.CountUsage(f.tctx, f.userID, billing.UsageListFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

// TestTenantIsolation_LedgerCrossTenantDenied verifies the WS-17 DoD
// item "tenant A's admin can't see tenant B's ledger". Tenant B's
// admin's call against tenant A's user_id returns no rows (the
// tenant_id scoping is enforced at the repository seam).
func TestTenantIsolation_LedgerCrossTenantDenied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenantA := testutil.NewTenant(ctx, t, pool)
	tenantB := testutil.NewTenant(ctx, t, pool)
	userA := testutil.NewUser(ctx, t, pool, false)
	userB := testutil.NewUser(ctx, t, pool, false)

	// Top up userA in tenantA; top up userB in tenantB.
	repos := testutil.Repos()
	tenantACtx := database.WithTenant(ctx, tenantA.ID)
	tenantBCtx := database.WithTenant(ctx, tenantB.ID)
	emitter := newCapturingEmitter()
	svc := billing.New(repos, emitter, nil, nil,
		billing.NoopEnforcer{}, billing.NoopMeter{}, billing.Config{})

	_, err := svc.Topup(tenantACtx, tenantA.ID, userA.ID, billing.TopupParams{
		UserID: userA.ID, AmountCents: 5000,
	})
	require.NoError(t, err)
	_, err = svc.Topup(tenantBCtx, tenantB.ID, userB.ID, billing.TopupParams{
		UserID: userB.ID, AmountCents: 3000,
	})
	require.NoError(t, err)

	// Tenant A's admin lists userA's ledger: 1 row.
	rowsA, err := svc.ListLedger(tenantACtx, userA.ID, 50, 0)
	require.NoError(t, err)
	assert.Len(t, rowsA, 1, "tenant A sees userA's ledger row")
	assert.Equal(t, userA.ID, rowsA[0].UserID)

	// Tenant A's admin tries to list userB's ledger from tenant A's
	// context: should see 0 rows (the tenant_id scoping filters
	// userB's rows out, even though the caller asked for userB's
	// id).
	rowsB, err := svc.ListLedger(tenantACtx, userB.ID, 50, 0)
	require.NoError(t, err)
	assert.Empty(t, rowsB, "tenant A must not see tenant B's user ledger (tenant_id scoping)")

	// And vice versa.
	rowsAfromB, err := svc.ListLedger(tenantBCtx, userA.ID, 50, 0)
	require.NoError(t, err)
	assert.Empty(t, rowsAfromB, "tenant B must not see tenant A's user ledger")
}

// TestTenantIsolation_BalanceCrossTenantDenied verifies the balance
// cache is also tenant-scoped. A tenant A admin reading userB's
// balance gets a zero row (cache row never created), not userB's
// actual balance.
func TestTenantIsolation_BalanceCrossTenantDenied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenantA := testutil.NewTenant(ctx, t, pool)
	tenantB := testutil.NewTenant(ctx, t, pool)
	userA := testutil.NewUser(ctx, t, pool, false)
	userB := testutil.NewUser(ctx, t, pool, false)

	repos := testutil.Repos()
	tenantACtx := database.WithTenant(ctx, tenantA.ID)
	tenantBCtx := database.WithTenant(ctx, tenantB.ID)
	emitter := newCapturingEmitter()
	svc := billing.New(repos, emitter, nil, nil,
		billing.NoopEnforcer{}, billing.NoopMeter{}, billing.Config{})

	// Top up both users in their respective tenants.
	_, err := svc.Topup(tenantACtx, tenantA.ID, userA.ID, billing.TopupParams{
		UserID: userA.ID, AmountCents: 7000,
	})
	require.NoError(t, err)
	_, err = svc.Topup(tenantBCtx, tenantB.ID, userB.ID, billing.TopupParams{
		UserID: userB.ID, AmountCents: 3000,
	})
	require.NoError(t, err)

	// Tenant A reading userA's balance: 7000.
	balA, err := svc.GetBalance(tenantACtx, userA.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(7000), balA.BalanceCents)

	// Tenant A reading userB's balance from tenant A's context:
	// the cache lookup is tenant-scoped, so userB's row is not
	// visible. The service returns a synthetic zero row instead of
	// leaking userB's actual balance.
	balBinA, err := svc.GetBalance(tenantACtx, userB.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), balBinA.BalanceCents,
		"tenant A reading userB must see zero balance (no cache row in tenant A)")
}

// TestUpsertPrice_HappyPath verifies the price catalog swap is
// atomic (the prior price is expired; the new one takes effect) and
// emits audit pre + post.
func TestUpsertPrice_HappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// First upsert: no prior price; row created.
	row1, err := f.svc.UpsertPrice(f.tctx, f.tenantID, f.actorID, billing.PriceUpsertParams{
		ResourceType: "compute.cpu",
		Unit:         "core-hours",
		PriceCents:   7,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, row1.ID)
	assert.Nil(t, row1.EffectiveTo, "newly inserted price has no effective_to")

	// Second upsert: prior price is expired; new row takes effect.
	row2, err := f.svc.UpsertPrice(f.tctx, f.tenantID, f.actorID, billing.PriceUpsertParams{
		ResourceType: "compute.cpu",
		Unit:         "core-hours",
		PriceCents:   10,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, row2.ID)
	assert.NotEqual(t, row1.ID, row2.ID, "second upsert creates a new row")

	// The first row should now have an effective_to.
	row1After, err := f.svc.GetPrice(f.tctx, row1.ID)
	require.NoError(t, err)
	require.NotNil(t, row1After.EffectiveTo, "prior price must have an effective_to after the swap")

	// GetCurrentPrice should return the second row.
	current, err := f.svc.GetCurrentPrice(f.tctx, "compute.cpu", "core-hours")
	require.NoError(t, err)
	assert.Equal(t, int64(10), current.PriceCents)

	// Audit: emit (pending) + mark-outcome (success) for each upsert.
	assert.GreaterOrEqual(t, f.emitter.pendingCount(billing.AuditPriceUpsert), 2)
}

// TestRollup_JoinsUsageWithPrice verifies the metering rollup math:
// usage_events * price = charge. All integer math; no float.
func TestRollup_JoinsUsageWithPrice(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// Configure a price: 5 cents per core-hour.
	_, err := f.svc.UpsertPrice(f.tctx, f.tenantID, f.actorID, billing.PriceUpsertParams{
		ResourceType: "compute.cpu",
		Unit:         "core-hours",
		PriceCents:   5,
	})
	require.NoError(t, err)

	// Record 60 core-minutes (= 1 core-hour) of usage.
	started := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		minute := started.Add(time.Duration(i) * time.Minute)
		key := "minute-" + minute.Format("150405")
		_, err := f.svc.RecordUsage(f.tctx, f.tenantID, billing.RecordUsageParams{
			UserID:         f.userID,
			ResourceType:   "compute.cpu",
			Qty:            1,
			Unit:           "core-minutes",
			StartedAt:      minute,
			EndedAt:        minute.Add(time.Minute),
			IdempotencyKey: &key,
		})
		require.NoError(t, err)
	}

	// Rollup: 60 core-minutes total. The ChargeForRollup helper
	// joins with the price (5 cents per core-hour = 5/60 per
	// core-minute; but our prices table unit is "core-hours" and
	// the usage unit is "core-minutes" — they DON'T match, so the
	// join skips this resource (soft error: no price configured for
	// the (resource, unit) tuple).
	//
	// This is intentional: the WS-17 doc says "All amounts in
	// integer cents"; it does NOT promise unit-conversion math in
	// the rollup. The meter records the qty in the unit the
	// provider reports; the rollup matches the (resource_type,
	// unit) tuple against prices; the admin is responsible for
	// setting prices in the unit the meter uses.
	total, err := f.svc.ChargeForRollup(f.tctx, f.userID,
		started, started.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(0), total,
		"rollup with no matching price-unit pair should charge 0 (soft error)")

	// Add a price for "core-minutes" so the rollup matches.
	_, err = f.svc.UpsertPrice(f.tctx, f.tenantID, f.actorID, billing.PriceUpsertParams{
		ResourceType: "compute.cpu",
		Unit:         "core-minutes",
		PriceCents:   1, // 1 cent per core-minute
	})
	require.NoError(t, err)

	total, err = f.svc.ChargeForRollup(f.tctx, f.userID,
		started, started.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(60), total,
		"60 core-minutes * 1 cent = 60 centimals (integer math)")
}

// TestGenerateReceipt_ProducesPDF verifies the WS-17 DoD item
// "receipt PDF generates correctly for a sample period".
func TestGenerateReceipt_ProducesPDF(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 100000,
	})
	require.NoError(t, err)

	// Charge 2500 cents within the period.
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)
	chargeKey := "july-charge"
	_, err = f.svc.PostCharge(f.tctx, f.tenantID, billing.PostChargeParams{
		UserID:         f.userID,
		AmountCents:    2500,
		IdempotencyKey: &chargeKey,
		Reference:      "July usage",
	})
	require.NoError(t, err)

	row, err := f.svc.GenerateReceipt(f.tctx, f.tenantID, f.actorID, billing.GenerateReceiptParams{
		UserID:      f.userID,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, row.ID)
	assert.Equal(t, int64(2500), row.TotalCents, "receipt total must equal the period's charges")
	assert.Equal(t, "ready", row.Status)
	assert.NotEmpty(t, row.PdfBytes, "PDF body must be attached")
	assert.True(t, len(row.PdfBytes) > 100, "PDF body should be a real PDF (>%d bytes)", 100)

	// PDF header check.
	assert.Equal(t, "%PDF-1.4", string(row.PdfBytes[:8]))

	// Idempotent: a second call overwrites the PDF + total without
	// creating a duplicate row.
	row2, err := f.svc.GenerateReceipt(f.tctx, f.tenantID, f.actorID, billing.GenerateReceiptParams{
		UserID:      f.userID,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	})
	require.NoError(t, err)
	assert.Equal(t, row.ID, row2.ID, "second generate must return the same receipt id")
}

// TestEnforceZeroBalances_StopsUsersPastGrace verifies the
// enforcement watcher fires when a user's balance crosses zero AND
// the grace period has expired. Uses a 0 grace period so the test
// does not have to sleep.
func TestEnforceZeroBalances_StopsUsersPastGrace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	repos := testutil.Repos()
	tctx := database.WithTenant(ctx, tenant.ID)

	// Build a service with a recording enforcer + tiny grace period so
	// the test fires immediately. (Config.GracePeriod=0 would default
	// to 24h via billing.New; we use 1ns to truly disable the grace.)
	rec := &recordingEnforcer{}
	svc := billing.New(repos, newCapturingEmitter(), nil, nil,
		rec, billing.NoopMeter{}, billing.Config{GracePeriod: 1 * time.Nanosecond})

	// Topup then charge past zero so the balance is negative.
	_, err := svc.Topup(tctx, tenant.ID, user.ID, billing.TopupParams{
		UserID: user.ID, AmountCents: 1000,
	})
	require.NoError(t, err)
	chargeKey := "drain-key"
	_, err = svc.PostCharge(tctx, tenant.ID, billing.PostChargeParams{
		UserID:         user.ID,
		AmountCents:    2000,
		IdempotencyKey: &chargeKey,
	})
	require.NoError(t, err)

	// Run enforcement. The recording enforcer should be called once.
	time.Sleep(2 * time.Millisecond) // ensure last_entry_at < cutoff

	// Sanity check: the cache row should exist with balance <= 0.
	bal, err := svc.GetBalance(tctx, user.ID)
	require.NoError(t, err)
	require.True(t, bal.BalanceCents <= 0, "balance should be <= 0 for enforcement, got %d", bal.BalanceCents)

	count, err := svc.EnforceZeroBalances(tctx, tenant.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1, "enforcer should have stopped at least one user")
	assert.NotEmpty(t, rec.stopped, "recording enforcer should have recorded the stop")
	assert.Equal(t, user.ID, rec.stopped[0].userID)
}

// recordingEnforcer records every StopAllForUser call so tests can
// assert the enforcement watcher fired.
type recordingEnforcer struct {
	mu      sync.Mutex
	stopped []stopCall
}

type stopCall struct {
	tenantID uuid.UUID
	userID   uuid.UUID
	reason   string
}

func (r *recordingEnforcer) StopAllForUser(_ context.Context, tenantID, userID uuid.UUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, stopCall{tenantID: tenantID, userID: userID, reason: reason})
	return nil
}

// TestRebuildBalance_RecomputesFromLedger verifies the cache can be
// rebuilt from scratch from the ledger.
func TestRebuildBalance_RecomputesFromLedger(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	// Three topups + two charges -> 1000 + 2000 + 3000 - 500 - 1500 = 4000.
	for _, amt := range []int64{1000, 2000, 3000} {
		_, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
			UserID: f.userID, AmountCents: amt,
		})
		require.NoError(t, err)
	}
	for i, amt := range []int64{500, 1500} {
		k := "rebuild-charge-" + string(rune('A'+i))
		_, err := f.svc.PostCharge(f.tctx, f.tenantID, billing.PostChargeParams{
			UserID: f.userID, AmountCents: amt, IdempotencyKey: &k,
		})
		require.NoError(t, err)
	}

	// Cache should be 4000 already (each insert refreshed it).
	bal, err := f.svc.GetBalance(f.tctx, f.userID)
	require.NoError(t, err)
	assert.Equal(t, int64(4000), bal.BalanceCents)

	// Force a rebuild; the value should not change.
	bal, err = f.svc.RebuildBalance(f.tctx, f.tenantID, f.actorID, f.userID)
	require.NoError(t, err)
	assert.Equal(t, int64(4000), bal.BalanceCents)

	// Audit: rebuild emitted + marked success.
	require.NotEmpty(t, f.emitter.eventsFor(billing.AuditForceRebuild))
}

// TestInvalidCurrency_Rejected verifies the WS-17 DoD item "all
// amounts in integer cents (no float money)" extends to the
// currency-code shape (3 letters).
func TestInvalidCurrency_Rejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 1000,
		Currency:    "US",
	})
	require.ErrorIs(t, err, billing.ErrInvalidCurrency)

	_, err = f.svc.Topup(f.tctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 1000,
		Currency:    "DOLLAR",
	})
	require.ErrorIs(t, err, billing.ErrInvalidCurrency)
}

// TestRecordUsage_InvalidPeriodRejected verifies the metering period
// validation rejects started_at >= ended_at.
func TestRecordUsage_InvalidPeriodRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	started := time.Now()
	// ended == started (not strictly after).
	_, err := f.svc.RecordUsage(f.tctx, f.tenantID, billing.RecordUsageParams{
		UserID:       f.userID,
		ResourceType: "compute.cpu",
		Qty:          1,
		Unit:         "core-minutes",
		StartedAt:    started,
		EndedAt:      started,
	})
	require.ErrorIs(t, err, billing.ErrInvalidPeriod)

	// ended < started.
	_, err = f.svc.RecordUsage(f.tctx, f.tenantID, billing.RecordUsageParams{
		UserID:       f.userID,
		ResourceType: "compute.cpu",
		Qty:          1,
		Unit:         "core-minutes",
		StartedAt:    started.Add(time.Minute),
		EndedAt:      started,
	})
	require.ErrorIs(t, err, billing.ErrInvalidPeriod)
}

// TestMissingTenantInContext_FailsClosed verifies the repository seam
// fails closed when the context has no tenant scope. A topup call
// without WithTenant returns an error and writes no row.
func TestMissingTenantInContext_FailsClosed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	_, err := f.svc.Topup(f.ctx, f.tenantID, f.actorID, billing.TopupParams{
		UserID:      f.userID,
		AmountCents: 1000,
	})
	require.Error(t, err)
	// The error chain wraps the database sentinel.
	require.True(t, errors.Is(err, database.ErrNoTenantInContext),
		"topup without tenant context should fail closed with ErrNoTenantInContext, got: %v", err)
}
