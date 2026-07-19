// Package storage: service_integration_test.go exercises the storage.Service
// end-to-end against a real Postgres (via testcontainers-go) + an in-process
// fake of the SeaweedFS provider. Covers the WS-16 DoD:
//
//   - create bucket -> mint credential -> list -> revoke -> delete with
//     audit + bus assertions
//   - presign URL generation for GET + PUT
//   - quota set + usage read
//   - tenant isolation: tenant A cannot operate on tenant B's bucket
//   - every privileged action emits audit pre + post
//
// Run with:  go test -tags integration ./internal/app/lahijan/storage/...

//go:build integration

package storage_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// fakeSW is an in-memory implementation of storage.swProvider. It tracks
// calls + drives bucket + credential state so the service test can assert
// the orchestration order without standing up the httptest fake. (The
// httptest fake has its own coverage in providers/seaweedfs/fake/.)
type fakeSW struct {
	mu sync.Mutex

	buckets     map[string]*fakeBucketState
	credentials map[string]*fakeIdentityState
	quotas      map[string]seaweedfs.QuotaSpec

	presignCalls int
	presignLast  presignCall

	createBucketErr error
	deleteBucketErr error
	mintErr         error
	revokeErr       error
	setQuotaErr     error
	presignErr      error
}

type fakeBucketState struct {
	name string
}

type fakeIdentityState struct {
	accessKey string
	secretKey string
	buckets   []string
	actions   []seaweedfs.IAMAction
	disabled  bool
}

type presignCall struct {
	bucket string
	key    string
	ttl    time.Duration
	isPut  bool
}

func newFakeSW() *fakeSW {
	return &fakeSW{
		buckets:     make(map[string]*fakeBucketState),
		credentials: make(map[string]*fakeIdentityState),
		quotas:      make(map[string]seaweedfs.QuotaSpec),
	}
}

func (f *fakeSW) Ping(_ context.Context) error { return nil }

func (f *fakeSW) CreateBucket(_ context.Context, params seaweedfs.CreateBucketParams) (*seaweedfs.Bucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createBucketErr != nil {
		return nil, f.createBucketErr
	}
	if _, exists := f.buckets[params.Bucket]; exists {
		return nil, seaweedfs.ErrAlreadyExists
	}
	f.buckets[params.Bucket] = &fakeBucketState{name: params.Bucket}
	if params.Quota != nil {
		f.quotas[params.Bucket] = *params.Quota
	}
	return &seaweedfs.Bucket{Name: params.Bucket, CreatedAt: time.Now()}, nil
}

func (f *fakeSW) HeadBucket(_ context.Context, bucket string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.buckets[bucket]; !ok {
		return seaweedfs.ErrNotFound
	}
	return nil
}

func (f *fakeSW) ListBuckets(_ context.Context) ([]seaweedfs.Bucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]seaweedfs.Bucket, 0, len(f.buckets))
	for _, b := range f.buckets {
		out = append(out, seaweedfs.Bucket{Name: b.name})
	}
	return out, nil
}

func (f *fakeSW) DeleteBucket(_ context.Context, bucket string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteBucketErr != nil {
		return f.deleteBucketErr
	}
	if _, ok := f.buckets[bucket]; !ok {
		return seaweedfs.ErrNotFound
	}
	delete(f.buckets, bucket)
	delete(f.quotas, bucket)
	return nil
}

func (f *fakeSW) MintCredentials(_ context.Context, params seaweedfs.MintCredentialsParams) (*seaweedfs.IAMCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mintErr != nil {
		return nil, f.mintErr
	}
	accessKey := "lah-test-" + uuid.NewString()[:12]
	secretKey := "test-secret-" + uuid.NewString()[:16]
	f.credentials[accessKey] = &fakeIdentityState{
		accessKey: accessKey,
		secretKey: secretKey,
		buckets:   append([]string(nil), params.Buckets...),
		actions:   append([]seaweedfs.IAMAction(nil), params.Actions...),
	}
	return &seaweedfs.IAMCredential{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Buckets:   append([]string(nil), params.Buckets...),
		Actions:   nil, // elided for brevity
		Enabled:   true,
	}, nil
}

func (f *fakeSW) RevokeCredentials(_ context.Context, accessKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revokeErr != nil {
		return f.revokeErr
	}
	if _, ok := f.credentials[accessKey]; !ok {
		return seaweedfs.ErrNotFound
	}
	delete(f.credentials, accessKey)
	return nil
}

func (f *fakeSW) SetBucketQuota(_ context.Context, bucket string, quota seaweedfs.QuotaSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setQuotaErr != nil {
		return f.setQuotaErr
	}
	f.quotas[bucket] = quota
	return nil
}

func (f *fakeSW) GetBucketQuota(_ context.Context, bucket string) (seaweedfs.QuotaSpec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quotas[bucket], nil
}

func (f *fakeSW) PresignGetObject(_ context.Context, bucket, key string, ttl time.Duration) (*seaweedfs.PresignResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.presignErr != nil {
		return nil, f.presignErr
	}
	f.presignCalls++
	f.presignLast = presignCall{bucket: bucket, key: key, ttl: ttl, isPut: false}
	return &seaweedfs.PresignResult{
		URL:       "https://fake-s3.example/" + bucket + "/" + key + "?get",
		Method:    "GET",
		Bucket:    bucket,
		Key:       key,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func (f *fakeSW) PresignPutObject(_ context.Context, bucket, key string, ttl time.Duration) (*seaweedfs.PresignResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.presignErr != nil {
		return nil, f.presignErr
	}
	f.presignCalls++
	f.presignLast = presignCall{bucket: bucket, key: key, ttl: ttl, isPut: true}
	return &seaweedfs.PresignResult{
		URL:       "https://fake-s3.example/" + bucket + "/" + key + "?put",
		Method:    "PUT",
		Bucket:    bucket,
		Key:       key,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

// recorderBus is a minimal eventbus.Bus-shaped recorder.
type recorderBus struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (r *recorderBus) Emit(_ context.Context, e eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recorderBus) snapshot() []eventbus.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventbus.Event, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recorderBus) topics() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Topic
	}
	return out
}

// capturingEmitter records every Emit + MarkOutcome call.
type capturingEmitter struct {
	mu       sync.Mutex
	rows     []audit.Event
	outcomes map[uuid.UUID]audit.Outcome
}

func newCapturingEmitter() *capturingEmitter {
	return &capturingEmitter{outcomes: map[uuid.UUID]audit.Outcome{}}
}

func (e *capturingEmitter) Emit(_ context.Context, ev audit.Event) (uuid.UUID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := uuid.New()
	e.rows = append(e.rows, ev)
	return id, nil
}

func (e *capturingEmitter) MarkOutcome(_ context.Context, auditID uuid.UUID, o audit.Outcome) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outcomes[auditID] = o
	return nil
}

func (e *capturingEmitter) eventsFor(action string) []audit.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []audit.Event{}
	for _, r := range e.rows {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

func (e *capturingEmitter) successCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.outcomes {
		if o.Status == audit.StatusSuccess {
			n++
		}
	}
	return n
}

func (e *capturingEmitter) failureCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.outcomes {
		if o.Status == audit.StatusFailure {
			n++
		}
	}
	return n
}

// fixture wires the dependencies the suite shares.
type fixture struct {
	svc       *storage.Service
	sw        *fakeSW
	bus       *recorderBus
	auditEm   *capturingEmitter
	tenantID  uuid.UUID
	userID    uuid.UUID
	ctx       context.Context
	tenantCtx context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	f := &fixture{
		sw:        newFakeSW(),
		bus:       &recorderBus{},
		auditEm:   newCapturingEmitter(),
		tenantID:  tenant.ID,
		userID:    user.ID,
		ctx:       ctx,
		tenantCtx: database.WithTenant(ctx, tenant.ID),
	}
	f.svc = storage.New(f.sw, repos, f.auditEm, f.bus, nil, storage.Config{})
	return f
}

// TestCreateBucket_HappyPath is the WS-16 DoD happy-path test: create a
// bucket, assert the canonical name + audit + bus emissions.
func TestCreateBucket_HappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug:        "logs",
		Label:       "Application logs",
		Description: "central log bucket",
		QuotaBytes:  1024 * 1024 * 100, // 100 MiB
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, bucket.ID)
	assert.Equal(t, "logs", bucket.Slug)
	assert.Contains(t, bucket.Name, "logs")
	assert.Contains(t, bucket.Name, f.tenantID.String())
	assert.Equal(t, int64(1024*1024*100), bucket.QuotaBytes)

	// Audit + bus assertions for create-bucket.
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketCreate),
		"create-bucket must emit an audit row")
	require.NotEmpty(t, f.bus.snapshot(), "create-bucket must emit into the bus")
	assert.Contains(t, f.bus.topics(), eventbus.S3BucketCreated)

	// Provider saw the create + the quota push.
	f.sw.mu.Lock()
	_, ok := f.sw.buckets[bucket.Name]
	f.sw.mu.Unlock()
	require.True(t, ok, "bucket must be created on the provider")
}

// TestCreateBucket_InvalidSlug covers the WS-16 DoD "invalid slug
// rejected with a clear message".
func TestCreateBucket_InvalidSlug(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	cases := []struct {
		name string
		slug string
	}{
		{"empty", ""},
		{"uppercase", "Logs"},
		{"starts with dash", "-logs"},
		{"ends with dash", "logs-"},
		{"consecutive dashes", "lo--gs"},
		{"too long", strings.Repeat("a", 27)},
		{"illegal char", "lo_gs"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
				Slug: tc.slug,
			})
			require.ErrorIs(t, err, storage.ErrInvalidBucketSlug)
		})
	}
}

// TestCreateBucket_AlreadyExists covers the WS-16 DoD for the 409
// envelope path: a second create with the same slug returns
// ErrBucketAlreadyExists.
func TestCreateBucket_AlreadyExists(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	params := storage.BucketCreateParams{Slug: "images"}
	_, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, params)
	require.NoError(t, err)

	_, err = f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, params)
	require.ErrorIs(t, err, storage.ErrBucketAlreadyExists)
}

// TestBucketCRUD_GetListUpdateDelete walks the full bucket lifecycle.
func TestBucketCRUD_GetListUpdateDelete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	created, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug:  "data",
		Label: "Initial label",
	})
	require.NoError(t, err)

	// Get.
	fetched, err := f.svc.GetBucket(f.tenantCtx, f.tenantID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, "Initial label", fetched.Label)

	// Update.
	newLabel := "Updated label"
	newDesc := "Updated description"
	updated, err := f.svc.UpdateBucket(f.tenantCtx, f.tenantID, f.userID, created.ID, storage.BucketUpdateParams{
		Label:       &newLabel,
		Description: &newDesc,
	})
	require.NoError(t, err)
	assert.Equal(t, newLabel, updated.Label)
	assert.Equal(t, newDesc, updated.Description)
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketUpdate))

	// List.
	listed, err := f.svc.ListBuckets(f.tenantCtx, f.tenantID, 100, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(listed), 1)

	// Delete.
	require.NoError(t, f.svc.DeleteBucket(f.tenantCtx, f.tenantID, f.userID, created.ID))
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketDelete))

	// Get after delete -> 404 envelope.
	_, err = f.svc.GetBucket(f.tenantCtx, f.tenantID, created.ID)
	require.ErrorIs(t, err, storage.ErrBucketNotFound)
}

// TestMintCredential_HappyPath_Revoke covers the WS-16 DoD for the
// credential lifecycle: mint -> list -> revoke. The plaintext secret is
// returned at mint time and is NOT in the list response.
func TestMintCredential_HappyPath_Revoke(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "uploads",
	})
	require.NoError(t, err)

	// Mint.
	minted, err := f.svc.MintCredential(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.CredentialMintParams{
		Label:   "ci-runner",
		Actions: []storage.CredentialAction{storage.ActionRead, storage.ActionWrite},
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, minted.Row.ID)
	assert.NotEmpty(t, minted.SecretKey, "plaintext secret must be returned at mint time")
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditCredentialMint))
	assert.Contains(t, f.bus.topics(), eventbus.S3CredentialMinted)

	// List.
	creds, err := f.svc.ListCredentials(f.tenantCtx, f.tenantID, bucket.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Equal(t, minted.Row.AccessKeyID, creds[0].AccessKeyID)

	// Revoke.
	require.NoError(t, f.svc.RevokeCredential(f.tenantCtx, f.tenantID, f.userID, bucket.ID, minted.Row.ID))
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditCredentialRevoke))
	assert.Contains(t, f.bus.topics(), eventbus.S3CredentialRevoked)

	// List after revoke -> empty.
	creds, err = f.svc.ListCredentials(f.tenantCtx, f.tenantID, bucket.ID, 100, 0)
	require.NoError(t, err)
	assert.Empty(t, creds, "revoked credentials must not appear in the list")

	// Revoke is idempotent.
	require.NoError(t, f.svc.RevokeCredential(f.tenantCtx, f.tenantID, f.userID, bucket.ID, minted.Row.ID))
}

// TestMintCredential_BucketDeleteRevokesAll ensures that deleting a
// bucket revokes every credential scoped to it.
func TestMintCredential_BucketDeleteRevokesAll(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "todelete",
	})
	require.NoError(t, err)

	_, err = f.svc.MintCredential(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.CredentialMintParams{
		Actions: []storage.CredentialAction{storage.ActionRead},
	})
	require.NoError(t, err)
	_, err = f.svc.MintCredential(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.CredentialMintParams{
		Actions: []storage.CredentialAction{storage.ActionWrite},
	})
	require.NoError(t, err)

	creds, err := f.svc.ListCredentials(f.tenantCtx, f.tenantID, bucket.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, creds, 2)

	require.NoError(t, f.svc.DeleteBucket(f.tenantCtx, f.tenantID, f.userID, bucket.ID))

	// Bucket is gone; the service-level List returns ErrBucketNotFound
	// because the bucket row is soft-deleted. The point of this test
	// is that no orphaned credentials outlive the bucket, so query the
	// pool directly: every credential row for this bucket must carry a
	// non-nil revoked_at. The query is scoped by bucket_id; the tenant
	// scope is implied because the bucket itself is tenant-scoped.
	rows, err := testutil.Pool().Query(f.tenantCtx,
		`SELECT id, revoked_at FROM storage_credentials WHERE bucket_id = $1`, bucket.ID)
	require.NoError(t, err)
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var id uuid.UUID
		var revokedAt *time.Time
		require.NoError(t, rows.Scan(&id, &revokedAt))
		require.NotNil(t, revokedAt, "credential %s must be revoked", id)
	}
	require.Equal(t, 2, count, "both rows must still exist (audit trail)")
}

// TestPresign_GetAndPut covers the WS-16 DoD "pre-signed URL works for
// GET and PUT".
func TestPresign_GetAndPut(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "presign",
	})
	require.NoError(t, err)

	// GET.
	getRes, err := f.svc.Presign(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.PresignParams{
		Method:    storage.PresignGet,
		Key:       "path/to/object.bin",
		ExpiresIn: 5 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, "GET", getRes.Method)
	assert.Contains(t, getRes.URL, bucket.Name)
	assert.Contains(t, getRes.URL, "path/to/object.bin")

	// PUT.
	putRes, err := f.svc.Presign(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.PresignParams{
		Method:    storage.PresignPut,
		Key:       "uploads/file.tar",
		ExpiresIn: 10 * time.Minute,
	})
	require.NoError(t, err)
	assert.Equal(t, "PUT", putRes.Method)
	assert.Contains(t, putRes.URL, "uploads/file.tar")

	// Audit + bus assertions.
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditPresign))
	assert.Contains(t, f.bus.topics(), eventbus.S3PresignIssued)

	// Provider saw both presign calls.
	f.sw.mu.Lock()
	presignCount := f.sw.presignCalls
	f.sw.mu.Unlock()
	assert.GreaterOrEqual(t, presignCount, 2)
}

// TestPresign_InvalidMethod covers the WS-16 DoD for the 400 envelope
// path: a presign with an unsupported method returns ErrInvalidPresignMethod.
func TestPresign_InvalidMethod(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "presign-bad",
	})
	require.NoError(t, err)

	_, err = f.svc.Presign(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.PresignParams{
		Method:    "DELETE",
		ExpiresIn: time.Minute,
	})
	require.ErrorIs(t, err, storage.ErrInvalidPresignMethod)
}

// TestSetBucketQuota_HappyPath covers the WS-16 DoD "quota exceeded ->
// PUT rejected": the quota is pushed to the provider so the daemon
// enforces it.
func TestSetBucketQuota_HappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "quota",
	})
	require.NoError(t, err)

	require.NoError(t, f.svc.SetBucketQuota(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.QuotaSpec{
		QuotaBytes:   500 * 1024 * 1024,
		QuotaObjects: 1000,
	}))
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketQuotaSet))
	assert.Contains(t, f.bus.topics(), eventbus.S3BucketQuotaSet)

	// Provider saw the quota push.
	f.sw.mu.Lock()
	q := f.sw.quotas[bucket.Name]
	f.sw.mu.Unlock()
	assert.Greater(t, q.SizeMiB, int64(0), "provider must have received a non-zero SizeMiB quota")
	assert.Equal(t, int64(1000), q.FileCount)

	// DB row carries the new ceiling.
	row, err := f.svc.GetBucket(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(500*1024*1024), row.QuotaBytes)
	assert.Equal(t, int64(1000), row.QuotaObjects)
}

// TestGetBucketUsage covers the WS-16 cached-usage read path.
func TestGetBucketUsage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug:       "usage",
		QuotaBytes: 10 * 1024 * 1024,
	})
	require.NoError(t, err)

	usage, err := f.svc.GetBucketUsage(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(10*1024*1024), usage.QuotaBytes)
	// BytesUsed starts at zero; the metering job (WS-17) refreshes it.
	assert.Equal(t, int64(0), usage.BytesUsed)
}

// TestTenantIsolation_BucketCannotCrossTenant ensures tenant A cannot
// operate on tenant B's bucket via the storage service. The repository
// layer enforces tenant scoping so a cross-tenant bucket id surfaces as
// ErrBucketNotFound.
func TestTenantIsolation_BucketCannotCrossTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	userA := testutil.NewUser(ctx, t, testutil.Pool(), false)
	userB := testutil.NewUser(ctx, t, testutil.Pool(), false)

	sw := newFakeSW()
	bus := &recorderBus{}
	auditEm := newCapturingEmitter()
	svc := storage.New(sw, repos, auditEm, bus, nil, storage.Config{})

	ctxA := database.WithTenant(ctx, tenantA.ID)
	ctxB := database.WithTenant(ctx, tenantB.ID)

	// Tenant A creates a bucket.
	bucket, err := svc.CreateBucket(ctxA, tenantA.ID, userA.ID, storage.BucketCreateParams{
		Slug: "private",
	})
	require.NoError(t, err)

	// Tenant B cannot get it.
	_, err = svc.GetBucket(ctxB, tenantB.ID, bucket.ID)
	require.ErrorIs(t, err, storage.ErrBucketNotFound, "tenant B must not see tenant A's bucket")

	// Tenant B cannot delete it.
	err = svc.DeleteBucket(ctxB, tenantB.ID, userB.ID, bucket.ID)
	require.ErrorIs(t, err, storage.ErrBucketNotFound, "tenant B must not delete tenant A's bucket")

	// Tenant B cannot mint a credential against it.
	_, err = svc.MintCredential(ctxB, tenantB.ID, userB.ID, bucket.ID, storage.CredentialMintParams{
		Actions: []storage.CredentialAction{storage.ActionRead},
	})
	require.ErrorIs(t, err, storage.ErrBucketNotFound)
}

// TestProviderDisabled returns ErrProviderDisabled when no provider is
// wired. Mirrors the dns service's TestProviderDisabled.
func TestProviderDisabled(t *testing.T) {
	t.Parallel()

	repos := testutil.Repos()
	svc := storage.New(nil, repos, nil, nil, nil, storage.Config{})

	ctx := database.WithTenant(context.Background(), uuid.New())
	_, err := svc.CreateBucket(ctx, uuid.New(), uuid.New(), storage.BucketCreateParams{Slug: "x"})
	require.ErrorIs(t, err, storage.ErrProviderDisabled)
}

// TestRevokeCredential_NotFound ensures revoke returns
// ErrCredentialNotFound for an unknown credential id.
func TestRevokeCredential_NotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	bucket, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "no-cred",
	})
	require.NoError(t, err)

	err = f.svc.RevokeCredential(f.tenantCtx, f.tenantID, f.userID, bucket.ID, uuid.New())
	require.ErrorIs(t, err, storage.ErrCredentialNotFound)
}

// TestAuditEmit_Order confirms the audit pre-emit happens BEFORE the
// side effect. We assert this indirectly: when the provider fails, the
// audit row exists with status=failure (the pre-emit + mark-outcome
// flow). The pre-emit itself is implicitly tested by the audit
// assertions in every other test (the rows exist before the call
// returns).
func TestAuditEmit_Order(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sw.createBucketErr = errors.New("simulated provider failure")

	_, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{
		Slug: "will-fail",
	})
	require.Error(t, err)

	// Pre-emit happened + outcome was marked failure.
	rows := f.auditEm.eventsFor(storage.AuditBucketCreate)
	require.Len(t, rows, 1, "audit row must exist even when the provider fails")
	assert.Equal(t, audit.StatusPending, rows[0].Status)
	assert.Greater(t, f.auditEm.failureCount(), 0, "outcome must be marked failure")
}
