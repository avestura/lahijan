// Package seaweedfs_test: buckets_test.go covers the bucket CRUD surface
// (Create / Head / List / Delete) against the in-memory fake. The
// "tenant A's user cannot access tenant B's bucket" isolation test
// lives here too — it's enforced by the naming convention + SeaweedFS'
// per-identity bucket allowlist, both of which the fake honours.
package seaweedfs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuckets_CreateAndHead(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := mustBucketName(t, "uploads")
	created, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{
		Bucket:   name,
		TenantID: validTenantUUID,
	})
	require.NoError(t, err)
	assert.Equal(t, name, created.Name)
	assert.False(t, created.CreatedAt.IsZero(), "CreatedAt must be populated")

	require.NoError(t, p.HeadBucket(ctx, name))
}

func TestBuckets_Create_AlreadyExists(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := mustBucketName(t, "dup")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: name, TenantID: validTenantUUID})
	require.NoError(t, err)

	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: name, TenantID: validTenantUUID})
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrAlreadyExists)
}

func TestBuckets_Create_InvalidName(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: "UPPER"})
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrInvalidBucketName)

	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrInvalidBucketName)
}

func TestBuckets_Head_NotFound(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.HeadBucket(ctx, mustBucketName(t, "missing"))
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrNotFound)
}

func TestBuckets_List(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create three buckets across two tenants and verify List returns all.
	const tenantA = "11111111-2222-3333-4444-555555555555"
	const tenantB = "66666666-7777-8888-9999-aaaaaaaaaaaa"
	a1, err := seaweedfs.BucketName(tenantA, "avatars")
	require.NoError(t, err)
	a2, err := seaweedfs.BucketName(tenantA, "uploads")
	require.NoError(t, err)
	b1, err := seaweedfs.BucketName(tenantB, "logs")
	require.NoError(t, err)

	for _, name := range []string{a1, a2, b1} {
		_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: name})
		require.NoError(t, err)
	}

	buckets, err := p.ListBuckets(ctx)
	require.NoError(t, err)
	assert.Len(t, buckets, 3)

	names := make(map[string]bool, len(buckets))
	for _, b := range buckets {
		names[b.Name] = true
	}
	assert.True(t, names[a1])
	assert.True(t, names[a2])
	assert.True(t, names[b1])
}

func TestBuckets_Delete(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := mustBucketName(t, "todelete")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: name})
	require.NoError(t, err)

	require.NoError(t, p.DeleteBucket(ctx, name))
	require.ErrorIs(t, p.HeadBucket(ctx, name), seaweedfs.ErrNotFound)
}

func TestBuckets_Delete_NotFound(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.DeleteBucket(ctx, mustBucketName(t, "ghost"))
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrNotFound)
}

func TestBuckets_Delete_RemovesQuotaRecord(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	name := mustBucketName(t, "quota-clear")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{
		Bucket: name,
		Quota:  &seaweedfs.QuotaSpec{SizeMiB: 1},
	})
	require.NoError(t, err)

	// Quota is set + readable.
	got, err := p.GetBucketQuota(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.SizeMiB)

	require.NoError(t, p.DeleteBucket(ctx, name))
}

// TestBuckets_TenantIsolation_ByNaming proves the naming convention
// alone enforces tenant isolation at the bucket layer: tenant B cannot
// collide with tenant A's name by choosing the same slug. The
// per-identity bucket allowlist (see iam_test.go) is the second layer
// of isolation.
func TestBuckets_TenantIsolation_ByNaming(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const tenantA = "11111111-2222-3333-4444-555555555555"
	const tenantB = "66666666-7777-8888-9999-aaaaaaaaaaaa"
	aName, err := seaweedfs.BucketName(tenantA, "shared-slug")
	require.NoError(t, err)
	bName, err := seaweedfs.BucketName(tenantB, "shared-slug")
	require.NoError(t, err)
	assert.NotEqual(t, aName, bName, "same slug across tenants MUST produce distinct names")

	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: aName, TenantID: tenantA})
	require.NoError(t, err)
	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bName, TenantID: tenantB})
	require.NoError(t, err)
}

// TestBuckets_TenantIsolation_ByCredentialScope proves the per-identity
// bucket allowlist enforces tenant isolation at the data plane. Tenant
// A's user has credentials scoped to tenant A's buckets only; even if
// that user somehow learns tenant B's bucket name, SeaweedFS rejects
// the request because the bucket is not in the credential's scope.
//
// The fake enforces this by storing the bucket allowlist per identity
// (IdentityBuckets) — the actual S3 server-side enforcement is what
// WS-22 will exercise end-to-end.
func TestBuckets_TenantIsolation_ByCredentialScope(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const tenantA = "11111111-2222-3333-4444-555555555555"
	const tenantB = "66666666-7777-8888-9999-aaaaaaaaaaaa"
	aBucket, err := seaweedfs.BucketName(tenantA, "avatars")
	require.NoError(t, err)
	bBucket, err := seaweedfs.BucketName(tenantB, "secrets")
	require.NoError(t, err)
	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: aBucket, TenantID: tenantA})
	require.NoError(t, err)
	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bBucket, TenantID: tenantB})
	require.NoError(t, err)

	// Mint a credential scoped ONLY to tenant A's bucket.
	cred, err := p.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		TenantID: tenantA,
		Buckets:  []string{aBucket},
		Actions:  []seaweedfs.IAMAction{seaweedfs.IAMActionAdmin},
	})
	require.NoError(t, err)

	// The fake stores the scope per identity. The tenant A credential's
	// scope MUST contain tenant A's bucket and MUST NOT contain tenant
	// B's bucket.
	scope := srv.IdentityBuckets(cred.AccessKey)
	assert.Contains(t, scope, aBucket)
	assert.NotContains(t, scope, bBucket)
}

func TestBuckets_Create_BadInput_NoPanic(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A nil params struct's zero-value Bucket ("") fails validation
	// rather than panicking. Errors flow back via the standard wrap.
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, seaweedfs.ErrInvalidBucketName))
}
