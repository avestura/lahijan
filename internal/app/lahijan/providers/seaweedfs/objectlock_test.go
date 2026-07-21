// Package seaweedfs_test: objectlock_test.go covers the WS-29 bucket-
// object-lock surface (SetObjectLockConfiguration / GetObjectLockConfiguration)
// against the in-memory fake. Per ADR-0027 the test boundary is the
// operations interface, not the S3 wire.
package seaweedfs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
)

func TestObjectLock_SetGet(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ol-set")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Fresh bucket has no object-lock config.
	got, err := p.GetObjectLockConfiguration(ctx, bucket)
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	// Enable.
	cfg := seaweedfs.ObjectLockConfig{
		Enabled: true,
		Mode:    seaweedfs.ObjectLockModeGovernance,
		Days:    30,
	}
	require.NoError(t, p.SetObjectLockConfiguration(ctx, bucket, cfg))
	got, err = p.GetObjectLockConfiguration(ctx, bucket)
	require.NoError(t, err)
	assert.True(t, got.Enabled)
	assert.Equal(t, seaweedfs.ObjectLockModeGovernance, got.Mode)
	assert.EqualValues(t, 30, got.Days)
}

func TestObjectLock_Set_Compliance(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ol-compliance")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	cfg := seaweedfs.ObjectLockConfig{
		Enabled: true,
		Mode:    seaweedfs.ObjectLockModeCompliance,
		Days:    365,
	}
	require.NoError(t, p.SetObjectLockConfiguration(ctx, bucket, cfg))
	got, err := p.GetObjectLockConfiguration(ctx, bucket)
	require.NoError(t, err)
	assert.True(t, got.Enabled)
	assert.Equal(t, seaweedfs.ObjectLockModeCompliance, got.Mode)
	assert.EqualValues(t, 365, got.Days)
}

func TestObjectLock_Set_InvalidMode(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ol-bad-mode")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	err = p.SetObjectLockConfiguration(ctx, bucket, seaweedfs.ObjectLockConfig{
		Enabled: true,
		Mode:    seaweedfs.ObjectLockMode("BOGUS"),
		Days:    30,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrBadRequest)
}

func TestObjectLock_Set_ZeroDays(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ol-zero-days")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	err = p.SetObjectLockConfiguration(ctx, bucket, seaweedfs.ObjectLockConfig{
		Enabled: true,
		Mode:    seaweedfs.ObjectLockModeGovernance,
		Days:    0,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrBadRequest)
}

func TestObjectLock_Set_DisabledNoOp(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ol-disable")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Disabling on a fresh bucket is a no-op (no daemon call); the
	// Postgres cache is updated by the storage service.
	require.NoError(t, p.SetObjectLockConfiguration(ctx, bucket, seaweedfs.ObjectLockConfig{Enabled: false}))
	// The fake still reports the bucket has no object-lock config.
	got, err := p.GetObjectLockConfiguration(ctx, bucket)
	require.NoError(t, err)
	assert.False(t, got.Enabled)
}

func TestObjectLock_EmitsBusEvent(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ol-emit")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	require.NoError(t, p.SetObjectLockConfiguration(ctx, bucket, seaweedfs.ObjectLockConfig{
		Enabled: true,
		Mode:    seaweedfs.ObjectLockModeGovernance,
		Days:    30,
	}))
	topics := bus.Topics()
	require.Contains(t, topics, "storage.bucket.object_lock.set")
}
