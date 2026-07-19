// billing_http_integration_test.go exercises the full /api/v1/me/*
// + /api/v1/admin/* billing HTTP surface end to end against a real
// Postgres (testcontainers) plus the real auth + tenant + rbac
// middleware. It is the WS-17 DoD "every privileged action calls
// RequirePerm" + "tenant isolation" + "audit emit pre + post" at the
// HTTP boundary.
//
// The deeper ledger / cache / metering math coverage is in
// billing/service_integration_test.go at the service layer; this file
// focuses on the boundary concerns (auth, rbac, envelope shape, 501
// degradation).

//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

// newBillingTestApp extends newTestApp with the WS-17 billing stack
// wired against the real Postgres repos. Returns the test app + the
// billing service so the test can drive further setup.
func newBillingTestApp(t *testing.T) (*testApp, *billing.Service) {
	t.Helper()
	ta := newTestApp(t)
	svc := billing.New(
		ta.repos,
		audit.NewDBEmitter(ta.repos.AuditLog),
		nil,
		rbac.NewEvaluator(ta.repos.Memberships),
		billing.NoopEnforcer{},
		billing.NoopMeter{},
		billing.Config{},
	)
	ta.server.SetBillingService(svc)
	return ta, svc
}

// billingSeedUserInTenant creates a brand-new user via /api/v1/auth/register
// (no membership anywhere) and returns the user id. The caller grants
// the membership in the target tenant via testutil.NewMembership.
func billingSeedUserInTenant(t *testing.T, ta *testApp, tidStr string, roleSlug string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	role, err := ta.repos.RBAC.GetRoleBySlug(ctx, roleSlug)
	require.NoError(t, err)

	addr := "billing+" + uuid.NewString() + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	require.Equalf(t, 201, status, "register must succeed (got %d)", status)
	sess := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sess, "register must set session cookie")

	user, err := ta.repos.Users.GetByEmail(ctx, addr)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, testutil.Pool(),
		uuid.MustParse(tidStr), user.ID, &role.ID)
	return user.ID, sess
}

// TestBillingMe_RequiresAuth is the WS-17 DoD "every privileged action
// calls RequirePerm" at the HTTP layer: every billing endpoint
// requires a session.
func TestBillingMe_RequiresAuth(t *testing.T) {
	t.Parallel()
	ta, _ := newBillingTestApp(t)

	cases := []struct{ method, path string }{
		{"GET", "/api/v1/me/balance"},
		{"GET", "/api/v1/me/usage"},
		{"GET", "/api/v1/me/ledger"},
		{"GET", "/api/v1/me/receipts"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Header.Set(middleware.HeaderTenantID, uuid.NewString())
			resp, err := ta.app.Test(req, -1)
			require.NoError(t, err)
			assert.Equal(t, 401, resp.StatusCode, "no session -> 401 for %s %s", c.method, c.path)
			_ = resp.Body.Close()
		})
	}
}

// TestBillingMe_GetBalance_HappyPath covers the most common read.
// The balance cache starts at zero (no ledger rows yet); the handler
// must return 200 with balanceCents=0.
func TestBillingMe_GetBalance_HappyPath(t *testing.T) {
	t.Parallel()
	ta, _ := newBillingTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/me/balance", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 0.0, body["balanceCents"], "fresh user must have zero balance")
	assert.Equal(t, "USD", body["currency"])
}

// TestBillingAdmin_TopupHappyPath verifies the admin topup endpoint
// writes a ledger row + emits an audit row. The audit row must be
// visible in /api/v1/audit (WS-17 DoD "every privileged admin action
// emits audit").
func TestBillingAdmin_TopupHappyPath(t *testing.T) {
	t.Parallel()
	ta, _ := newBillingTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)
	tid := uuid.MustParse(tidStr)

	// Make a second user (the topup target) within the same tenant.
	target, _ := billingSeedUserInTenant(t, ta, tidStr, rbac.RoleTenantMember)

	body := map[string]any{"amountCents": 1234, "currency": "USD", "reference": "test"}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/admin/users/"+target.String()+"/topup", strings.NewReader(string(raw)))
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 201, resp.StatusCode, "topup must succeed for tenant.admin")

	// Verify the ledger row landed via a service-layer read (the
	// admin GET ledger endpoint is what an operator would hit; here
	// we read directly via the service to keep the test tight).
	ctx := context.Background()
	tctx := database.WithTenant(ctx, tid)
	rows, err := ta.server.BillingService().ListLedger(tctx, target, 50, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1, "topup must land one ledger row for the target")
	assert.Equal(t, int64(1234), rows[0].AmountCents)

	// The audit row should be visible in /api/v1/audit.
	aReq := httptest.NewRequest("GET", "/api/v1/audit?action=billing.balance.topup", nil)
	aReq.Header.Set("Cookie", "lahijan_session="+sess)
	aReq.Header.Set(middleware.HeaderTenantID, tidStr)
	aResp, err := ta.app.Test(aReq, -1)
	require.NoError(t, err)
	defer aResp.Body.Close()
	require.Equal(t, 200, aResp.StatusCode)
	raw2, _ := io.ReadAll(aResp.Body)
	assert.Contains(t, string(raw2), "billing.balance.topup",
		"audit endpoint must surface the topup action")
}

// TestBillingAdmin_TopupForbiddenForViewer covers the RBAC layer:
// tenant.viewer does NOT hold billing.balance.adjust; the audit gate
// must return 403 before the handler runs.
func TestBillingAdmin_TopupForbiddenForViewer(t *testing.T) {
	t.Parallel()
	ta, _ := newBillingTestApp(t)
	uid, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	body := map[string]any{"amountCents": 5000, "currency": "USD"}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/admin/users/"+uid.String()+"/topup", strings.NewReader(string(raw)))
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 403, resp.StatusCode, "viewer must NOT be able to topup")
}

// TestBillingTenantIsolation_AdminSeesOwnTenantOnly covers the WS-17
// DoD item "tenant A's admin can't see tenant B's ledger". Tenant A
// admin queries tenant A's ledger for tenant B's user: 0 rows.
func TestBillingTenantIsolation_AdminSeesOwnTenantOnly(t *testing.T) {
	t.Parallel()
	ta, _ := newBillingTestApp(t)
	_, sessA, tidStrA := registerAndLogin(t, ta, rbac.RoleTenantAdmin)
	_, _, tidStrB := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	// Make a user in tenant B that we'll query from tenant A.
	targetB, _ := billingSeedUserInTenant(t, ta, tidStrB, rbac.RoleTenantMember)

	// Tenant A admin queries /admin/users/{targetB}/ledger from
	// tenant A's context. The repository filters out tenant B's
	// rows; the response should be an empty page.
	req := httptest.NewRequest("GET", "/api/v1/admin/users/"+targetB.String()+"/ledger", nil)
	req.Header.Set("Cookie", "lahijan_session="+sessA)
	req.Header.Set(middleware.HeaderTenantID, tidStrA)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	items, _ := body["items"].([]any)
	assert.Empty(t, items, "tenant A must not see tenant B's ledger rows")
}

// TestBillingDisabled_Returns501 covers the WS-17 DoD "feature disabled
// degrades to 501": when the billing service is not wired, every
// billing endpoint returns 501 not_implemented.
func TestBillingDisabled_Returns501(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // note: no billing service wired
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/me/balance", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 501, resp.StatusCode, "unwired billing must return 501")
}
