// Package seaweedfs_test: lifecycle_test.go covers the WS-29 bucket-
// lifecycle surface (SetBucketLifecycle / GetBucketLifecycle) against
// the in-memory fake. Per ADR-0027 the test boundary is the operations
// interface, not the S3 wire.
package seaweedfs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
)

func TestLifecycle_SetGetEmpty(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "lc-empty")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Get on a fresh bucket returns an empty slice, not an error.
	rules, err := p.GetBucketLifecycle(ctx, bucket)
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestLifecycle_SetGetRules(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "lc-set")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	in := []seaweedfs.LifecycleRule{
		{
			ID:      "expire-logs",
			Enabled: true,
			Action:  seaweedfs.LifecycleActionExpiration,
			Days:    30,
			Prefix:  "logs/",
		},
		{
			ID:      "abort-multipart",
			Enabled: true,
			Action:  seaweedfs.LifecycleActionAbortIncompleteMultipart,
			Days:    7,
		},
	}
	require.NoError(t, p.SetBucketLifecycle(ctx, bucket, in))
	out, err := p.GetBucketLifecycle(ctx, bucket)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "expire-logs", out[0].ID)
	assert.True(t, out[0].Enabled)
	assert.Equal(t, "abort-multipart", out[1].ID)
}

func TestLifecycle_SetEmptyClearsPolicy(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "lc-clear")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Set one rule.
	require.NoError(t, p.SetBucketLifecycle(ctx, bucket, []seaweedfs.LifecycleRule{
		{ID: "x", Enabled: true, Action: seaweedfs.LifecycleActionExpiration, Days: 1},
	}))
	// Clear via empty rule set.
	require.NoError(t, p.SetBucketLifecycle(ctx, bucket, nil))
	rules, err := p.GetBucketLifecycle(ctx, bucket)
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestLifecycle_Set_InvalidBucket(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.SetBucketLifecycle(ctx, "UPPER", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrInvalidBucketName)
}

func TestLifecycle_Set_NotFound(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "lc-missing")
	err := p.SetBucketLifecycle(ctx, bucket, []seaweedfs.LifecycleRule{
		{ID: "x", Enabled: true, Action: seaweedfs.LifecycleActionExpiration, Days: 1},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrNotFound)
}

func TestLifecycle_EmitsBusEvent(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "lc-emit")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	require.NoError(t, p.SetBucketLifecycle(ctx, bucket, []seaweedfs.LifecycleRule{
		{ID: "x", Enabled: true, Action: seaweedfs.LifecycleActionExpiration, Days: 1},
	}))
	topics := bus.Topics()
	require.Contains(t, topics, "storage.bucket.lifecycle.set")
}
