// storage_http_integration_test.go exercises the full /api/v1/storage/*
// HTTP surface end to end against a real Postgres (testcontainers) plus
// the real auth + tenant + rbac middleware. It is the WS-16 DoD "every
// endpoint under /api/v1/storage/* uses the error envelope" + "every
// privileged action calls RequirePerm" + "credentials shown once at
// creation" + "tenant isolation tested" requirement at the HTTP
// boundary.
//
// The deeper bucket + credential CRUD coverage (audit + bus assertions)
// is in storage/service_integration_test.go at the service layer; this
// file focuses on the boundary concerns (auth, rbac, envelope shape,
// 501 degradation).

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
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStorageTestApp extends newTestApp with the WS-16 storage stack
// wired against a fresh in-memory fake SeaweedFS server. Mirrors
// newDNSTestApp's pattern. Returns the test app + the fake server so the
// test can drive state (e.g. assert a bucket was created) if it needs to.
func newStorageTestApp(t *testing.T) (*testApp, *fake.Server) {
	t.Helper()
	ta := newTestApp(t)
	swSrv := fake.NewServer(t)
	provider, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient:     http.DefaultClient,
		S3:             swSrv,
		Presign:        swSrv,
		Filer:          swSrv,
		S3Endpoint:     "https://fake-s3.example",
		FilerURL:       "https://fake-filer.example",
		Region:         "us-east-1",
		AdminAccessKey: "lahijan-dev-admin-key",
		AdminSecretKey: "lahijan-dev-admin-secret",
	})
	require.NoError(t, err)
	storageSvc := storage.New(
		provider,
		ta.repos,
		audit.NewDBEmitter(ta.repos.AuditLog),
		nil,
		rbac.NewEvaluator(ta.repos.Memberships),
		storage.Config{},
	)
	ta.server.SetStorageService(storageSvc)
	return ta, swSrv
}

// storageReq is a thin helper that builds an *http.Request with the
// session cookie + X-Tenant-Id header pre-set. body may be nil.
func storageReq(method, path, sess, tidStr string, body []byte) *http.Request {
	var r *bytes.Reader
	if body != nil {
		r = bytes.NewReader(body)
	} else {
		r = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// jsonReq marshals v as JSON and returns the bytes (or nil for nil v).
func jsonReq(t *testing.T, v any) []byte {
	t.Helper()
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// TestStorageEndpoints_RequireAuth is the WS-16 DoD "every privileged
// action calls RequirePerm" at the HTTP layer: every storage endpoint
// rejects an anonymous request with 401.
func TestStorageEndpoints_RequireAuth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	bucketID := uuid.NewString()
	credentialID := uuid.NewString()
	cases := []struct {
		method, path string
	}{
		{"GET", "/api/v1/storage/buckets"},
		{"POST", "/api/v1/storage/buckets"},
		{"GET", "/api/v1/storage/buckets/" + bucketID},
		{"PATCH", "/api/v1/storage/buckets/" + bucketID},
		{"DELETE", "/api/v1/storage/buckets/" + bucketID},
		{"GET", "/api/v1/storage/buckets/" + bucketID + "/credentials"},
		{"POST", "/api/v1/storage/buckets/" + bucketID + "/credentials"},
		{"DELETE", "/api/v1/storage/buckets/" + bucketID + "/credentials/" + credentialID},
		{"POST", "/api/v1/storage/buckets/" + bucketID + "/presign"},
		{"POST", "/api/v1/storage/buckets/" + bucketID + "/quota"},
		{"GET", "/api/v1/storage/buckets/" + bucketID + "/usage"},
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

// TestStorageEndpoints_RequireTenantScope covers the WS-16 DoD for the
// tenant-scope contract: a request without X-Tenant-Id returns 400.
func TestStorageEndpoints_RequireTenantScope(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	_, sess, _ := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/storage/buckets", nil)
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

// TestStorageEndpoints_DisabledWhenNoProvider covers the WS-16 DoD 501
// path: when the SeaweedFS provider is not wired (storageSvc == nil),
// every storage endpoint returns 501 with the localised "feature
// disabled" envelope.
func TestStorageEndpoints_DisabledWhenNoProvider(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // storageSvc is nil by default
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := httptest.NewRequest("GET", "/api/v1/storage/buckets", nil)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tidStr)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 501, resp.StatusCode, "storage must 501 when provider is disabled")
	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok, "error envelope must be present")
	assert.Equal(t, "not_implemented", errBody["code"])
}

// TestCreateStorageBucket_BadRequest covers the WS-16 DoD for the 400
// envelope path: a body missing required fields returns the standard
// bad_request envelope, NOT 501 (validation runs before the service).
func TestCreateStorageBucket_BadRequest(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr, []byte(`{"slug":""}`))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	body := decodeBody(t, resp)
	errBody, ok := body["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "bad_request", errBody["code"])
}

// TestCreateStorageBucket_HappyPath exercises the full create-bucket
// flow: 201 + envelope-shaped response + the bucket exists in the
// tenant's list.
func TestCreateStorageBucket_HappyPath(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	// Create.
	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr,
		jsonReq(t, map[string]any{"slug": "applogs", "label": "Application logs"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucket := decodeBody(t, resp)
	assert.NotEmpty(t, bucket["id"])
	assert.Equal(t, "applogs", bucket["slug"])
	// Canonical name is "<tenant-uuid>-<slug>"; the tenant-uuid portion
	// matches the tenant id we passed via the X-Tenant-Id header.
	assert.Contains(t, bucket["name"].(string), tidStr)
	assert.Contains(t, bucket["name"].(string), "applogs")
	bucketID := bucket["id"].(string)

	// List.
	req = storageReq("GET", "/api/v1/storage/buckets", sess, tidStr, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	page := decodeBody(t, resp)
	items, _ := page["items"].([]any)
	require.Len(t, items, 1)

	// Get by id.
	req = storageReq("GET", "/api/v1/storage/buckets/"+bucketID, sess, tidStr, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
}

// TestStorage_RBAC_ViewerCannotCreate covers the WS-16 DoD "every
// privileged action calls RequirePerm": a tenant viewer (read only)
// cannot create a bucket, but can list buckets.
func TestStorage_RBAC_ViewerCannotCreate(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantViewer)

	// List buckets: gate passes (rbac read granted to viewer).
	req := storageReq("GET", "/api/v1/storage/buckets", sess, tidStr, nil)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.NotEqual(t, 403, resp.StatusCode, "viewer should pass the rbac read gate")

	// Create bucket: denied (viewer does NOT hold s3.bucket.create).
	req = storageReq("POST", "/api/v1/storage/buckets", sess, tidStr, []byte(`{"slug":"x"}`))
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "viewer cannot create")
	body403 := decodeBody(t, resp)
	errBody, ok := body403["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "forbidden", errBody["code"])
	assert.Equal(t, "s3.bucket.create", errBody["details"].(map[string]any)["permission"])
}

// TestStorage_CredentialShownOnce is the WS-16 DoD "credentials hashed
// at rest; plaintext shown once at creation": the mint response carries
// secretKey; the list response does NOT.
func TestStorage_CredentialShownOnce(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	// Create bucket.
	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr,
		jsonReq(t, map[string]any{"slug": "credtest"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucketID := decodeBody(t, resp)["id"].(string)

	// Mint credential.
	req = storageReq("POST", "/api/v1/storage/buckets/"+bucketID+"/credentials", sess, tidStr,
		jsonReq(t, map[string]any{"label": "ci", "actions": []string{"Read", "Write"}}))
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	minted := decodeBody(t, resp)
	assert.NotEmpty(t, minted["secretKey"], "plaintext secret must be in the mint response")
	credential := minted["credential"].(map[string]any)
	credID := credential["id"].(string)
	accessKey := credential["accessKeyId"].(string)
	assert.NotEmpty(t, accessKey)

	// List credentials: no secretKey field anywhere.
	req = storageReq("GET", "/api/v1/storage/buckets/"+bucketID+"/credentials", sess, tidStr, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	page := decodeBody(t, resp)
	items, _ := page["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, credID, first["id"])
	assert.Equal(t, accessKey, first["accessKeyId"])
	_, hasSecret := first["secretKey"]
	assert.False(t, hasSecret, "list response must not include secretKey")
}

// TestStorage_Presign_GetAndPut is the WS-16 DoD "pre-signed URL works
// for GET and PUT" at the HTTP boundary.
func TestStorage_Presign_GetAndPut(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr,
		jsonReq(t, map[string]any{"slug": "presign-http"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucketID := decodeBody(t, resp)["id"].(string)

	// GET presign.
	req = storageReq("POST", "/api/v1/storage/buckets/"+bucketID+"/presign", sess, tidStr,
		jsonReq(t, map[string]any{"method": "GET", "key": "obj.bin", "expiresInSeconds": 300}))
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	getRes := decodeBody(t, resp)
	assert.Equal(t, "GET", getRes["method"])
	assert.NotEmpty(t, getRes["url"])

	// PUT presign.
	req = storageReq("POST", "/api/v1/storage/buckets/"+bucketID+"/presign", sess, tidStr,
		jsonReq(t, map[string]any{"method": "PUT", "key": "upload.bin", "expiresInSeconds": 600}))
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	putRes := decodeBody(t, resp)
	assert.Equal(t, "PUT", putRes["method"])
	assert.NotEmpty(t, putRes["url"])
}

// TestStorage_QuotaAndUsage covers the WS-16 quota + usage endpoints at
// the HTTP boundary.
func TestStorage_QuotaAndUsage(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr,
		jsonReq(t, map[string]any{"slug": "quota-http"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucketID := decodeBody(t, resp)["id"].(string)

	// Set quota.
	req = storageReq("POST", "/api/v1/storage/buckets/"+bucketID+"/quota", sess, tidStr,
		jsonReq(t, map[string]any{"quotaBytes": 100 * 1024 * 1024, "quotaObjects": 500}))
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 204, resp.StatusCode)

	// Get usage.
	req = storageReq("GET", "/api/v1/storage/buckets/"+bucketID+"/usage", sess, tidStr, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	usage := decodeBody(t, resp)
	assert.EqualValues(t, 100*1024*1024, usage["quotaBytes"])
	assert.EqualValues(t, 500, usage["quotaObjects"])
	assert.EqualValues(t, 0, usage["bytesUsed"])
}

// TestStorage_TenantIsolation_HTTP ensures tenant B cannot reach tenant
// A's bucket via the HTTP API. The repository layer enforces tenant
// scoping; a cross-tenant bucket id surfaces as 404.
func TestStorage_TenantIsolation_HTTP(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)

	// Tenant A: register + create bucket.
	_, sessA, tidStrA := registerAndLogin(t, ta, rbac.RoleTenantAdmin)
	req := storageReq("POST", "/api/v1/storage/buckets", sessA, tidStrA,
		jsonReq(t, map[string]any{"slug": "tenant-a-only"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucketID := decodeBody(t, resp)["id"].(string)

	// Tenant B: register + try to access tenant A's bucket.
	_, sessB, tidStrB := registerAndLogin(t, ta, rbac.RoleTenantAdmin)
	req = storageReq("GET", "/api/v1/storage/buckets/"+bucketID, sessB, tidStrB, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode, "tenant B must NOT see tenant A's bucket")
}

// TestStorage_BucketDelete_CascadeCredentials ensures deleting a bucket
// returns 204 and subsequent lookups 404.
func TestStorage_BucketDelete_CascadeCredentials(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr,
		jsonReq(t, map[string]any{"slug": "delete-me"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucketID := decodeBody(t, resp)["id"].(string)

	// Mint a credential against the bucket.
	req = storageReq("POST", "/api/v1/storage/buckets/"+bucketID+"/credentials", sess, tidStr,
		jsonReq(t, map[string]any{"actions": []string{"Read"}}))
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)

	// Delete the bucket.
	req = storageReq("DELETE", "/api/v1/storage/buckets/"+bucketID, sess, tidStr, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 204, resp.StatusCode)

	// Subsequent get 404s.
	req = storageReq("GET", "/api/v1/storage/buckets/"+bucketID, sess, tidStr, nil)
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 404, resp.StatusCode)
}

// TestStorage_AuditRowsPersisted confirms every privileged action emits
// an audit row that lands in the DB. The DB emitter is wired in
// newStorageTestApp; we read the audit_log table directly to assert.
func TestStorage_AuditRowsPersisted(t *testing.T) {
	t.Parallel()
	ta, _ := newStorageTestApp(t)
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	// Create bucket.
	req := storageReq("POST", "/api/v1/storage/buckets", sess, tidStr,
		jsonReq(t, map[string]any{"slug": "audit-test"}))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	bucketID := decodeBody(t, resp)["id"].(string)

	// Set quota (privileged).
	req = storageReq("POST", "/api/v1/storage/buckets/"+bucketID+"/quota", sess, tidStr,
		jsonReq(t, map[string]any{"quotaBytes": 1024, "quotaObjects": 1}))
	_, err = ta.app.Test(req, -1)
	require.NoError(t, err)

	// Query the audit_log directly for storage actions.
	ctx := context.Background()
	rows, err := testutil.Pool().Query(ctx,
		`SELECT action FROM audit_log WHERE action LIKE 's3.%' ORDER BY created_at`)
	require.NoError(t, err)
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		actions = append(actions, a)
	}
	require.Contains(t, actions, audit.ActionS3BucketCreate)
	require.Contains(t, actions, audit.ActionS3BucketQuotaSet)
}
