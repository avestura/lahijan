// dns_http_integration_test.go exercises the full /api/v1/dns/* HTTP
// surface end to end against a real Postgres (testcontainers) plus the
// real auth + tenant + rbac middleware. It is the WS-15 DoD "every
// endpoint under /api/v1/dns/* uses the error envelope" + "every
// privileged action calls RequirePerm" + "tenant isolation tested"
// requirement at the HTTP boundary.
//
// The deeper zone + record CRUD coverage (audit + bus assertions) is in
// dns/service_integration_test.go at the service layer; this file
// focuses on the boundary concerns (auth, rbac, envelope shape, 501
// degradation).

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
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns/fake"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDNSTestApp extends newTestApp with the WS-15 DNS stack wired against
// a fresh in-memory fake PDNS server. Mirrors newComputeTestApp's
// pattern. Returns the test app + the fake PDNS server so the test can
// drive state (e.g. assert a zone was created) if it needs to.
//
// The handler closures capture the *api.Server pointer at registration
// time and read s.dnsSvc at request time, so mutating ta.server.dnsSvc
// here takes effect for every subsequent request without re-registering
// routes.
func newDNSTestApp(t *testing.T) (*testApp, *fake.Server) {
	t.Helper()
	ta := newTestApp(t)
	pdnsSrv := fake.NewServer(t)
	provider, err := powerdns.NewClient(powerdns.Config{
		HTTPClient: http.DefaultClient,
		BaseURL:    pdnsSrv.HTTP.URL,
		APIKey:     pdnsSrv.APIKey,
	})
	require.NoError(t, err)
	dnsSvc := dns.New(
		provider,
		ta.repos,
		audit.NewDBEmitter(ta.repos.AuditLog),
		nil,
		rbac.NewEvaluator(ta.repos.Memberships),
		dns.Config{DefaultNameservers: []string{"ns1.example.net."}},
	)
	ta.server.SetDNSService(dnsSvc)
	return ta, pdnsSrv
}

// TestDNSEndpoints_RequireAuth is the WS-15 DoD "every privileged action
// calls RequirePerm" at the HTTP layer: every DNS endpoint rejects an
// anonymous request with 401.
func TestDNSEndpoints_RequireAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	zoneID := uuid.NewString()
	recordID := uuid.NewString()
	cases := []struct {
		method, path string
	}{
		{"GET", "/api/v1/dns/zones"},
		{"POST", "/api/v1/dns/zones"},
		{"GET", "/api/v1/dns/zones/" + zoneID},
		{"PATCH", "/api/v1/dns/zones/" + zoneID},
		{"DELETE", "/api/v1/dns/zones/" + zoneID},
		{"GET", "/api/v1/dns/zones/" + zoneID + "/records"},
		{"POST", "/api/v1/dns/zones/" + zoneID + "/records"},
		{"GET", "/api/v1/dns/zones/" + zoneID + "/records/" + recordID},
		{"PATCH", "/api/v1/dns/zones/" + zoneID + "/records/" + recordID},
		{"DELETE", "/api/v1/dns/zones/" + zoneID + "/records/" + recordID},
		{"POST", "/api/v1/dns/zones/" + zoneID + "/dnssec/enable"},
		{"POST", "/api/v1/dns/zones/" + zoneID + "/dnssec/disable"},
		{"GET", "/api/v1/dns/templates"},
		{"POST", "/api/v1/dns/zones/" + zoneID + "/apply-template"},
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

// TestDNSEndpoints_RequireTenantScope covers the WS-15 DoD for the
// tenant-scope contract: a request without X-Tenant-Id returns 400.
func TestDNSEndpoints_RequireTenantScope(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, _ := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/dns/zones", nil)
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

// TestDNSEndpoints_RequireDNSReadPermission covers the WS-15 DoD
// "every privileged action calls RequirePerm": a tenant viewer (read
// only) cannot create a zone, but can list zones + browse templates.
func TestDNSEndpoints_RequireDNSReadPermission(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	// List zones: gate passes (any non-403 status proves the rbac check
	// passed). The handler returns 501 because dnsSvc is nil in this
	// test app; the point is the rbac gate did not reject the read.
	req := httptest.NewRequest("GET", "/api/v1/dns/zones", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.NotEqual(t, 403, resp.StatusCode, "viewer should pass the rbac read gate")
	assert.NotEqual(t, 401, resp.StatusCode, "viewer should be authenticated")

	// Templates list: gate passes (read).
	req = httptest.NewRequest("GET", "/api/v1/dns/templates", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.NotEqual(t, 403, resp.StatusCode, "viewer should pass templates read gate")

	// Create zone: denied (viewer does NOT hold dns.zone.create).
	body := bytes.NewReader([]byte(`{"name":"example.com."}`))
	req = httptest.NewRequest("POST", "/api/v1/dns/zones", body)
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
	assert.Equal(t, "dns.zone.create", errBody["details"].(map[string]any)["permission"])
}

// TestDNSEndpoints_DisabledWhenNoProvider covers the WS-15 DoD 501 path:
// when the PowerDNS provider is not wired (dnsSvc == nil), every DNS
// endpoint returns 501 with the localised "feature disabled" envelope.
func TestDNSEndpoints_DisabledWhenNoProvider(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // dnsSvc is nil by default
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/dns/zones", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode, "dns must 501 when provider is disabled")
	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "not_implemented", errBody["code"])
}

// TestCreateDNSZone_BadRequest covers the WS-15 DoD for the 400 envelope
// path: a body missing required fields returns the standard bad_request
// envelope, NOT 501 (validation runs before the service).
func TestCreateDNSZone_BadRequest(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	body := bytes.NewReader([]byte(`{"name":""}`))
	req := httptest.NewRequest("POST", "/api/v1/dns/zones", body)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "missing fields must 400")
}

// TestCreateDNSZone_HappyPath creates a zone via the HTTP surface with
// the PDNS fake wired. Verifies the response shape + that the canonical
// id round-trips.
func TestCreateDNSZone_HappyPath(t *testing.T) {
	t.Parallel()
	ta, _ := newDNSTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	body := bytes.NewReader([]byte(`{"name":"happy.example.com.","description":"WS-15 HTTP happy"}`))
	req := httptest.NewRequest("POST", "/api/v1/dns/zones", body)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	var envelope struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		CanonicalID  string `json:"canonicalId"`
		Kind         string `json:"kind"`
		IsDNSSecEnabled bool `json:"isDnssecEnabled"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
	assert.Equal(t, "happy.example.com.", envelope.Name)
	assert.Equal(t, "happy.example.com.", envelope.CanonicalID)
	assert.Equal(t, "Native", envelope.Kind)
	assert.False(t, envelope.IsDNSSecEnabled)
}

// TestCreateDNSRecord_InvalidContentReturnsEnvelope covers the WS-15 DoD
// "invalid record content rejected with a clear message (per type)": the
// 400 response carries the per-type validator message verbatim.
func TestCreateDNSRecord_InvalidContentReturnsEnvelope(t *testing.T) {
	t.Parallel()
	ta, _ := newDNSTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	// Create a zone first.
	body := bytes.NewReader([]byte(`{"name":"invalid.example.com."}`))
	req := httptest.NewRequest("POST", "/api/v1/dns/zones", body)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	var zone struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&zone))

	// Attempt to create an A record with malformed content.
	recBody := bytes.NewReader([]byte(`{"name":"x.invalid.example.com.","type":"A","content":"not-an-ip"}`))
	req = httptest.NewRequest("POST", "/api/v1/dns/zones/"+zone.ID+"/records", recBody)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 400, resp.StatusCode, "invalid A content must 400")
	body400 := decodeBody(t, resp)
	errBody, ok := body400["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "bad_request", errBody["code"])
	// The validator message mentions "IPv4".
	assert.Contains(t, errBody["message"].(string), "IPv4")
}

// TestListDNSTemplates_ReturnsCatalog covers the WS-15 DoD "templates
// apply correctly": the catalog endpoint surfaces the built-in
// templates.
func TestListDNSTemplates_ReturnsCatalog(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET", "/api/v1/dns/templates", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var out struct {
		Items []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Records     []struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				Content string `json:"content"`
			} `json:"records"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Greater(t, len(out.Items), 0, "catalog must have at least one template")
	known := map[string]bool{}
	for _, tpl := range out.Items {
		known[tpl.ID] = true
	}
	assert.True(t, known["google-workspace"], "catalog must include google-workspace")
	assert.True(t, known["microsoft-365"], "catalog must include microsoft-365")
}

// TestDNSEndpoints_TenantIsolationAtHTTPBoundary is the WS-15 DoD
// "tenant A cannot see/manage tenant B" at the HTTP layer. Tenant A is
// a member of tenant A; tenant B is a separate tenant. Tenant A's
// membership does NOT grant access to tenant B's scope.
func TestDNSEndpoints_TenantIsolationAtHTTPBoundary(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ctx := context.Background()
	require.NoError(t, rbac.SeedOnce(ctx, testutil.Repos()))

	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	role, err := ta.repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantAdmin)
	require.NoError(t, err)

	// Register user A and link to tenant A only.
	addr := "dns-iso+" + uuid.NewString() + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	require.Equal(t, 201, status)
	sessA := extractCookie(sc, "lahijan_session")
	userA, _ := ta.repos.Users.GetByEmail(ctx, addr)
	testutil.NewMembership(ctx, t, testutil.Pool(), tenantA.ID, userA.ID, &role.ID)

	// User A attempts to list DNS zones as tenant B (header spoof).
	req := httptest.NewRequest("GET", "/api/v1/dns/zones", nil)
	req.Header.Set("Cookie", "lahijan_session="+sessA)
	req.Header.Set(middleware.HeaderTenantID, tenantB.ID.String())
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.NotEqual(t, 200, resp.StatusCode,
		"tenant A's user must not reach tenant B's DNS surface")
	assert.LessOrEqual(t, resp.StatusCode, 403,
		"expected a 4xx rejection; got %d", resp.StatusCode)
}
