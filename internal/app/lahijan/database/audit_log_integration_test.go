// Package database: audit_log_integration_test.go verifies the append-only
// guarantee of the audit_log table (ADR-0002 pillar 7, WS-03 DoD). The
// audit_log_block_mutation trigger installed by migration 0005 must reject
// every UPDATE and DELETE on the table, even from the table owner.

//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

func TestAuditLog_InsertSucceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	tenant := testutil.NewTenant(ctx, t, pool)
	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.create")
	require.NotEqual(t, uuid.Nil, row.ID, "audit row should get a generated id")
	assert.Equal(t, "compute.instance.create", row.Action)
}

func TestAuditLog_RejectsUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	tenant := testutil.NewTenant(ctx, t, pool)
	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.start")

	_, err := pool.Exec(ctx, "UPDATE audit_log SET status = 'failure' WHERE id = $1", row.ID)
	require.Error(t, err, "UPDATE on audit_log must be rejected by the trigger")
	assert.Contains(t, err.Error(), "append-only", "error should explain the append-only contract")
}

func TestAuditLog_RejectsDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	tenant := testutil.NewTenant(ctx, t, pool)
	row := testutil.NewAuditLog(ctx, t, pool, &tenant.ID, "compute.instance.delete")

	_, err := pool.Exec(ctx, "DELETE FROM audit_log WHERE id = $1", row.ID)
	require.Error(t, err, "DELETE on audit_log must be rejected by the trigger")
	assert.Contains(t, err.Error(), "append-only", "error should explain the append-only contract")

	// Row must still be present after the failed DELETE.
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE id = $1", row.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the rejected DELETE must not have removed the row")
}

func TestAuditLog_SystemEvent_NullTenantOK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	// System-level event with no tenant (e.g. admin login) must be insertable.
	row := testutil.NewAuditLog(ctx, t, pool, nil, "auth.user.login")
	require.Nil(t, row.TenantID, "system events keep tenant_id NULL")
}
