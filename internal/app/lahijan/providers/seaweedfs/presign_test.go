// Package seaweedfs_test: presign_test.go covers the pre-signed URL
// generation path. The fake presign client produces a deterministic
// SigV4-shaped URL; tests assert the bucket + key + method landed.
package seaweedfs_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresign_GetObject(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "uploads")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	res, err := p.PresignGetObject(ctx, bucket, "photo.jpg", 0)
	require.NoError(t, err)
	assert.Equal(t, "GET", res.Method)
	assert.Equal(t, bucket, res.Bucket)
	assert.Equal(t, "photo.jpg", res.Key)
	assert.Contains(t, res.URL, bucket+"/photo.jpg")
	assert.Contains(t, res.URL, "X-Amz-Algorithm=AWS4-HMAC-SHA256")
	assert.False(t, res.ExpiresAt.IsZero())
}

func TestPresign_PutObject(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "uploads")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	res, err := p.PresignPutObject(ctx, bucket, "upload.bin", 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "PUT", res.Method)
	assert.Contains(t, res.URL, bucket+"/upload.bin")
}

func TestPresign_InvalidBucket(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.PresignGetObject(ctx, "UPPER", "k", time.Minute)
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrInvalidBucketName)
}

func TestPresign_DefaultTTLApplied(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	// Default TTL = 60s via connectProvider.
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	before := time.Now()
	res, err := p.PresignGetObject(ctx, bucket, "k", 0)
	require.NoError(t, err)
	// ExpiresAt should be ~60s in the future (default TTL).
	assert.WithinDuration(t, before.Add(60*time.Second), res.ExpiresAt, 5*time.Second)
}

func TestPresign_KeyedURLs_Distinct(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "x")
	a, err := p.PresignGetObject(ctx, bucket, "a.txt", time.Minute)
	require.NoError(t, err)
	b, err := p.PresignGetObject(ctx, bucket, "b.txt", time.Minute)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(a.URL, "/a.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256"))
	assert.True(t, strings.HasSuffix(b.URL, "/b.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256"))
}
