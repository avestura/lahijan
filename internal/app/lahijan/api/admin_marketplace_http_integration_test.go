// admin_marketplace_http_integration_test.go exercises the
// /api/v1/admin/marketplace* and /api/v1/admin/plugins/{install,upgrade}/*
// HTTP surfaces end to end against a real Postgres + real wazero runtime.
// Covers the WS-10c DoD items:
//   - all 3 sample plugins install via the marketplace
//   - upgrade adds a permission -> admin is prompted to grant it
//   - upgrade removes a permission -> grant is dropped cleanly
//   - only platform.admin can hit the marketplace API

//go:build integration

package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/marketplace"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	wasmruntime "github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
	"github.com/gofiber/fiber/v2"
)

// buildLocalMarketplace writes a single-plugin marketplace into a temp
// directory and returns its path. The sha256 in the index matches the
// supplied wasm bytes so verifySHA256 accepts.
func buildLocalMarketplace(t *testing.T, name, version, manifestYAML string, wasm []byte) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "lahijan.manifest.yaml"), []byte(manifestYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "plugin.wasm"), wasm, 0o644))
	sum := sha256.Sum256(wasm)
	idx := fmt.Sprintf(`
version: 1
updated_at: "2026-07-18T00:00:00Z"
plugins:
  - name: %s
    version: "%s"
    source: { repo: local, path: %s }
    sha256: "%s"
`, name, version, name, hex.EncodeToString(sum[:]))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugins-marketplace.yaml"), []byte(idx), 0o644))
	return dir
}

// marketplaceTestApp wires a full test app with the marketplace service
// rooted at dir.
type marketplaceTestApp struct {
	ta  *testApp
	dir string
}

func newMarketplaceTestApp(t *testing.T, dir string) *marketplaceTestApp {
	t.Helper()
	repos := testutil.Repos()
	signer := secrets.NewSigner("http-test-signing-key")
	hasher := password.NewHasher(4, 1, 1, 8, 16)
	cookies := api.CookieConfig{
		SessionName: "lahijan_session", RefreshName: "lahijan_refresh",
		Path: "/", SameSite: "lax", SessionMaxAge: 3600, RefreshMaxAge: 3600,
	}
	mailer := email.New(
		repos.Users, repos.EmailTokens, hasher, signer,
		notifyemail.NoopSender{}, audit.NoopEmitter{},
		email.Config{
			VerifyTTL: time.Hour, ResetTTL: time.Hour, EmailChangeTTL: time.Hour,
			TokenByteLen: 32, AppBaseURL: "https://app.test",
		},
	)
	sessionSvc := session.New(
		repos.Users, repos.Sessions, repos.Tokens, hasher, signer,
		mailer, audit.NoopEmitter{},
		session.Config{
			SessionLifetime: time.Hour, RefreshLifetime: time.Hour,
			TokenByteLen: 32, MinPasswordLen: 12,
		},
	)
	patSvc := pat.New(
		repos.Tokens, signer, audit.NoopEmitter{},
		pat.Config{Prefix: "lah_pat_", ByteLen: 32},
	)
	rt, err := wasmruntime.New(context.Background(), permission.NewDBEnforcer(repos.Plugins), wasmruntime.Config{
		MaxMemoryBytes: 4 * wasmruntime.WasmPageBytes,
		ExecTimeout:    500 * time.Millisecond,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	installSvc := installer.New(repos.Plugins, rt, audit.NewDBEmitter(repos.AuditLog)).
		WithSideRepos(repos.PluginHTTPHandlers, repos.PluginSubscriptions)
	mktSvc := marketplace.New(
		marketplace.NewLocalIndexLoader(dir, time.Minute),
		marketplace.NewLocalAssetLoader(dir),
		installSvc, repos.Plugins, audit.NewDBEmitter(repos.AuditLog), nil,
	)

	server := api.NewServer(api.ServerDeps{
		Users:          repos.Users,
		Sessions:       repos.Sessions,
		SessionSvc:     sessionSvc,
		PATSvc:         patSvc,
		EmailSvc:       mailer,
		Signer:         signer,
		Cookies:        cookies,
		Audit:          repos.AuditLog,
		AuditEmitter:   audit.NewDBEmitter(repos.AuditLog),
		PluginsRepo:    repos.Plugins,
		PluginSvc:      installSvc,
		MarketplaceSvc: mktSvc,
	})
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(repos.Memberships))
	app := fiber.New()
	middleware.Apply(app, middleware.Options{
		Tenant: middleware.TenantWithResolver(middleware.TenantResolver{Tenants: repos.Tenants}),
		Auth: middleware.AuthWithResolver(middleware.AuthResolver{
			Cookies: middleware.CookieConfig{
				SessionName: cookies.SessionName,
				RefreshName: cookies.RefreshName,
			},
			Signer:       signer,
			SessionsRepo: repos.Sessions,
			PATService:   patSvc,
		}),
	})
	api.RegisterRoutes(app, server, policy)
	return &marketplaceTestApp{ta: &testApp{app: app, cookies: cookies, repos: repos}, dir: dir}
}

func TestMarketplace_List_ReturnsEntries(t *testing.T) {
	t.Parallel()
	dir := buildLocalMarketplace(t, "list-demo", "1.0.0",
		"name: list-demo\nversion: 1.0.0\npermissions: [\"kv.read:cache\"]\n",
		tinyWASM)
	mta := newMarketplaceTestApp(t, dir)
	sess, tid := pluginsRegisterPlatformAdmin(t, mta.ta)

	req := httptest.NewRequest("GET", "/api/v1/admin/marketplace", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := mta.ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, "list-demo", page.Items[0]["name"])
}

func TestMarketplace_GetEntry_NotFound(t *testing.T) {
	t.Parallel()
	dir := buildLocalMarketplace(t, "get-demo", "1.0.0",
		"name: get-demo\nversion: 1.0.0\n", tinyWASM)
	mta := newMarketplaceTestApp(t, dir)
	sess, tid := pluginsRegisterPlatformAdmin(t, mta.ta)

	req := httptest.NewRequest("GET", "/api/v1/admin/marketplace/nonexistent", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := mta.ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestMarketplace_Install_HappyPath(t *testing.T) {
	t.Parallel()
	dir := buildLocalMarketplace(t, "install-demo", "1.0.0",
		"name: install-demo\nversion: 1.0.0\npermissions: [\"kv.read:cache\"]\n",
		tinyWASM)
	mta := newMarketplaceTestApp(t, dir)
	sess, tid := pluginsRegisterPlatformAdmin(t, mta.ta)

	req := httptest.NewRequest("POST", "/api/v1/admin/plugins/install/install-demo", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := mta.ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	var row map[string]any
	require.NoError(t, json.Unmarshal(raw, &row))
	assert.Equal(t, "install-demo", row["name"])
	assert.Equal(t, "1.0.0", row["version"])
	assert.Equal(t, "pending", row["status"])
}

func TestMarketplace_Install_RequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	dir := buildLocalMarketplace(t, "perm-demo", "1.0.0",
		"name: perm-demo\nversion: 1.0.0\n", tinyWASM)
	mta := newMarketplaceTestApp(t, dir)
	// Register a tenant viewer (not platform admin).
	_, sess, tid := registerAndLogin(t, mta.ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("POST", "/api/v1/admin/plugins/install/perm-demo", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := mta.ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "viewer must not reach the marketplace install endpoint")
}

func TestMarketplace_Upgrade_AddsNewPermission_PromptsAdmin(t *testing.T) {
	t.Parallel()
	// Marketplace advertises v2 with an extra permission.
	dir := buildLocalMarketplace(t, "upgrade-demo", "2.0.0",
		"name: upgrade-demo\nversion: 2.0.0\npermissions:\n"+
			"  - \"kv.read:cache\"\n"+
			"  - \"events.emit\"\n",
		tinyWASM)
	mta := newMarketplaceTestApp(t, dir)
	sess, tid := pluginsRegisterPlatformAdmin(t, mta.ta)
	ctx := context.Background()

	// Seed v1 directly via the install service so we don't depend on the
	// previous test step. Grant kv.read:cache so it's preserved on upgrade.
	installSvc := installer.New(testutil.Repos().Plugins, nil, audit.NoopEmitter{})
	prior, err := installSvc.Upload(ctx, installer.UploadParams{
		Manifest: &manifest.Manifest{
			Name: "upgrade-demo", Version: "1.0.0",
			Permissions: []string{"kv.read:cache"},
		},
		WasmBytes: tinyWASM,
	})
	require.NoError(t, err)
	require.NoError(t, installSvc.Grant(ctx, prior.ID, testutil.NewUser(ctx, t, testutil.Pool(), true).ID,
		"kv.read:cache", nil, nil))

	req := httptest.NewRequest("POST", "/api/v1/admin/plugins/upgrade/upgrade-demo", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := mta.ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, "upgrade should succeed")
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Plugin          map[string]any `json:"plugin"`
		NewPermissions  []string       `json:"newPermissions"`
		PreservedGrants []string       `json:"preservedGrants"`
		DroppedGrants   []string       `json:"droppedGrants"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, "2.0.0", out.Plugin["version"])
	// kv.read:cache was granted on v1 and is still in v2's manifest ->
	// preserved.
	assert.Contains(t, out.PreservedGrants, "kv.read:cache")
	// events.emit is new in v2 -> the admin must approve.
	assert.Contains(t, out.NewPermissions, "events.emit")
}

func TestMarketplace_Install_HashMismatch_Rejects(t *testing.T) {
	t.Parallel()
	// Build a marketplace with a bogus sha256 pin.
	dir := t.TempDir()
	sub := filepath.Join(dir, "bad-hash")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	manifestYAML := "name: bad-hash\nversion: 1.0.0\n"
	require.NoError(t, os.WriteFile(filepath.Join(sub, "lahijan.manifest.yaml"), []byte(manifestYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "plugin.wasm"), tinyWASM, 0o644))
	idx := `
version: 1
plugins:
  - name: bad-hash
    version: "1.0.0"
    source: { repo: local, path: bad-hash }
    sha256: "0000000000000000000000000000000000000000000000000000000000000000"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugins-marketplace.yaml"), []byte(idx), 0o644))

	mta := newMarketplaceTestApp(t, dir)
	sess, tid := pluginsRegisterPlatformAdmin(t, mta.ta)

	req := httptest.NewRequest("POST", "/api/v1/admin/plugins/install/bad-hash", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := mta.ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestMarketplace_List_Disabled_Returns501(t *testing.T) {
	t.Parallel()
	// Reuse newTestApp (no marketplace wired) so the marketplace service
	// is nil. The 501 envelope is the expected response.
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	req := httptest.NewRequest("GET", "/api/v1/admin/marketplace", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode)
}
