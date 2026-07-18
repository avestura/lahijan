// service_integration_test.go exercises the marketplace.Service end-to-end
// against a real Postgres. Covers the WS-10c DoD items:
//   - install a plugin from a local marketplace index
//   - upgrade an installed plugin to a newer version
//   - the upgrade surfaces NewPermissions for admin approval

//go:build integration

package marketplace_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/marketplace"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

func TestMain(m *testing.M) { testutil.Setup(m) }

// tinyWASM is a minimal (i32, i32) -> i32 add module (mirrors
// runtime.addModule). Used as the artifact the local marketplace
// returns for Fetch().
var tinyWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x05, 0x04, 0x01, 0x01, 0x01, 0x01,
	0x07, 0x10, 0x02,
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
	0x03, 0x61, 0x64, 0x64, 0x00, 0x00,
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b,
}

// localMarketplace writes a single-plugin marketplace into a temp
// directory and returns the directory plus the computed sha256 of the
// wasm bytes (so the test can flip the index pin on).
func localMarketplace(t *testing.T, name, version, manifestYAML string, wasmBytes []byte) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "lahijan.manifest.yaml"), []byte(manifestYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "plugin.wasm"), wasmBytes, 0o644))
	sum := sha256.Sum256(wasmBytes)
	indexYAML := "version: 1\nupdated_at: \"2026-07-18T00:00:00Z\"\nplugins:\n" +
		"  - name: " + name + "\n" +
		"    version: \"" + version + "\"\n" +
		"    source: { repo: local, path: " + name + " }\n" +
		"    sha256: \"" + hex.EncodeToString(sum[:]) + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugins-marketplace.yaml"), []byte(indexYAML), 0o644))
	return dir
}

// newSvc builds a marketplace.Service with the local loaders rooted at
// dir + the shared testutil repos.
func newSvc(t *testing.T, dir string) *marketplace.Service {
	t.Helper()
	rt, err := runtime.New(context.Background(), permission.NewDBEnforcer(testutil.Repos().Plugins), runtime.Config{
		MaxMemoryBytes: 4 * runtime.WasmPageBytes,
		ExecTimeout:    500 * time.Millisecond,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	installSvc := installer.New(testutil.Repos().Plugins, rt, audit.NewDBEmitter(testutil.Repos().AuditLog)).
		WithSideRepos(testutil.Repos().PluginHTTPHandlers, testutil.Repos().PluginSubscriptions)
	idx := marketplace.NewLocalIndexLoader(dir, time.Minute)
	assets := marketplace.NewLocalAssetLoader(dir)
	return marketplace.New(idx, assets, installSvc, testutil.Repos().Plugins,
		audit.NewDBEmitter(testutil.Repos().AuditLog), nil)
}

func TestService_List_ReturnsIndex(t *testing.T) {
	t.Parallel()
	dir := localMarketplace(t, "demo", "1.0.0",
		"name: demo\nversion: 1.0.0\npermissions: [\"kv.read:cache\"]\n",
		tinyWASM)
	svc := newSvc(t, dir)

	entries, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "demo", entries[0].Name)
	assert.Equal(t, "1.0.0", entries[0].Version)
}

func TestService_Get_NotFound(t *testing.T) {
	t.Parallel()
	dir := localMarketplace(t, "demo", "1.0.0",
		"name: demo\nversion: 1.0.0\n", tinyWASM)
	svc := newSvc(t, dir)
	_, err := svc.Get(context.Background(), "nonexistent")
	require.ErrorIs(t, err, marketplace.ErrNotFound)
}

func TestService_Install_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := localMarketplace(t, "demo-install", "1.0.0",
		"name: demo-install\nversion: 1.0.0\npermissions: [\"kv.read:cache\"]\n",
		tinyWASM)
	svc := newSvc(t, dir)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	row, err := svc.Install(ctx, "demo-install", marketplace.InstallParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "demo-install", row.Name)
	assert.Equal(t, "1.0.0", row.Version)
	assert.Equal(t, "pending", row.Status)
}

func TestService_Upgrade_SurfacesNewPermissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := localMarketplace(t, "demo-up", "2.0.0",
		"name: demo-up\nversion: 2.0.0\npermissions:\n"+
			"  - \"kv.read:cache\"\n"+
			"  - \"events.emit\"\n",
		tinyWASM)
	svc := newSvc(t, dir)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	// Seed the prior version directly via the installer.
	installSvc := installer.New(testutil.Repos().Plugins, nil, audit.NoopEmitter{})
	prior, err := installSvc.Upload(ctx, installer.UploadParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
		WasmBytes: tinyWASM,
		Manifest: &manifest.Manifest{
			Name: "demo-up", Version: "1.0.0",
			Permissions: []string{"kv.read:cache"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, installSvc.Grant(ctx, prior.ID, actor.ID, "kv.read:cache", &tenant.ID, nil))

	// Upgrade through the marketplace. The upgrade's lookup uses the
	// tenant-scoped finder, so the ctx must carry the tenant id.
	tctx := database.WithTenant(ctx, tenant.ID)
	res, err := svc.Upgrade(tctx, "demo-up", marketplace.InstallParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", res.New.Version)
	// kv.read:cache was preserved.
	assert.ElementsMatch(t, []string{"kv.read:cache"}, res.PreservedGrants)
	// events.emit is new — the admin must approve.
	assert.ElementsMatch(t, []string{"events.emit"}, res.NewPermissions)
}

func TestService_Install_HashMismatch_Rejects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Build a marketplace whose index pins a BOGUS sha256.
	dir := t.TempDir()
	sub := filepath.Join(dir, "bad-hash")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	manifestYAML := "name: bad-hash\nversion: 1.0.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(sub, "lahijan.manifest.yaml"), []byte(manifestYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "plugin.wasm"), tinyWASM, 0o644))
	badIndex := "version: 1\nplugins:\n  - name: bad-hash\n    version: \"1.0.0\"\n" +
		"    source: { repo: local, path: bad-hash }\n" +
		"    sha256: \"deadbeef\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugins-marketplace.yaml"), []byte(badIndex), 0o644))

	svc := newSvc(t, dir)
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	actor := testutil.NewUser(ctx, t, pool, true)

	_, err := svc.Install(ctx, "bad-hash", marketplace.InstallParams{
		TenantID: &tenant.ID, ActorUserID: actor.ID,
	})
	require.Error(t, err)
}
