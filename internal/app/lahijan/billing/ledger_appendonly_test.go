// Package billing: ledger_appendonly_test.go verifies the append-only
// guarantee of the ledger_entries table (ADR-0013 / WS-17 DoD item
// "ledger is append-only (UPDATE/DELETE rejected via trigger)"). The
// ledger_entries_block_mutation trigger installed by migration 0035
// must reject every UPDATE and DELETE on the table, even from the
// table owner.

//go:build integration

package billing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

func TestLedgerEntries_InsertSucceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)

	tenantCtx := database.WithTenant(ctx, tenant.ID)
	repos := testutil.Repos()
	row, err := repos.BillingLedger.Create(tenantCtx, database.CreateLedgerEntryParams{
		UserID:      user.ID,
		Type:        "credit",
		AmountCents: 1000,
		Currency:    "USD",
		Source:      "topup",
		Reference:   "test-topup",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, row.ID, "ledger row should get a generated id")
	assert.Equal(t, int64(1000), row.AmountCents)
}

func TestLedgerEntries_RejectsUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)

	tenantCtx := database.WithTenant(ctx, tenant.ID)
	repos := testutil.Repos()
	row, err := repos.BillingLedger.Create(tenantCtx, database.CreateLedgerEntryParams{
		UserID:      user.ID,
		Type:        "credit",
		AmountCents: 500,
		Currency:    "USD",
		Source:      "topup",
	})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "UPDATE ledger_entries SET amount_cents = 99999 WHERE id = $1", row.ID)
	require.Error(t, err, "UPDATE on ledger_entries must be rejected by the trigger")
	assert.Contains(t, err.Error(), "append-only",
		"error should explain the append-only contract")
}

func TestLedgerEntries_RejectsDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)

	tenantCtx := database.WithTenant(ctx, tenant.ID)
	repos := testutil.Repos()
	row, err := repos.BillingLedger.Create(tenantCtx, database.CreateLedgerEntryParams{
		UserID:      user.ID,
		Type:        "debit",
		AmountCents: 100,
		Currency:    "USD",
		Source:      "charge",
	})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "DELETE FROM ledger_entries WHERE id = $1", row.ID)
	require.Error(t, err, "DELETE on ledger_entries must be rejected by the trigger")
	assert.Contains(t, err.Error(), "append-only",
		"error should explain the append-only contract")

	// Row must still be present after the failed DELETE.
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger_entries WHERE id = $1", row.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the rejected DELETE must not have removed the row")
}

// TestLedgerEntries_IdempotencyKeyUnique verifies the unique index on
// idempotency_key rejects duplicates. The WS-17 DoD requires
// "idempotency keys on every metering job" — the DB enforcement is
// the second line of defense after the service-layer dedup.
func TestLedgerEntries_IdempotencyKeyUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	tenantCtx := database.WithTenant(ctx, tenant.ID)
	repos := testutil.Repos()
	key := "test-key-tenant-" + tenant.ID.String() + "-user-" + user.ID.String()
	_, err := repos.BillingLedger.Create(tenantCtx, database.CreateLedgerEntryParams{
		UserID:         user.ID,
		Type:           "debit",
		AmountCents:    10,
		Currency:       "USD",
		Source:         "charge",
		IdempotencyKey: &key,
	})
	require.NoError(t, err)

	_, err = repos.BillingLedger.Create(tenantCtx, database.CreateLedgerEntryParams{
		UserID:         user.ID,
		Type:           "debit",
		AmountCents:    10,
		Currency:       "USD",
		Source:         "charge",
		IdempotencyKey: &key,
	})
	require.Error(t, err, "duplicate idempotency key must be rejected")
	assert.True(t, database.IsUniqueViolation(err),
		"error should be a unique violation, got: %v", err)
}
