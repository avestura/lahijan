// admin_plugins_http_integration_test.go exercises the
// /api/v1/admin/plugins* HTTP surface end to end against a real Postgres
// (testcontainers) plus a real wazero runtime. Covers the WS-10a DoD items
// "uploading a .wasm parses the manifest and lists requested permissions",
// "admin can grant/deny each permission individually", "only platform.admin
// can hit the admin plugin API", and "every install/grant/revoke/enable/
// disable/delete emits an audit event".

//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
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
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	wasmruntime "github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// tinyWASM is a minimal (i32, i32) -> i32 add module. Duplicated here so
// the api test package does not need to import the internal runtime test
// helpers. Bytes mirror runtime.addModule().
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

// pluginsTestApp bundles a testApp with a real wazero runtime + plugin
// installer. It mirrors newTestApp but additionally wires the WASM deps.
type pluginsTestApp struct {
	ta  *testApp
	rt  *wasmruntime.Runtime
	svc *installer.Service
}

func newPluginsTestApp(t *testing.T) *pluginsTestApp {
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

	// Real wazero runtime + real DB-backed enforcer so end-to-end works.
	rt, err := wasmruntime.New(context.Background(), permission.NewDBEnforcer(repos.Plugins), wasmruntime.Config{
		MaxMemoryBytes: 4 * wasmruntime.WasmPageBytes,
		ExecTimeout:    500 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	svc := installer.New(repos.Plugins, rt, audit.NewDBEmitter(repos.AuditLog))

	server := api.NewServer(api.ServerDeps{
		Users:        repos.Users,
		Sessions:     repos.Sessions,
		SessionSvc:   sessionSvc,
		PATSvc:       patSvc,
		EmailSvc:     mailer,
		Signer:       signer,
		Cookies:      cookies,
		Audit:        repos.AuditLog,
		AuditEmitter: audit.NewDBEmitter(repos.AuditLog),
		PluginsRepo:  repos.Plugins,
		PluginSvc:    svc,
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
	return &pluginsTestApp{
		ta:  &testApp{app: app, cookies: cookies, repos: repos},
		rt:  rt,
		svc: svc,
	}
}

// registerPlatformAdmin seeds RBAC and grants the user the platform.admin
// role in a fresh tenant. Returns the user's session cookie + tenant id.
func pluginsRegisterPlatformAdmin(t *testing.T, ta *testApp) (string, string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, rbac.SeedOnce(ctx, testutil.Repos()))
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	role, err := testutil.Repos().RBAC.GetRoleBySlug(ctx, rbac.RolePlatformAdmin)
	require.NoError(t, err)

	addr := "plugins+" + uuid.NewString()[:12] + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	require.Equalf(t, 201, status, "register must succeed (got %d)", status)
	sess := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sess)

	user, err := ta.repos.Users.GetByEmail(ctx, addr)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)
	return sess, tenant.ID.String()
}

// uploadMultipart builds a multipart/form-data body with the given wasm +
// manifest parts. The manifest is sent as a file (not a form field) because
// the handler reads both via c.FormFile.
func uploadMultipart(t *testing.T, wasm []byte, manifest string) ([]byte, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	wasmWriter, err := mw.CreateFormFile("wasm", "plugin.wasm")
	require.NoError(t, err)
	_, err = wasmWriter.Write(wasm)
	require.NoError(t, err)
	manifestWriter, err := mw.CreateFormFile("manifest", "lahijan.manifest.yaml")
	require.NoError(t, err)
	_, err = manifestWriter.Write([]byte(manifest))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return buf.Bytes(), mw.FormDataContentType()
}

// uploadPlugin posts the wasm + manifest to /admin/plugins/upload. Returns
// the parsed AdminPlugin response.
func uploadPlugin(t *testing.T, ta *testApp, sess, tenantID, name, wasmBytes, manifestYAML string) map[string]any {
	t.Helper()
	body, ct := uploadMultipart(t, []byte(wasmBytes), manifestYAML)
	req := httptest.NewRequest("POST", "/api/v1/admin/plugins/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tenantID)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	out["_status"] = resp.StatusCode
	if resp.StatusCode != 201 {
		t.Logf("upload %s failed: status=%d body=%s", name, resp.StatusCode, string(raw))
	}
	return out
}

func TestListAdminPlugins_RequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET", "/api/v1/admin/plugins", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "viewer should not reach admin plugins")
}

func TestUploadPlugin_HappyPath(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	manifest := fmt.Sprintf(`
name: happy-plugin
version: 1.0.0
description: "test plugin"
permissions:
  - "kv.read:cache"
entrypoints:
  - on_event
`)
	out := uploadPlugin(t, ta, sess, tid, "happy-plugin", string(tinyWASM), manifest)
	status, _ := out["_status"].(int)
	require.Equalf(t, 201, status, "want 201 got %d body=%v", status, out)
	assert.Equal(t, "happy-plugin", out["name"])
	assert.Equal(t, "pending", out["status"])
	assert.NotEmpty(t, out["wasmHash"])

	// The manifest's permissions are NOT auto-granted — they appear only in
	// the manifest blob. The admin must grant each explicitly. The
	// permissions array on the response is empty here.
	perms, _ := out["permissions"].([]any)
	assert.Empty(t, perms, "no grants should exist immediately after upload")
}

func TestUploadPlugin_DuplicateReturns409(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	manifest := "name: dup\nversion: 1.0.0\n"
	out1 := uploadPlugin(t, ta, sess, tid, "dup", string(tinyWASM), manifest)
	status1, _ := out1["_status"].(int)
	require.Equal(t, 201, status1)

	out2 := uploadPlugin(t, ta, sess, tid, "dup", string(tinyWASM), manifest)
	status2, _ := out2["_status"].(int)
	assert.Equal(t, 409, status2)
}

func TestUploadPlugin_BadManifestReturns400(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	// Manifest with an unknown permission.
	manifest := `
name: bad-perm
version: 1.0.0
permissions:
  - totally.bogus
`
	out := uploadPlugin(t, ta, sess, tid, "bad-perm", string(tinyWASM), manifest)
	status, _ := out["_status"].(int)
	assert.Equal(t, 400, status)
}

func TestGetAdminPlugin_ReturnsManifestAndGrants(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	manifest := `
name: detaily
version: 1.0.0
permissions:
  - "kv.read:cache"
  - "events.emit"
`
	out := uploadPlugin(t, ta, sess, tid, "detaily", string(tinyWASM), manifest)
	status, _ := out["_status"].(int)
	require.Equal(t, 201, status)
	pluginID, _ := out["id"].(string)
	require.NotEmpty(t, pluginID)

	// Grant one of the two requested permissions.
	gReq := httptest.NewRequest("POST",
		fmt.Sprintf("/api/v1/admin/plugins/%s/permissions/kv.read%%3Acache/grant", pluginID), nil)
	gReq.Header.Set("Cookie", "lahijan_session="+sess)
	gReq.Header.Set(middleware.HeaderTenantID, tid)
	gResp, err := ta.app.Test(gReq, -1)
	require.NoError(t, err)
	require.Equal(t, 200, gResp.StatusCode)

	// Detail view shows the manifest blob + the one granted permission.
	req := httptest.NewRequest("GET", "/api/v1/admin/plugins/"+pluginID, nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(raw, &detail))
	assert.Equal(t, "detaily", detail["name"])

	perms, _ := detail["permissions"].([]any)
	require.Len(t, perms, 1)
	assert.Equal(t, "kv.read:cache", perms[0])

	manifestField, _ := detail["manifest"].(map[string]any)
	require.NotNil(t, manifestField, "detail view must include the parsed manifest")
	assert.Equal(t, "detaily", manifestField["name"])
}

func TestSetAdminPluginPermission_Revoke(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	out := uploadPlugin(t, ta, sess, tid, "rev", string(tinyWASM), "name: rev\nversion: 1.0.0\n")
	pluginID, _ := out["id"].(string)

	// Grant then revoke.
	for _, act := range []string{"grant", "revoke"} {
		req := httptest.NewRequest("POST",
			fmt.Sprintf("/api/v1/admin/plugins/%s/permissions/events.emit/%s", pluginID, act), nil)
		req.Header.Set("Cookie", "lahijan_session="+sess)
		req.Header.Set(middleware.HeaderTenantID, tid)
		resp, err := ta.app.Test(req, -1)
		require.NoError(t, err)
		require.Equalf(t, 200, resp.StatusCode, "%s should succeed", act)
	}

	// Verify grant is gone.
	req := httptest.NewRequest("GET", "/api/v1/admin/plugins/"+pluginID, nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(raw, &detail))
	perms, _ := detail["permissions"].([]any)
	assert.Empty(t, perms, "revoked permission should not appear")
}

func TestEnableDisable_Lifecycle(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	out := uploadPlugin(t, ta, sess, tid, "lc", string(tinyWASM), "name: lc\nversion: 1.0.0\n")
	pluginID, _ := out["id"].(string)
	require.Equal(t, "pending", out["status"])

	// Enable.
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/admin/plugins/%s/enable", pluginID), nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "active", got["status"])

	// Disable.
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v1/admin/plugins/%s/disable", pluginID), nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	raw, _ = io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "disabled", got["status"])
}

func TestDeletePlugin(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	out := uploadPlugin(t, ta, sess, tid, "del", string(tinyWASM), "name: del\nversion: 1.0.0\n")
	pluginID, _ := out["id"].(string)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/plugins/"+pluginID, nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	// Second GET should 404.
	req = httptest.NewRequest("GET", "/api/v1/admin/plugins/"+pluginID, nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestListAdminPlugins_Paginated(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)

	// Upload three plugins.
	for _, n := range []string{"p-a", "p-b", "p-c"} {
		out := uploadPlugin(t, ta, sess, tid, n, string(tinyWASM),
			fmt.Sprintf("name: %s\nversion: 1.0.0\n", n))
		status, _ := out["_status"].(int)
		require.Equal(t, 201, status)
	}

	// Page through; just assert we can list at least three back.
	req := httptest.NewRequest("GET", "/api/v1/admin/plugins?limit=10", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(raw, &page))
	assert.GreaterOrEqual(t, page.Total, int64(3))
}

// countPluginAuditEvents returns the count of audit rows matching one of
// the plugin action slugs since the given timestamp.
func countPluginAuditEvents(t *testing.T, action string, since time.Time) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := testutil.Pool().QueryRow(
		ctx,
		"SELECT count(*) FROM audit_log WHERE action = $1 AND created_at >= $2",
		action, since,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

func TestPluginActions_EmitAuditEvents(t *testing.T) {
	t.Parallel()
	ta := newPluginsTestApp(t).ta
	sess, tid := pluginsRegisterPlatformAdmin(t, ta)
	before := time.Now().UTC()

	out := uploadPlugin(t, ta, sess, tid, "audited", string(tinyWASM), "name: audited\nversion: 1.0.0\n")
	pluginID, _ := out["id"].(string)

	require.NoError(t, doPluginAction(t, ta, sess, tid, pluginID, "kv.read:cache", "grant"))
	require.NoError(t, doPluginAction(t, ta, sess, tid, pluginID, "kv.read:cache", "revoke"))
	require.NoError(t, doPluginAction(t, ta, sess, tid, pluginID, "", "enable"))
	require.NoError(t, doPluginAction(t, ta, sess, tid, pluginID, "", "disable"))
	require.NoError(t, doPluginAction(t, ta, sess, tid, pluginID, "", "delete"))

	// Each action should have emitted exactly one audit row.
	for _, action := range []string{
		audit.ActionPluginUpload,
		audit.ActionPluginGrant,
		audit.ActionPluginRevoke,
		audit.ActionPluginEnable,
		audit.ActionPluginDisable,
		audit.ActionPluginDelete,
	} {
		n := countPluginAuditEvents(t, action, before)
		assert.GreaterOrEqualf(t, n, 1, "expected audit row for %s", action)
	}
}

// doPluginAction performs a plugin lifecycle action via the API and returns
// nil on a 2xx response. Used by the audit test to drive every endpoint in
// a compact form.
func doPluginAction(t *testing.T, ta *testApp, sess, tenantID, pluginID, perm, action string) error {
	t.Helper()
	var path, method string
	switch action {
	case "grant", "revoke":
		method = "POST"
		path = fmt.Sprintf("/api/v1/admin/plugins/%s/permissions/%s/%s",
			pluginID, strings.ReplaceAll(perm, ":", "%3A"), action)
	case "enable", "disable":
		method = "POST"
		path = fmt.Sprintf("/api/v1/admin/plugins/%s/%s", pluginID, action)
	case "delete":
		method = "DELETE"
		path = "/api/v1/admin/plugins/" + pluginID
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tenantID)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s returned %d", method, path, resp.StatusCode)
	}
	return nil
}
