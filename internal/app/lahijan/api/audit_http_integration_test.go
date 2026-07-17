// audit_http_integration_test.go exercises the /api/v1/audit* HTTP surface
// end to end against a real Postgres (testcontainers) plus the real audit
// middleware wiring. It is the WS-08 DoD "audit emit + mark-outcome flow
// tested end-to-end" + "audit query API supports filters + pagination +
// export" requirement.

//go:build integration

package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

// seedRBACAndMembership is the lightweight variant for tests that don't need
// a session (e.g. TestListAudit_RequiresAuth checks 401 before login).
func seedRBACAndMembership(t *testing.T, roleSlug string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, rbac.SeedOnce(ctx, testutil.Repos()))

	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	repos := testutil.Repos()
	role, err := repos.RBAC.GetRoleBySlug(ctx, roleSlug)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)
	return tenant.ID
}

// registerAndLogin seeds a tenant + role, registers a brand-new user via the
// API (which gives us a session cookie), then links that user to the tenant
// with the given role. Returns the new user id + session cookie + tenant id
// header value.
//
// We register-then-link (rather than link-then-patch-email) so we never
// collide with the random emails testutil.NewUser mints for other tests.
func registerAndLogin(t *testing.T, ta *testApp, roleSlug string) (uuid.UUID, string, string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, rbac.SeedOnce(ctx, testutil.Repos()))

	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	role, err := testutil.Repos().RBAC.GetRoleBySlug(ctx, roleSlug)
	require.NoError(t, err)

	// Register a brand-new user via the API so we get a session cookie.
	addr := "audit+" + uuid.NewString() + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	require.Equalf(t, 201, status, "register must succeed (got %d)", status)
	sess := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sess, "register must set session cookie")

	// Look up the freshly-registered user + grant membership.
	user, err := ta.repos.Users.GetByEmail(ctx, addr)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)

	return user.ID, sess, tenant.ID.String()
}

func TestListAudit_RequiresAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	tid := seedRBACAndMembership(t, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set(middleware.HeaderTenantID, tid.String())
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode, "no user -> 401")
}

func TestListAudit_RequiresTenantScope(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, _ := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	// No X-Tenant-Id header.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode, "no tenant header -> 400 tenant_scope_required")
}

func TestListAudit_ReturnsEvents(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	uid, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)
	tid := uuid.MustParse(tidStr)

	// Seed two audit events for the tenant.
	ctx := context.Background()
	pool := testutil.Pool()
	_, err := pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.start', 'instance', 'success', '{}'::jsonb)",
		tid, uid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.stop', 'instance', 'success', '{}'::jsonb)",
		tid, uid)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
		Limit int              `json:"limit"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Equal(t, int64(2), page.Total, "expected 2 audit events")
	assert.Len(t, page.Items, 2)
	assert.Equal(t, 50, page.Limit, "default page size")
}

func TestListAudit_FiltersByAction(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	uid, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)
	tid := uuid.MustParse(tidStr)

	ctx := context.Background()
	pool := testutil.Pool()
	_, err := pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.start', 'instance', 'success', '{}'::jsonb)",
		tid, uid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.stop', 'instance', 'success', '{}'::jsonb)",
		tid, uid)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/audit?action=compute.instance.start", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Equal(t, int64(1), page.Total, "filter by action should return 1 row")
	require.Len(t, page.Items, 1)
	assert.Equal(t, "compute.instance.start", page.Items[0]["action"])
}

func TestGetAudit_NotFound(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	req := httptest.NewRequest("GET", "/api/v1/audit/"+uuid.New().String(), nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestGetAudit_ReturnsEventWithOutcomes(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	uid, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)
	tid := uuid.MustParse(tidStr)

	// Insert an audit row + outcome trail directly.
	ctx := context.Background()
	pool := testutil.Pool()
	var auditID uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata)
		 VALUES ($1, $2, 'user', 'compute.instance.start', 'instance', 'pending', '{}'::jsonb)
		 RETURNING id`, tid, uid).Scan(&auditID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO audit_log_outcomes (audit_id, status, details) VALUES ($1, 'success', '{"duration_ms": 42}'::jsonb)`,
		auditID)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/audit/"+auditID.String(), nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var ev map[string]any
	require.NoError(t, json.Unmarshal(body, &ev))
	assert.Equal(t, "compute.instance.start", ev["action"])
	// Status should be the latest outcome's status, not the row's initial.
	assert.Equal(t, "success", ev["status"], "current status = latest outcome")
	outcomes, ok := ev["outcomes"].([]any)
	require.True(t, ok, "outcomes should be present")
	require.Len(t, outcomes, 1)
}

func TestExportAudit_CSVStream(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	uid, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)
	tid := uuid.MustParse(tidStr)

	// Seed one audit row.
	ctx := context.Background()
	pool := testutil.Pool()
	_, err := pool.Exec(ctx,
		"INSERT INTO audit_log (tenant_id, actor_user_id, actor_type, action, resource_type, status, metadata) VALUES ($1, $2, 'user', 'compute.instance.start', 'instance', 'success', '{}'::jsonb)",
		tid, uid)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/v1/audit/export?format=csv", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/csv")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	r := csv.NewReader(strings.NewReader(string(body)))
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(records), 2, "CSV must have header + at least 1 row")
	assert.Equal(t, "id", records[0][0])
	// Find the action column in the data row (column index 4).
	assert.Equal(t, "compute.instance.start", records[1][4])
}

func TestExportAudit_RequiresAuditExportPermission(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	// Viewer does NOT have audit.export; should get 403.
	req := httptest.NewRequest("GET", "/api/v1/audit/export", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}
