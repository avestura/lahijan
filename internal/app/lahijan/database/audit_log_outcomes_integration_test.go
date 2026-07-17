// Package database: audit_log_outcomes_integration_test.go verifies the
// MarkOutcome seam end to end against a real Postgres. The outcomes table is
// append-only (migration 0010) just like audit_log; this test exercises:
//
//   - Emit creates a row in audit_log.
//   - MarkOutcome writes a NEW row in audit_log_outcomes (no UPDATE on audit_log).
//   - Listing outcomes returns the trail newest-first.
//   - The outcomes table rejects UPDATE and DELETE (same trigger contract).
//   - MarkOutcome on a non-existent audit id fails via FK.

//go:build integration

package database_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

func TestAuditLogOutcomes_MarkOutcome_AppendsRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	repos := testutil.Repos()

	// Emit the initial audit row.
	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.start")

	// MarkOutcome via the repo.
	details := json.RawMessage(`{"duration_ms": 42}`)
	require.NoError(t, repos.AuditLog.MarkOutcome(ctx, row.ID, "success", details))

	// Verify the outcome row exists.
	outcomes, err := repos.AuditLog.ListOutcomes(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, outcomes, 1, "one outcome row expected")
	assert.Equal(t, "success", outcomes[0].Status)
	assert.Equal(t, row.ID, outcomes[0].AuditID)
	assert.JSONEq(t, `{"duration_ms":42}`, string(outcomes[0].Details))
}

func TestAuditLogOutcomes_MultipleMarks_KeepLatest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	repos := testutil.Repos()

	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.start")
	// pending -> success trail.
	require.NoError(t, repos.AuditLog.MarkOutcome(ctx, row.ID, "pending", nil))
	require.NoError(t, repos.AuditLog.MarkOutcome(ctx, row.ID, "success", nil))

	outcomes, err := repos.AuditLog.ListOutcomes(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, outcomes, 2, "two outcome rows expected (append-only)")
	assert.Equal(t, "success", outcomes[0].Status, "first element is newest")
	assert.Equal(t, "pending", outcomes[1].Status)
}

func TestAuditLogOutcomes_RejectsUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	repos := testutil.Repos()

	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.start")
	require.NoError(t, repos.AuditLog.MarkOutcome(ctx, row.ID, "pending", nil))
	outcomes, err := repos.AuditLog.ListOutcomes(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	_, err = pool.Exec(ctx, "UPDATE audit_log_outcomes SET status = 'failure' WHERE id = $1", outcomes[0].ID)
	require.Error(t, err, "UPDATE on audit_log_outcomes must be rejected")
	assert.Contains(t, err.Error(), "append-only")
}

func TestAuditLogOutcomes_RejectsDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	repos := testutil.Repos()

	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.start")
	require.NoError(t, repos.AuditLog.MarkOutcome(ctx, row.ID, "success", nil))
	outcomes, err := repos.AuditLog.ListOutcomes(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	_, err = pool.Exec(ctx, "DELETE FROM audit_log_outcomes WHERE id = $1", outcomes[0].ID)
	require.Error(t, err, "DELETE on audit_log_outcomes must be rejected")
	assert.Contains(t, err.Error(), "append-only")
}

func TestAuditLogOutcomes_MarkOutcome_FKRejectsUnknownAuditID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()

	err := repos.AuditLog.MarkOutcome(ctx, uuid.New(), "success", nil)
	require.Error(t, err, "MarkOutcome on a non-existent audit id must fail via FK")
}

func TestAuditLog_Filtered_ListAppliesFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	repos := testutil.Repos()

	// Three events: two of action=A, one of action=B.
	_, _ = pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.start', 'instance', 'success', '{}'::jsonb)",
		tenant.ID, user.ID)
	_, _ = pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.stop', 'instance', 'success', '{}'::jsonb)",
		tenant.ID, user.ID)
	_, _ = pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.start', 'instance', 'failure', '{}'::jsonb)",
		tenant.ID, user.ID)

	ctxT := database.WithTenant(ctx, tenant.ID)

	// Filter by action only.
	action := "compute.instance.start"
	rows, err := repos.AuditLog.ListForTenantFiltered(ctxT, database.AuditLogFilter{Action: &action}, 50, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 2, "filter by action should return 2 rows")

	count, err := repos.AuditLog.CountForTenantFiltered(ctxT, database.AuditLogFilter{Action: &action})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Filter by status.
	status := "failure"
	rows, err = repos.AuditLog.ListForTenantFiltered(ctxT, database.AuditLogFilter{Status: &status}, 50, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 1, "filter by status should return 1 row")

	// Filter by actor.
	rows, err = repos.AuditLog.ListForTenantFiltered(ctxT, database.AuditLogFilter{ActorUserID: &user.ID}, 50, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 3, "filter by actor should return all 3 rows")

	// No filters: all 3.
	rows, err = repos.AuditLog.ListForTenantFiltered(ctxT, database.AuditLogFilter{}, 50, 0)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

func TestAuditLog_GetForTenant_TenantScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenantA := testutil.NewTenant(ctx, t, pool)
	tenantB := testutil.NewTenant(ctx, t, pool)
	repos := testutil.Repos()

	// Audit row in tenant A.
	rowA := testutil.NewAuditLog(ctx, t, pool, &tenantA.ID, "compute.instance.start")

	// Tenant A context can read.
	ctxA := database.WithTenant(ctx, tenantA.ID)
	got, err := repos.AuditLog.GetForTenant(ctxA, rowA.ID)
	require.NoError(t, err)
	assert.Equal(t, rowA.ID, got.ID)

	// Tenant B context cannot read tenant A's row.
	ctxB := database.WithTenant(ctx, tenantB.ID)
	_, err = repos.AuditLog.GetForTenant(ctxB, rowA.ID)
	assert.Error(t, err, "tenant B must not see tenant A's audit row")
}
