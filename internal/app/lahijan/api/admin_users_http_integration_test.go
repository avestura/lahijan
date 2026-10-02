// admin_users_http_integration_test.go exercises the platform-admin user
// management and directory-connection HTTP surface end to end against a real
// Postgres: permission gating, user create / search / update / role change /
// delete, the permission catalogue, personal-access-token scope enforcement,
// and the directory connection CRUD + test endpoints.

//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
	"github.com/avestura/lahijan/internal/app/lahijan/directory"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
)

// newAdminTestApp is newTestApp plus the dependencies the admin user and
// directory handlers need (memberships, roles, the directory service).
func newAdminTestApp(t *testing.T) *testApp {
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
	patSvc := pat.New(repos.Tokens, signer, audit.NoopEmitter{}, pat.Config{Prefix: "lah_pat_", ByteLen: 32}).
		WithUserChecker(func(ctx context.Context, id uuid.UUID) bool {
			u, err := repos.Users.GetByID(ctx, id)
			return err == nil && u.IsActive && u.DeletedAt == nil
		})
	crypto, err := secrets.NewCrypto([]byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	emitter := audit.NewDBEmitter(repos.AuditLog)

	server := api.NewServer(api.ServerDeps{
		Users:        repos.Users,
		Sessions:     repos.Sessions,
		SessionSvc:   sessionSvc,
		PATSvc:       patSvc,
		EmailSvc:     mailer,
		Signer:       signer,
		Cookies:      cookies,
		Memberships:  repos.Memberships,
		RBAC:         repos.RBAC,
		Audit:        repos.AuditLog,
		AuditEmitter: emitter,
		DirectorySvc: directory.New(repos, crypto, emitter, nil),
	})
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(repos.Memberships))
	app := fiber.New()
	middleware.Apply(app, middleware.Options{
		Tenant: middleware.TenantWithResolver(middleware.TenantResolver{Tenants: repos.Tenants}),
		Auth: middleware.AuthWithResolver(middleware.AuthResolver{
			Cookies:      middleware.CookieConfig{SessionName: cookies.SessionName, RefreshName: cookies.RefreshName},
			Signer:       signer,
			SessionsRepo: repos.Sessions,
			PATService:   patSvc,
		}),
	})
	api.RegisterRoutes(app, server, policy)
	return &testApp{app: app, cookies: cookies, repos: repos, server: server}
}

// adminCall issues a request as the session user in the given tenant and
// returns the status and decoded JSON body.
func adminCall(t *testing.T, ta *testApp, method, path, sess, tenant string, body any) (int, map[string]any) {
	t.Helper()
	return adminCallAuth(t, ta, method, path, "lahijan_session="+sess, "", tenant, body)
}

func adminCallAuth(t *testing.T, ta *testApp, method, path, cookie, bearer, tenant string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if tenant != "" {
		req.Header.Set(middleware.HeaderTenantID, tenant)
	}
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestAdminUsers_RequirePlatformAdmin(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)
	_, sess, tid := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	for _, path := range []string{"/api/v1/admin/users", "/api/v1/admin/directory/connections"} {
		status, _ := adminCall(t, ta, "GET", path, sess, tid, nil)
		assert.Equalf(t, 403, status, "a tenant admin must not reach %s", path)
	}
	status, _ := adminCall(t, ta, "POST", "/api/v1/admin/users", sess, tid, map[string]any{"email": "x@example.test"})
	assert.Equal(t, 403, status)
}

func TestAdminUsers_Lifecycle(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)
	adminID, sess, tid := registerAndLogin(t, ta, rbac.RolePlatformAdmin)

	// Create (no password) -> 201, verified, no password.
	addr := "managed+" + uuid.NewString()[:10] + "@example.test"
	status, created := adminCall(t, ta, "POST", "/api/v1/admin/users", sess, tid,
		map[string]any{"email": addr, "displayName": "Managed User", "locale": "fa"})
	require.Equal(t, 201, status, "body=%v", created)
	assert.Equal(t, addr, created["email"])
	assert.Equal(t, true, created["emailVerified"])
	assert.Equal(t, false, created["hasPassword"])
	assert.Equal(t, "fa", created["locale"])
	userID, _ := created["id"].(string)
	require.NotEmpty(t, userID)

	// Duplicate email -> 409.
	status, _ = adminCall(t, ta, "POST", "/api/v1/admin/users", sess, tid, map[string]any{"email": addr})
	assert.Equal(t, 409, status)
	// Weak password -> 400.
	status, _ = adminCall(t, ta, "POST", "/api/v1/admin/users", sess, tid,
		map[string]any{"email": "weak+" + uuid.NewString()[:8] + "@example.test", "password": "short"})
	assert.Equal(t, 400, status)

	// Search finds exactly the new user.
	status, page := adminCall(t, ta, "GET", "/api/v1/admin/users?q="+url.QueryEscape(addr), sess, tid, nil)
	require.Equal(t, 200, status)
	assert.EqualValues(t, 1, page["total"])
	items, _ := page["items"].([]any)
	require.Len(t, items, 1)
	// memberships is always an array (empty here: this test app has no signup
	// provisioner), never null.
	first, _ := items[0].(map[string]any)
	memberships, isArray := first["memberships"].([]any)
	assert.True(t, isArray, "memberships must serialize as an array")
	assert.Empty(t, memberships)

	// Pagination bounds are honoured.
	status, page = adminCall(t, ta, "GET", "/api/v1/admin/users?limit=1&offset=0", sess, tid, nil)
	require.Equal(t, 200, status)
	assert.Len(t, page["items"], 1)
	assert.EqualValues(t, 1, page["limit"])

	// Disable the user.
	status, updated := adminCall(t, ta, "PATCH", "/api/v1/admin/users/"+userID, sess, tid, map[string]any{"isActive": false})
	require.Equal(t, 200, status)
	assert.Equal(t, false, updated["isActive"])

	// An admin cannot disable or delete themselves.
	status, _ = adminCall(t, ta, "PATCH", "/api/v1/admin/users/"+adminID.String(), sess, tid, map[string]any{"isActive": false})
	assert.Equal(t, 409, status)
	status, _ = adminCall(t, ta, "DELETE", "/api/v1/admin/users/"+adminID.String(), sess, tid, nil)
	assert.Equal(t, 409, status)

	// Delete -> 204, then 404 on read, and gone from the list.
	status, _ = adminCall(t, ta, "DELETE", "/api/v1/admin/users/"+userID, sess, tid, nil)
	assert.Equal(t, 204, status)
	status, _ = adminCall(t, ta, "GET", "/api/v1/admin/users/"+userID, sess, tid, nil)
	assert.Equal(t, 404, status)
	status, page = adminCall(t, ta, "GET", "/api/v1/admin/users?q="+url.QueryEscape(addr), sess, tid, nil)
	require.Equal(t, 200, status)
	assert.EqualValues(t, 0, page["total"])
}

func TestAdminUsers_ChangeRole(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)
	_, adminSess, adminTid := registerAndLogin(t, ta, rbac.RolePlatformAdmin)
	memberID, _, memberTid := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	status, out := adminCall(t, ta, "PUT",
		"/api/v1/admin/users/"+memberID.String()+"/memberships/"+memberTid, adminSess, adminTid,
		map[string]any{"role": rbac.RoleTenantMember})
	require.Equal(t, 200, status, "body=%v", out)
	var found bool
	ms, _ := out["memberships"].([]any)
	for _, m := range ms {
		mm, _ := m.(map[string]any)
		if mm["tenantId"] == memberTid {
			found = true
			assert.Equal(t, rbac.RoleTenantMember, mm["role"])
		}
	}
	assert.True(t, found, "the membership must be in the response")

	// Unknown role -> 400; user not in tenant -> 404.
	status, _ = adminCall(t, ta, "PUT", "/api/v1/admin/users/"+memberID.String()+"/memberships/"+memberTid, adminSess, adminTid,
		map[string]any{"role": "no.such.role"})
	assert.Equal(t, 400, status)
	status, _ = adminCall(t, ta, "PUT", "/api/v1/admin/users/"+memberID.String()+"/memberships/"+uuid.NewString(), adminSess, adminTid,
		map[string]any{"role": rbac.RoleTenantMember})
	assert.Equal(t, 404, status)
}

func TestPermissionCatalogue(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)

	status, _ := adminCallAuth(t, ta, "GET", "/api/v1/permissions", "", "", "", nil)
	assert.Equal(t, 401, status, "anonymous callers cannot read the catalogue")

	_, sess, tid := registerAndLogin(t, ta, rbac.RoleTenantViewer)
	status, out := adminCall(t, ta, "GET", "/api/v1/permissions", sess, tid, nil)
	require.Equal(t, 200, status)
	items, _ := out["items"].([]any)
	require.NotEmpty(t, items)
	slugs := make([]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		s, _ := m["slug"].(string)
		slugs = append(slugs, s)
	}
	assert.Contains(t, slugs, rbac.PermComputeInstanceStart)
	assert.IsIncreasing(t, slugs, "catalogue is sorted by slug")
}

func TestPAT_ScopesLimitPermissions(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)
	_, sess, tid := registerAndLogin(t, ta, rbac.RolePlatformAdmin)

	mint := func(scopes []string) string {
		status, out := adminCall(t, ta, "POST", "/api/v1/auth/personal-access-tokens", sess, tid,
			map[string]any{"name": "t-" + uuid.NewString()[:6], "scopes": scopes})
		require.Equal(t, 201, status, "body=%v", out)
		tok, _ := out["token"].(string)
		require.NotEmpty(t, tok)
		return tok
	}

	narrow := mint([]string{"dns.zone.read"})
	status, _ := adminCallAuth(t, ta, "GET", "/api/v1/admin/users", "", narrow, tid, nil)
	assert.Equal(t, 403, status, "a token scoped to dns.zone.read must not list users")

	wide := mint([]string{rbac.PermPlatformUserList})
	status, _ = adminCallAuth(t, ta, "GET", "/api/v1/admin/users", "", wide, tid, nil)
	assert.Equal(t, 200, status, "the scope that matches the permission is allowed")

	unscoped := mint(nil)
	status, _ = adminCallAuth(t, ta, "GET", "/api/v1/admin/users", "", unscoped, tid, nil)
	assert.Equal(t, 200, status, "a token with no scopes keeps the owner's permissions")
}

func TestDirectoryConnections_API(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)
	_, sess, tid := registerAndLogin(t, ta, rbac.RolePlatformAdmin)

	name := "corp-" + uuid.NewString()[:8]
	body := map[string]any{
		"kind": "ldap", "name": name, "enabled": true, "bindPassword": "s3cret",
		"config": map[string]any{
			// Port 1 refuses immediately, so the test needs no LDAP server.
			"url": "ldap://127.0.0.1:1", "bindDn": "cn=svc,dc=x", "userBaseDn": "ou=people,dc=x",
		},
	}
	status, created := adminCall(t, ta, "POST", "/api/v1/admin/directory/connections", sess, tid, body)
	require.Equal(t, 201, status, "body=%v", created)
	assert.Equal(t, true, created["hasSecret"])
	raw, _ := json.Marshal(created)
	assert.NotContains(t, string(raw), "s3cret", "the bind password must never be returned")
	id, _ := created["id"].(string)

	// Duplicate name -> 409; invalid config -> 400.
	status, _ = adminCall(t, ta, "POST", "/api/v1/admin/directory/connections", sess, tid, body)
	assert.Equal(t, 409, status)
	status, _ = adminCall(t, ta, "POST", "/api/v1/admin/directory/connections", sess, tid, map[string]any{
		"kind": "ldap", "name": "bad-" + uuid.NewString()[:6], "config": map[string]any{"url": "http://nope"},
	})
	assert.Equal(t, 400, status)

	// Test against the unreachable server -> 200 with ok=false.
	status, res := adminCall(t, ta, "POST", "/api/v1/admin/directory/test", sess, tid, map[string]any{"connectionId": id})
	require.Equal(t, 200, status)
	assert.Equal(t, false, res["ok"])
	assert.Equal(t, "connect_failed", res["code"])

	// Sync of an unreachable server -> 502, and the failure is recorded.
	status, _ = adminCall(t, ta, "POST", "/api/v1/admin/directory/connections/"+id+"/sync", sess, tid, nil)
	assert.Equal(t, 502, status)
	status, got := adminCall(t, ta, "GET", "/api/v1/admin/directory/connections/"+id, sess, tid, nil)
	require.Equal(t, 200, status)
	assert.Equal(t, "error", got["lastSyncStatus"])

	// A SAML connection cannot be synced.
	status, saml := adminCall(t, ta, "POST", "/api/v1/admin/directory/connections", sess, tid, map[string]any{
		"kind": "saml", "name": "sso-" + uuid.NewString()[:6],
		"config": map[string]any{"idpMetadataUrl": "https://idp.example.com/metadata"},
	})
	require.Equal(t, 201, status, "body=%v", saml)
	samlID, _ := saml["id"].(string)
	status, _ = adminCall(t, ta, "POST", "/api/v1/admin/directory/connections/"+samlID+"/sync", sess, tid, nil)
	assert.Equal(t, 409, status)

	// Delete -> 204 then 404.
	status, _ = adminCall(t, ta, "DELETE", "/api/v1/admin/directory/connections/"+id, sess, tid, nil)
	assert.Equal(t, 204, status)
	status, _ = adminCall(t, ta, "GET", "/api/v1/admin/directory/connections/"+id, sess, tid, nil)
	assert.Equal(t, 404, status)
}

func TestDisablingAUserStopsTheirTokensAndSessions(t *testing.T) {
	t.Parallel()
	ta := newAdminTestApp(t)
	_, adminSess, adminTid := registerAndLogin(t, ta, rbac.RolePlatformAdmin)
	victimID, victimSess, victimTid := registerAndLogin(t, ta, rbac.RoleTenantMember)

	status, out := adminCall(t, ta, "POST", "/api/v1/auth/personal-access-tokens", victimSess, victimTid,
		map[string]any{"name": "victim-token"})
	require.Equal(t, 201, status, "body=%v", out)
	token, _ := out["token"].(string)
	require.NotEmpty(t, token)

	status, _ = adminCallAuth(t, ta, "GET", "/api/v1/auth/me", "", token, victimTid, nil)
	require.Equal(t, 200, status, "the token works while the user is active")

	status, _ = adminCall(t, ta, "PATCH", "/api/v1/admin/users/"+victimID.String(), adminSess, adminTid,
		map[string]any{"isActive": false})
	require.Equal(t, 200, status)

	status, _ = adminCallAuth(t, ta, "GET", "/api/v1/auth/me", "", token, victimTid, nil)
	assert.Equal(t, 401, status, "a disabled user's token must stop working")
	status, _ = adminCall(t, ta, "GET", "/api/v1/auth/me", victimSess, victimTid, nil)
	assert.Equal(t, 401, status, "a disabled user's session must stop working")
}
