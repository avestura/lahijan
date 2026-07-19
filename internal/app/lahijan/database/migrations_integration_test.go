// Package database: migrations_integration_test.go exercises a full up -> down
// -> up cycle against a second throwaway Postgres database (created inside the
// shared test container) to prove the migrations are reversible. This is the
// runtime guarantee behind the CI migrations.yml workflow's structural pairing
// check, and it directly ticks the WS-03 Definition of Done item "migrations up
// + down both apply cleanly".

//go:build integration

package database_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

func TestMigrations_UpThenDownThenUp_AppliesCleanly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	admin := testutil.Pool()

	// Create a throwaway database inside the shared container; we can't drop
	// the default test DB (it's in use). We connect via the postgres superuser
	// role the container was started with.
	dbName := "migrate_cycle_test_" + testutil.RandSuffix()
	_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s OWNER lahijan`, dbName))
	require.NoError(t, err, "create throwaway db")

	dsn := testutil.DatabaseDSN(dbName)
	t.Cleanup(func() {
		// DROP DATABASE needs no other connections; the migrate handle is
		// closed by the time we get here.
		_, _ = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, dbName))
	})

	src, err := iofs.New(database.MigrationsFS(), "migrations")
	require.NoError(t, err)

	// 1) Up.
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	v, dirty, err := m.Version()
	require.NoError(t, err)
	assert.False(t, dirty, "schema should not be dirty after a clean up")
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// 2) Down all.
	m, err = migrate.NewWithSourceInstance("iofs", src, dsn)
	require.NoError(t, err)
	require.NoError(t, m.Down())
	srcErr, dbErr = m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// 3) Up again — the real reversibility check.
	m, err = migrate.NewWithSourceInstance("iofs", src, dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	v2, _, err := m.Version()
	require.NoError(t, err)
	srcErr, dbErr = m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
	assert.Equal(t, v, v2, "re-up should reach the same version as the first up")
	// Sanity-check the version is at least the highest base migration number
	// present in the migrations directory. We don't hard-code the absolute
	// count here (it grows each WS); the equality above already proves
	// reversibility, and this just guards against a migration file that
	// forgets to bump the sequence.
	assert.GreaterOrEqual(t, v, uint(36), "expected at least migration 0036 by WS-17 (WS-16 left off at 0034)")
}
