// Package seaweedfs_test: quotas_test.go covers the per-bucket quota
// write/read/clear cycle and the enforcement path. Enforcement is
// exercised via the fake's PutObject hook, which reads the Filer-side
// quota record and rejects uploads that would cross the ceiling.
package seaweedfs_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotas_SetGetClear(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Initially no quota set.
	got, err := p.GetBucketQuota(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.QuotaSpec{}, got, "fresh bucket must have zero quota")

	// Set a quota.
	require.NoError(t, p.SetBucketQuota(ctx, bucket, seaweedfs.QuotaSpec{SizeMiB: 5, FileCount: 100}))
	got, err = p.GetBucketQuota(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.SizeMiB)
	assert.Equal(t, int64(100), got.FileCount)

	// Clear it.
	require.NoError(t, p.ClearBucketQuota(ctx, bucket))
	got, err = p.GetBucketQuota(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.QuotaSpec{}, got)
}

func TestQuotas_RejectsNegative(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	err = p.SetBucketQuota(ctx, bucket, seaweedfs.QuotaSpec{SizeMiB: -1})
	require.Error(t, err)
}

func TestQuotas_EnforcedOnPutObject(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "small")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)
	// 1 MiB size ceiling; we'll try to PUT 2 MiB.
	require.NoError(t, p.SetBucketQuota(ctx, bucket, seaweedfs.QuotaSpec{SizeMiB: 1}))

	// The Provider's PutObject is not directly exposed via the driver
	// (the data plane goes through SeaweedFS directly). The fake enforces
	// the quota via its own PutObject impl; we trigger it by calling
	// the underlying SDK method via the Provider's s3BucketAPI. Because
	// the driver only exposes bucket + presign + IAM + quota operations,
	// the test calls the fake's S3 impl directly to simulate a user
	// upload. The fake's enforcement path is identical to what
	// SeaweedFS would do.
	big := bytes.Repeat([]byte("x"), 2*1024*1024)
	_, err = srv.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: strPtr(bucket),
		Key:    strPtr("big.bin"),
		Body:   bytes.NewReader(big),
	})
	require.Error(t, err, "PUT over the size quota MUST be rejected")
	assert.True(t, strings.Contains(err.Error(), "quota") || strings.Contains(err.Error(), "Quota"),
		"error should mention quota, got %v", err)
}

func TestQuotas_FileCountEnforced(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "many-files")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)
	require.NoError(t, p.SetBucketQuota(ctx, bucket, seaweedfs.QuotaSpec{FileCount: 2}))

	// Two uploads fit; the third is rejected.
	for i := 0; i < 2; i++ {
		_, err := srv.PutObject(ctx, &awss3.PutObjectInput{
			Bucket: strPtr(bucket),
			Key:    strPtr(string(rune('a' + i))),
			Body:   bytes.NewReader([]byte{1}),
		})
		require.NoError(t, err, "upload %d should fit under file count quota", i)
	}
	_, err = srv.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: strPtr(bucket),
		Key:    strPtr("c"),
		Body:   bytes.NewReader([]byte{1}),
	})
	require.Error(t, err, "third upload MUST be rejected by file-count quota")
}

func TestQuotas_CreateWithDefaultQuota(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	// Construct a provider with a non-zero default quota.
	p, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient:     newHTTPClient(),
		S3:             srv,
		Presign:        srv,
		Filer:          srv,
		S3Endpoint:     "https://fake-s3.example",
		FilerURL:       "https://fake-filer.example",
		AdminAccessKey: "k",
		AdminSecretKey: "s",
		DefaultQuota:   seaweedfs.QuotaSpec{SizeMiB: 10},
		RequestTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "with-default")
	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	got, err := p.GetBucketQuota(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, int64(10), got.SizeMiB, "CreateBucket must apply the driver default quota")
}

func TestQuotas_CreateWithExplicitZero(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient:     newHTTPClient(),
		S3:             srv,
		Presign:        srv,
		Filer:          srv,
		S3Endpoint:     "https://fake-s3.example",
		FilerURL:       "https://fake-filer.example",
		AdminAccessKey: "k",
		AdminSecretKey: "s",
		DefaultQuota:   seaweedfs.QuotaSpec{SizeMiB: 10},
		RequestTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "no-default")
	// Explicit zero Quota pointer bypasses the driver default.
	zero := seaweedfs.QuotaSpec{}
	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket, Quota: &zero})
	require.NoError(t, err)

	got, err := p.GetBucketQuota(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.QuotaSpec{}, got, "explicit zero QuotaSpec must NOT trigger a write")
}

// strPtr is the test-side helper for SDK-style pointer fields.
func strPtr(s string) *string { return &s }
