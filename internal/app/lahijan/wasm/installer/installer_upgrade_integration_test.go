// installer_upgrade_integration_test.go exercises the WS-10c installer.Upgrade
// flow end-to-end against a real Postgres. Covers the DoD items:
//   - upgrade adds a permission → admin is prompted to grant it
//   - upgrade removes a permission → grant is dropped cleanly
//   - removal cleans up event subscriptions, KV, HTTP routes

//go:build integration

package installer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
)

// upgradeService is a tiny wrapper that builds an installer.Service
// pre-wired with the side-channel repos so the cleanup assertions in
// Upgrade tests can verify the HTTP handler + subscription tables were
// drained.
func upgradeService(t *testing.T) *installer.Service {
	t.Helper()
	svc, _ := newService(t)
	repos := testutil.Repos()
	return svc.WithSideRepos(repos.PluginHTTPHandlers, repos.PluginSubscriptions)
}

// TestUpgrade_HappyPath_PreservesGrants adds a permission in v2 that
// was not in v1; v1's grants that are still in v2's manifest are
// preserved, v1's grants that are NOT in v2 are dropped, and v2's new
// permissions surface in NewPermissions for admin approval.
func TestUpgrade_HappyPath_PreservesAndDropsGrants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := upgradeService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)
	tctx := database.WithTenant(ctx, tenant.ID)

	// v1: requests + grants kv.read:cache + events.emit.
	v1 := &manifest.Manifest{
		Name: "upgrade-target", Version: "1.0.0",
		Permissions: []string{"kv.read:cache", "events.emit"},
	}
	v1Row, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: v1,
	})
	require.NoError(t, err)
	require.NoError(t, svc.Grant(ctx, v1Row.ID, actor.ID, "kv.read:cache", &tenant.ID, nil))
	require.NoError(t, svc.Grant(ctx, v1Row.ID, actor.ID, "events.emit", &tenant.ID, nil))

	// v2: drops events.emit (so its grant must be dropped), keeps
	// kv.read:cache (preserved), and adds kv.write:cache (new).
	v2 := &manifest.Manifest{
		Name: "upgrade-target", Version: "2.0.0",
		Permissions: []string{"kv.read:cache", "kv.write:cache"},
	}
	res, err := svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: v2,
	})
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", res.New.Version)
	assert.Equal(t, v1Row.ID, res.OldID)

	// Preserved grants.
	assert.ElementsMatch(t, []string{"kv.read:cache"}, res.PreservedGrants)
	// Dropped grants (was granted on v1, not in v2's manifest).
	assert.ElementsMatch(t, []string{"events.emit"}, res.DroppedGrants)
	// New permissions (in v2's manifest, not granted on v1).
	assert.ElementsMatch(t, []string{"kv.write:cache"}, res.NewPermissions)

	// The new plugin row exists with the carried-over grant.
	grants, err := testutil.Repos().Plugins.ListPermissions(ctx, res.New.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, "kv.read:cache", grants[0].Permission)

	// The old plugin row is gone.
	_, err = testutil.Repos().Plugins.Get(ctx, v1Row.ID)
	require.Error(t, err, "old plugin row should be hard-deleted")

	// Audit row emitted for the upgrade.
	var n int
	require.NoError(t, pool.QueryRow(
		ctx,
		"SELECT count(*) FROM audit_log WHERE action = $1 AND resource_id = $2",
		installer.ActionUpgrade, res.New.ID,
	).Scan(&n))
	assert.GreaterOrEqual(t, n, 1, "upgrade should emit an audit row")
}

// TestUpgrade_NoPriorVersion_Rejects asserts that Upgrade returns
// ErrNotInstalled when no prior version exists in the (tenant, name)
// scope. The marketplace caller is expected to switch to Install.
func TestUpgrade_NoPriorVersion_Rejects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := upgradeService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)
	tctx := database.WithTenant(ctx, tenant.ID)

	_, err := svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(),
		Manifest:  &manifest.Manifest{Name: "no-prior", Version: "1.0.0"},
	})
	require.ErrorIs(t, err, installer.ErrNotInstalled)
}

// TestUpgrade_SameVersion_Rejects asserts the version-compare path
// rejects same-version reinstalls. The admin must Uninstall + Install.
func TestUpgrade_SameVersion_Rejects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := upgradeService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)
	tctx := database.WithTenant(ctx, tenant.ID)

	m := &manifest.Manifest{Name: "same-ver", Version: "1.0.0"}
	_, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m,
	})
	require.NoError(t, err)

	_, err = svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m,
	})
	require.ErrorIs(t, err, installer.ErrSameVersion)
}

// TestUpgrade_Downgrade_Rejects asserts the version-compare path
// rejects downgrades.
func TestUpgrade_Downgrade_Rejects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := upgradeService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)
	tctx := database.WithTenant(ctx, tenant.ID)

	_, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(),
		Manifest:  &manifest.Manifest{Name: "down-target", Version: "2.0.0"},
	})
	require.NoError(t, err)

	_, err = svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(),
		Manifest:  &manifest.Manifest{Name: "down-target", Version: "1.0.0"},
	})
	require.ErrorIs(t, err, installer.ErrDowngrade)
}

// TestUpgrade_CleansUpSideTables asserts the WS-10c DoD item "removal
// cleans up event subscriptions, KV, HTTP routes". A v1 plugin with an
// HTTP handler mount + an event subscription is upgraded; the old
// handler + subscription rows must be gone after the upgrade returns.
//
// KV rows persist across upgrades (the namespace is reused); this is
// intentional, since the plugin's state should survive a version bump.
// CASCADE on plugin_id catches the orphaned KV rows only when the
// plugin is fully deleted, not on upgrade.
func TestUpgrade_CleansUpSideTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := upgradeService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)
	tctx := database.WithTenant(ctx, tenant.ID)

	// v1 with a handler mount + a subscription.
	v1 := &manifest.Manifest{Name: "cleanup-target", Version: "1.0.0"}
	v1Row, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: v1,
	})
	require.NoError(t, err)
	repos := testutil.Repos()
	_, err = repos.PluginHTTPHandlers.Register(ctx, database.CreateHTTPHandlerParams{
		TenantID: &tenant.ID, PluginID: v1Row.ID,
		Method: "POST", Path: "/webhook", Handler: "on_request",
	})
	require.NoError(t, err)
	_, err = repos.PluginSubscriptions.Subscribe(ctx, database.CreateSubscriptionParams{
		TenantID: &tenant.ID, PluginID: v1Row.ID,
		TopicPattern: "compute.instance.*", Handler: "on_event",
	})
	require.NoError(t, err)

	// Sanity: the rows exist before upgrade.
	h, err := repos.PluginHTTPHandlers.ListForPlugin(ctx, v1Row.ID)
	require.NoError(t, err)
	require.Len(t, h, 1)
	s, err := repos.PluginSubscriptions.ListForPlugin(ctx, v1Row.ID)
	require.NoError(t, err)
	require.Len(t, s, 1)

	// Upgrade.
	v2 := &manifest.Manifest{Name: "cleanup-target", Version: "2.0.0"}
	_, err = svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: v2,
	})
	require.NoError(t, err)

	// The OLD rows are gone (whether via explicit cleanup or CASCADE).
	h, err = repos.PluginHTTPHandlers.ListForPlugin(ctx, v1Row.ID)
	require.NoError(t, err)
	assert.Empty(t, h, "old plugin's HTTP mounts must be cleaned up")
	s2, err := repos.PluginSubscriptions.ListForPlugin(ctx, v1Row.ID)
	require.NoError(t, err)
	assert.Empty(t, s2, "old plugin's event subscriptions must be cleaned up")
}

// TestUpgrade_MultiMajor_Works verifies that several sequential
// upgrades form a continuous chain (1 → 2 → 3) with grants carried
// forward at each step.
func TestUpgrade_MultiMajor_Works(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := upgradeService(t)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)
	tctx := database.WithTenant(ctx, tenant.ID)

	// v1.
	m1 := &manifest.Manifest{
		Name: "chain", Version: "1.0.0",
		Permissions: []string{"kv.read:cache"},
	}
	r1, err := svc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m1,
	})
	require.NoError(t, err)
	require.NoError(t, svc.Grant(ctx, r1.ID, actor.ID, "kv.read:cache", &tenant.ID, nil))

	// v2 — add a permission.
	m2 := &manifest.Manifest{
		Name: "chain", Version: "2.0.0",
		Permissions: []string{"kv.read:cache", "events.emit"},
	}
	r2, err := svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m2,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"kv.read:cache"}, r2.PreservedGrants)
	require.ElementsMatch(t, []string{"events.emit"}, r2.NewPermissions)

	// Admin grants the new permission.
	require.NoError(t, svc.Grant(ctx, r2.New.ID, actor.ID, "events.emit", &tenant.ID, nil))

	// v3 — keep both permissions; nothing new.
	m3 := &manifest.Manifest{
		Name: "chain", Version: "3.0.0",
		Permissions: []string{"kv.read:cache", "events.emit"},
	}
	r3, err := svc.Upgrade(tctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: addModule(), Manifest: m3,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"kv.read:cache", "events.emit"}, r3.PreservedGrants)
	assert.Empty(t, r3.NewPermissions, "no new permissions on a steady-state upgrade")
	assert.Empty(t, r3.DroppedGrants)

	// The latest row carries both grants.
	grants, err := testutil.Repos().Plugins.ListPermissions(ctx, r3.New.ID)
	require.NoError(t, err)
	assert.Len(t, grants, 2)
}
