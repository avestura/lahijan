// plugins_integration_test.go exercises the PluginsRepository + the
// plugin_permissions grant table end-to-end against a real Postgres. Sits
// next to the repository it covers; gated behind //go:build integration so
// the no-Docker `go test ./...` path stays fast.

//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

// repo builds a *database.PluginsRepository against the shared pool.
func repo(t *testing.T) *database.PluginsRepository {
	t.Helper()
	return testutil.Repos().Plugins
}

func TestPluginsRepo_CreateGetForTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")

	got, err := repo(t).Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)
	assert.Equal(t, tenant.ID, *got.TenantID)

	// Tenant-scoped read works.
	tctx := database.WithTenant(ctx, tenant.ID)
	gotForTenant, err := repo(t).GetForTenant(tctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, gotForTenant.ID)
}

func TestPluginsRepo_PlatformWide_VisibleToEveryTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)

	// Platform-wide plugin (NULL tenant_id).
	row := testutil.NewPlugin(ctx, t, pool, nil, "")

	tctx := database.WithTenant(ctx, tenant.ID)
	got, err := repo(t).GetForTenant(tctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)
	assert.Nil(t, got.TenantID, "platform-wide plugin keeps NULL tenant_id")
}

func TestPluginsRepo_GetForTenant_RejectsOtherTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	owner := testutil.NewTenant(ctx, t, pool)
	other := testutil.NewTenant(ctx, t, pool)

	row := testutil.NewPlugin(ctx, t, pool, &owner.ID, "")

	otherCtx := database.WithTenant(ctx, other.ID)
	_, err := repo(t).GetForTenant(otherCtx, row.ID)
	require.Error(t, err, "other tenant should not see this plugin")
}

func TestPluginsRepo_StatusTransition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)

	row := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")
	require.Equal(t, "pending", row.Status)

	require.NoError(t, repo(t).SetStatus(ctx, row.ID, database.PluginStatusActive))
	got, err := repo(t).Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, database.PluginStatusActive, got.Status)

	require.NoError(t, repo(t).SetStatus(ctx, row.ID, database.PluginStatusDisabled))
	got, err = repo(t).Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, database.PluginStatusDisabled, got.Status)
}

func TestPluginsRepo_GrantIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")

	require.NoError(t, repo(t).GrantPermission(ctx, plugin.ID, user.ID, "kv.read:cache"))
	require.NoError(t, repo(t).GrantPermission(ctx, plugin.ID, user.ID, "kv.read:cache"),
		"granting the same permission twice must be a no-op")

	grants, err := repo(t).ListPermissions(ctx, plugin.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, "kv.read:cache", grants[0].Permission)

	// Exact-grant lookup is true.
	ok, err := repo(t).HasExactGrant(ctx, plugin.ID, "kv.read:cache")
	require.NoError(t, err)
	assert.True(t, ok)

	// A different permission is not granted.
	ok, err = repo(t).HasExactGrant(ctx, plugin.ID, "kv.write:cache")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPluginsRepo_RevokePermission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")

	testutil.NewPluginPermission(ctx, t, pool, plugin.ID, user.ID, "events.emit")

	require.NoError(t, repo(t).RevokePermission(ctx, plugin.ID, "events.emit"))
	grants, err := repo(t).ListPermissions(ctx, plugin.ID)
	require.NoError(t, err)
	assert.Empty(t, grants)
}

func TestPluginsRepo_DeleteCascadesToGrants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "")

	testutil.NewPluginPermission(ctx, t, pool, plugin.ID, user.ID, "kv.read:cache")
	testutil.NewPluginPermission(ctx, t, pool, plugin.ID, user.ID, "events.emit")

	require.NoError(t, repo(t).Delete(ctx, plugin.ID))

	_, err := repo(t).Get(ctx, plugin.ID)
	require.Error(t, err, "plugin row must be gone after Delete")

	grants, err := repo(t).ListPermissions(ctx, plugin.ID)
	require.NoError(t, err)
	assert.Empty(t, grants, "grants must cascade-delete with the plugin row")
}

func TestPluginsRepo_UniqueNameVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)

	_ = testutil.NewPlugin(ctx, t, pool, &tenant.ID, "namey")
	_, err := repo(t).Create(ctx, database.CreatePluginParams{
		TenantID:  &tenant.ID,
		Name:      "namey",
		Version:   "0.0.1",
		WasmHash:  uuid.NewString(),
		WasmBytes: []byte("asm"),
		WasmSize:  3,
		Manifest:  nil,
		Status:    database.PluginStatusPending,
	})
	require.Error(t, err, "(tenant, name, version) must be unique")
	assert.True(t, database.IsUniqueViolation(err))
}
