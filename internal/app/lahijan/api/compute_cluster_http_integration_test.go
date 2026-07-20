// compute_cluster_http_integration_test.go exercises the WS-26
// /api/v1/compute/cluster/* + /api/v1/compute/instances/{id}/migrate
// HTTP surface. Mirrors the WS-14 + WS-25 integration tests: every
// privileged endpoint rejects an anonymous request with 401; every
// privileged endpoint rejects an under-permissioned request with 403;
// the disabled-compute path returns 501.

//go:build integration

package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComputeClusterEndpoints_RequireAuth is the WS-26 DoD "every
// privileged action calls RequirePerm" at the HTTP layer: every
// cluster endpoint rejects an anonymous request with 401.
func TestComputeClusterEndpoints_RequireAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	cases := []struct{ method, path string }{
		{"GET", "/api/v1/compute/cluster/members"},
		{"GET", "/api/v1/compute/cluster/members/node-1"},
		{"POST", "/api/v1/compute/cluster/members/node-1/evacuate"},
		{"POST", "/api/v1/compute/cluster/members/node-1/restore"},
		{"POST", "/api/v1/compute/instances/" + uuid.NewString() + "/migrate"},
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

// TestComputeClusterEndpoints_ViewerCanListMembersButCannotMutate covers
// the WS-26 permission split: a tenant.viewer can list members + read
// one member (so the cluster status panel renders), but cannot
// evacuate/restore a member or migrate an instance.
func TestComputeClusterEndpoints_ViewerCanListMembersButCannotMutate(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tenantID := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	// Reads: pass the rbac gate, hit the handler (returns 501 because
	// computeSvc is nil in this test app — the point is that the rbac
	// gate did not deny).
	readCases := []struct{ method, path string }{
		{"GET", "/api/v1/compute/cluster/members"},
		{"GET", "/api/v1/compute/cluster/members/node-1"},
	}
	for _, tc := range readCases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Cookie", "lahijan_session="+sess)
			req.Header.Set(middleware.HeaderTenantID, tenantID)
			resp, err := ta.app.Test(req, -1)
			require.NoError(t, err)
			assert.NotEqual(t, 403, resp.StatusCode,
				"viewer must pass the rbac gate on cluster reads; got %d", resp.StatusCode)
		})
	}

	// Mutations: viewer does NOT hold evacuate/migrate perms.
	mutCases := []struct{ method, path string }{
		{"POST", "/api/v1/compute/cluster/members/node-1/evacuate"},
		{"POST", "/api/v1/compute/cluster/members/node-1/restore"},
		{"POST", "/api/v1/compute/instances/" + uuid.NewString() + "/migrate"},
	}
	for _, tc := range mutCases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Cookie", "lahijan_session="+sess)
			req.Header.Set(middleware.HeaderTenantID, tenantID)
			resp, err := ta.app.Test(req, -1)
			require.NoError(t, err)
			assert.Equal(t, 403, resp.StatusCode,
				"viewer must be denied on cluster mutations")
		})
	}
}

// TestComputeClusterEndpoints_DisabledComputeIs501 covers the disabled
// path: when the compute module is not wired (the typical test
// configuration) every cluster endpoint returns 501 not_implemented
// after the rbac gate passes.
func TestComputeClusterEndpoints_DisabledComputeIs501(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // computeSvc is nil by default
	_, sess, tenantID := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/compute/cluster/members", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tenantID)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode,
		"compute must 501 when provider is disabled")
}

// TestComputeInstanceMigrate_BadRequestBody covers the bad-request
// path: an admin reaching the migrate handler with a body missing the
// required targetMember gets 400 (the rbac gate passed; the body
// parser rejected).
func TestComputeInstanceMigrate_BadRequestBody(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, tenantID := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("POST",
		"/api/v1/compute/instances/"+uuid.NewString()+"/migrate", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tenantID)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"missing body must 400")
}
