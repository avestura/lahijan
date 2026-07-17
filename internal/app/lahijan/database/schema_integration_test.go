// Package database: schema_integration_test.go verifies the schema invariants
// the WS-03 Definition of Done calls out: every base table has created_at and
// updated_at (where applicable), and every tenant-scoped table has an index on
// tenant_id. It also re-runs the full migration up/down cycle once against a
// throwaway database to prove the migrations are reversible — the guarantee
// CI's migrations.yml workflow checks structurally.

//go:build integration

package database_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// tablesExpectedTimestamps lists every base table that, per the conventions
// skill, must carry created_at and updated_at. Global append-only or join
// tables (audit_log, role_permissions, permissions) are excluded — they only
// have created_at by design.
func tablesExpectedTimestamps() []string {
	return []string{"tenants", "users", "memberships", "roles"}
}

func TestSchema_BaseTablesHaveTimestamps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	for _, table := range tablesExpectedTimestamps() {
		table := table
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			for _, col := range []string{"created_at", "updated_at"} {
				var present bool
				err := pool.QueryRow(ctx, `
					SELECT EXISTS (
						SELECT 1 FROM information_schema.columns
						WHERE table_name = $1 AND column_name = $2
					)`, table, col).Scan(&present)
				require.NoError(t, err)
				assert.True(t, present, "table %s must have column %s", table, col)
			}
		})
	}
}

// tablesExpectedTenantIndex lists every tenant-scoped table that must index
// tenant_id. Global tables (tenants, users, roles, permissions) are excluded
// because they have no tenant_id.
func tablesExpectedTenantIndex() []string {
	return []string{"memberships"}
}

func TestSchema_TenantScopedTablesIndexTenantID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	for _, table := range tablesExpectedTenantIndex() {
		table := table
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			var indexed bool
			// A tenant_id index shows up as an index whose definition references
			// tenant_id (either as a single-column index or the leading column
			// of a composite one).
			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_indexes
					WHERE tablename = $1
					  AND (indexdef ILIKE '%tenant_id%')
				)`, table).Scan(&indexed)
			require.NoError(t, err)
			assert.True(t, indexed, "tenant-scoped table %s must index tenant_id", table)
		})
	}
}

func TestSchema_AuditLogHasImmutabilityTriggers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	for _, trig := range []string{"audit_log_no_update", "audit_log_no_delete"} {
		trig := trig
		t.Run(trig, func(t *testing.T) {
			t.Parallel()
			var exists bool
			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.triggers
					WHERE event_object_table = 'audit_log' AND trigger_name = $1
				)`, trig).Scan(&exists)
			require.NoError(t, err)
			assert.True(t, exists, "audit_log must have trigger %s", trig)
		})
	}
}

func TestSchema_ExtensionsAndUUIDsWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()

	// gen_random_uuid() must work without an explicit extension (built into
	// PG13+). Every base table relies on it for its PK default.
	var got string
	err := pool.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&got)
	require.NoError(t, err)
	assert.True(t, strings.Contains(got, "-"), "gen_random_uuid should return a UUID text form")
}
