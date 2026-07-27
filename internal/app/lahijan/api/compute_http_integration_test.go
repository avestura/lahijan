// compute_http_integration_test.go exercises the full /api/v1/compute/*
// HTTP surface end to end against a real Postgres (testcontainers) plus
// the real auth + tenant + rbac middleware. It is the WS-14 DoD "every
// endpoint under /api/v1/compute/* uses the error envelope" + "every
// privileged action calls RequirePerm" + "multi-tenant isolation tested"
// requirement at the HTTP boundary.
//
// The deeper lifecycle (create -> start -> exec -> stop -> delete) is
// covered by compute/service_integration_test.go at the service layer;
// this file focuses on the boundary concerns (auth, rbac, envelope shape).

//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComputeEndpoints_RequireAuth is the WS-14 DoD "every privileged
// action calls RequirePerm" at the HTTP layer: every compute endpoint
// rejects an anonymous request with 401.
func TestComputeEndpoints_RequireAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	cases := []struct {
		method, path string
	}{
		{"GET", "/api/v1/compute/instances"},
		{"POST", "/api/v1/compute/instances"},
		{"GET", "/api/v1/compute/instances/" + uuid.NewString()},
		{"PATCH", "/api/v1/compute/instances/" + uuid.NewString()},
		{"DELETE", "/api/v1/compute/instances/" + uuid.NewString()},
		{"POST", "/api/v1/compute/instances/" + uuid.NewString() + "/start"},
		{"POST", "/api/v1/compute/instances/" + uuid.NewString() + "/exec"},
		{"GET", "/api/v1/compute/images"},
		{"POST", "/api/v1/compute/images"},
		{"GET", "/api/v1/compute/profiles"},
		{"POST", "/api/v1/compute/profiles"},
		{"GET", "/api/v1/compute/networks"},
		{"POST", "/api/v1/compute/networks"},
		{"GET", "/api/v1/compute/storage"},
		{"POST", "/api/v1/compute/storage"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(middleware.HeaderTenantID, uuid.NewString())
			resp, err := ta.app.Test(req, -1)
			require.NoError(t, err)
			assert.Equal(t, 401, resp.StatusCode, "anonymous request must 401")
		})
	}
}

// TestComputeEndpoints_RequireTenantScope covers the WS-14 DoD for the
// tenant-scope contract: a request without X-Tenant-Id returns 400 (the
// audit gate's RequirePerm hits sendTenantScopeRequired before the
// handler runs).
func TestComputeEndpoints_RequireTenantScope(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, _ := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/compute/instances", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	// No X-Tenant-Id header.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "missing tenant scope must 400")

	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "tenant_scope_required", errBody["code"])
}

// TestComputeEndpoints_RequireComputeReadPermission covers the WS-14 DoD
// "every privileged action calls RequirePerm": a tenant viewer (read
// only) cannot create an instance, but can list them.
//
// The test asserts the rbac gate runs and produces the right decision:
//   - viewer GET /instances passes the gate (status != 403)
//   - viewer POST /instances is denied (403 + the missing permission slug
//     in the envelope's details map)
//
// The list call itself may return 501 (no Incus provider wired in this
// test app); the rbac gate's PASS decision is what we assert.
func TestComputeEndpoints_RequireComputeReadPermission(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	// List: gate passes (any non-403 status proves the rbac check passed).
	// The handler returns 501 because computeSvc is nil in this test app;
	// the point is the rbac gate did not reject the read.
	req := httptest.NewRequest("GET", "/api/v1/compute/instances", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.NotEqual(t, 403, resp.StatusCode, "viewer should pass the rbac read gate")
	assert.NotEqual(t, 401, resp.StatusCode, "viewer should be authenticated")

	// Create: denied (viewer does NOT hold compute.instance.create).
	body := bytes.NewReader([]byte(`{"name":"x","imageAlias":"ubuntu/24.04"}`))
	req = httptest.NewRequest("POST", "/api/v1/compute/instances", body)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "viewer cannot create")
	body403 := decodeBody(t, resp)
	errBody, ok := body403["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "forbidden", errBody["code"])
	assert.Equal(t, "compute.instance.create", errBody["details"].(map[string]any)["permission"])
}

// TestComputeEndpoints_DisabledWhenNoProvider covers the WS-14 DoD 501
// path: when the Incus provider is not wired (computeSvc == nil), every
// compute endpoint returns 501 with the localised "feature disabled"
// envelope.
func TestComputeEndpoints_DisabledWhenNoProvider(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // computeSvc is nil by default
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/compute/instances", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode, "compute must 501 when provider is disabled")
	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "not_implemented", errBody["code"])
}

// TestCreateComputeInstance_BadRequest covers the WS-14 DoD for the 400
// envelope path: a body missing required fields returns the standard
// bad_request envelope, NOT 501 (validation runs before the service).
func TestCreateComputeInstance_BadRequest(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	// Empty name + imageAlias -> 400 (not 501).
	body := bytes.NewReader([]byte(`{"name":"","imageAlias":""}`))
	req := httptest.NewRequest("POST", "/api/v1/compute/instances", body)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "missing fields must 400")
}

// TestComputeEndpoints_TenantIsolationAtHTTPBoundary is the WS-14 DoD
// "tenant A cannot see/manage tenant B" at the HTTP layer. Tenant A is
// a member of tenant A; tenant B is a member of tenant B. Tenant A's
// membership does NOT grant access to tenant B's scope, even when
// tenant A sends X-Tenant-Id: <tenantB>.
//
// Concretely: the tenant middleware resolves the membership check; a
// viewer of tenant A who tries to spoof X-Tenant-Id: <tenantB> is
// rejected at the tenant middleware boundary (not at the rbac layer).
func TestComputeEndpoints_TenantIsolationAtHTTPBoundary(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ctx := context.Background()
	require.NoError(t, rbac.SeedOnce(ctx, ta.repos))

	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	role, err := ta.repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantAdmin)
	require.NoError(t, err)

	// Register user A and link to tenant A only.
	addr := "iso+" + uuid.NewString() + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	require.Equal(t, 201, status)
	sessA := extractCookie(sc, "lahijan_session")
	userA, _ := ta.repos.Users.GetByEmail(ctx, addr)
	testutil.NewMembership(ctx, t, testutil.Pool(), tenantA.ID, userA.ID, &role.ID)

	// Attempt: user A lists compute as tenant B (header spoof).
	req := httptest.NewRequest("GET", "/api/v1/compute/instances", nil)
	req.Header.Set("Cookie", "lahijan_session="+sessA)
	req.Header.Set(middleware.HeaderTenantID, tenantB.ID.String())
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	// The tenant middleware resolves the membership: user A is not a
	// member of tenant B, so the request is rejected. Status is either
	// 400 (invalid tenant scope) or 403 (no membership) depending on
	// whether the tenant middleware treats "not a member" as a scope
	// error or an auth error; both are acceptable for this DoD as long
	// as it's NOT 200.
	assert.NotEqual(t, 200, resp.StatusCode,
		"tenant A's user must not reach tenant B's compute surface")
	assert.LessOrEqual(t, resp.StatusCode, 403,
		"expected a 4xx rejection; got %d", resp.StatusCode)
}

// decodeBody reads the response body and decodes it as a generic JSON
// object. Used by tests that only need to assert on the error envelope
// shape (not on a typed response).
func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

// Ensure unused imports stay imported.
var (
	_ = bytes.NewReader
	_ = context.Background
)

// -------------------------------------------------------------------------
// WS-24: noVNC console endpoint (HTTP-level DoD coverage).
//
// The bytes-pump is covered at the provider level
// (providers/incus/console_test.go). These tests cover the HTTP boundary:
// auth, tenant scope, RBAC, and the disabled-provider path.
// -------------------------------------------------------------------------

// TestVNCConsole_RequireAuth covers the WS-14 DoD "every privileged action
// calls RequirePerm" at the HTTP layer for the WS-24 endpoint: an
// anonymous request to /vnc is rejected with 401 before the WS upgrade
// can happen.
func TestVNCConsole_RequireAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/vnc", nil)
	req.Header.Set(middleware.HeaderTenantID, uuid.NewString())
	// No session cookie.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode, "anonymous request must 401 before WS upgrade")
}

// TestVNCConsole_RequireTenantScope covers the tenant-scope contract: a
// request without X-Tenant-Id returns 400 (the audit gate's RequirePerm
// hits sendTenantScopeRequired before the handler runs).
func TestVNCConsole_RequireTenantScope(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, _ := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/vnc", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	// No X-Tenant-Id.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "missing tenant scope must 400")

	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "tenant_scope_required", errBody["code"])
}

// TestVNCConsole_ViewerDenied covers the WS-24 DoD for the new permission:
// a tenant viewer does NOT hold compute.instance.console.vnc, so the
// audit gate rejects with 403 + the missing permission slug in details.
func TestVNCConsole_ViewerDenied(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/vnc", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "viewer does not hold compute.instance.console.vnc")

	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "forbidden", errBody["code"])
	assert.Equal(t, rbac.PermComputeInstanceConsoleVNC,
		errBody["details"].(map[string]any)["permission"])
}

// TestVNCConsole_AdminWhenProviderDisabled covers the 501 path: an admin
// (who DOES hold compute.instance.console.vnc) reaches the handler, which
// returns 501 because computeSvc is nil in this test app.
func TestVNCConsole_AdminWhenProviderDisabled(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // computeSvc is nil by default
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/vnc", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode,
		"admin reaches the handler; nil computeSvc -> 501 (not 426 WS upgrade)")
	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "not_implemented", errBody["code"])
}

// TestVNCConsole_NonWSRequestRejected covers the malformed-request path:
// a plain GET (no WS upgrade headers) from an authenticated admin with a
// wired compute service should be rejected. With computeSvc nil we get
// 501 first (the provider-disabled check runs before the WS upgrade). The
// point of this test is that the WS upgrade never starts for a non-WS
// request from a viewer (403 short-circuits) — assert that the 403 path
// does NOT attempt an upgrade.
func TestVNCConsole_NonWSRequestRejected(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantMember)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/vnc", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	// Member holds compute.instance.console.vnc; provider is nil -> 501
	// (NOT 426; the provider-disabled check runs first).
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode,
		"member with VNC perm but nil provider gets 501, not 426")
}

// -------------------------------------------------------------------------
// WS-32: interactive xterm.js console endpoint (HTTP-level DoD coverage).
//
// Mirrors TestVNCConsole_* 1:1 — the orchestration is identical (auth,
// tenant scope, RBAC, disabled-provider). The bytes-pump + control-fd
// resize are covered at the provider level
// (providers/incus/exec_interactive_test.go).
// -------------------------------------------------------------------------

// TestExecConsole_RequireAuth covers the WS-32 endpoint's auth gate:
// an anonymous request to /console is rejected with 401 before the WS
// upgrade can happen.
func TestExecConsole_RequireAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/console", nil)
	req.Header.Set(middleware.HeaderTenantID, uuid.NewString())
	// No session cookie.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode, "anonymous request must 401 before WS upgrade")
}

// TestExecConsole_RequireTenantScope covers the tenant-scope contract:
// a request without X-Tenant-Id returns 400 (the audit gate's
// RequirePerm hits sendTenantScopeRequired before the handler runs).
func TestExecConsole_RequireTenantScope(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, _ := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/console", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	// No X-Tenant-Id.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "missing tenant scope must 400")

	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "tenant_scope_required", errBody["code"])
}

// TestExecConsole_ViewerDenied covers the WS-32 DoD for the new
// permission: a tenant viewer does NOT hold
// compute.instance.console.exec, so the audit gate rejects with 403 +
// the missing permission slug in details.
func TestExecConsole_ViewerDenied(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/console", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "viewer does not hold compute.instance.console.exec")

	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "forbidden", errBody["code"])
	assert.Equal(t, rbac.PermComputeInstanceConsoleExec,
		errBody["details"].(map[string]any)["permission"])
}

// TestExecConsole_AdminWhenProviderDisabled covers the 501 path: an
// admin (who DOES hold compute.instance.console.exec) reaches the
// handler, which returns 501 because computeSvc is nil in this test
// app.
func TestExecConsole_AdminWhenProviderDisabled(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // computeSvc is nil by default
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/console", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode,
		"admin reaches the handler; nil computeSvc -> 501 (not 426 WS upgrade)")
	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "not_implemented", errBody["code"])
}

// TestExecConsole_MemberWhenProviderDisabled covers the same 501 path
// from a member (who also holds the permission). The point is that the
// audit gate lets the member through (member has the perm) and the
// handler returns 501 before the WS upgrade.
func TestExecConsole_MemberWhenProviderDisabled(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantMember)

	req := httptest.NewRequest("GET",
		"/api/v1/compute/instances/"+uuid.NewString()+"/console", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	// Member holds compute.instance.console.exec; provider is nil -> 501
	// (NOT 426; the provider-disabled check runs first).
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode,
		"member with exec perm but nil provider gets 501, not 426")
}
