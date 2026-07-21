// Package seaweedfs_test: versioning_test.go covers the WS-29 bucket-
// versioning surface (SetBucketVersioning / GetBucketVersioning /
// ListObjectVersions / RestoreObjectVersion) against the in-memory
// fake. Per ADR-0027 the test boundary is the operations interface,
// not the S3 wire.
package seaweedfs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
)

func TestVersioning_SetGet(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ver-setget")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Fresh bucket is unversioned.
	got, err := p.GetBucketVersioning(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.VersioningStatusUnversioned, got)

	// Enable versioning.
	require.NoError(t, p.SetBucketVersioning(ctx, bucket, seaweedfs.VersioningStatusEnabled))
	got, err = p.GetBucketVersioning(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.VersioningStatusEnabled, got)

	// Suspend.
	require.NoError(t, p.SetBucketVersioning(ctx, bucket, seaweedfs.VersioningStatusSuspended))
	got, err = p.GetBucketVersioning(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.VersioningStatusSuspended, got)

	// Unversioned (sentinel -> SDK omits the field).
	require.NoError(t, p.SetBucketVersioning(ctx, bucket, seaweedfs.VersioningStatusUnversioned))
	got, err = p.GetBucketVersioning(ctx, bucket)
	require.NoError(t, err)
	assert.Equal(t, seaweedfs.VersioningStatusUnversioned, got)
}

func TestVersioning_Set_InvalidBucket(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.SetBucketVersioning(ctx, "UPPER-CASE-INVALID", seaweedfs.VersioningStatusEnabled)
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrInvalidBucketName)
}

func TestVersioning_Set_NotFound(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "missing")
	err := p.SetBucketVersioning(ctx, bucket, seaweedfs.VersioningStatusEnabled)
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrNotFound)
}

func TestVersioning_EmitsBusEvent(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ver-emit")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	require.NoError(t, p.SetBucketVersioning(ctx, bucket, seaweedfs.VersioningStatusEnabled))
	topics := bus.Topics()
	require.Contains(t, topics, "storage.bucket.versioning.set",
		"expected storage.bucket.versioning.set event after SetBucketVersioning")
}

func TestVersioning_ListObjectVersions(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ver-list")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Enable versioning so the fake records versions.
	require.NoError(t, p.SetBucketVersioning(ctx, bucket, seaweedfs.VersioningStatusEnabled))

	out, err := p.ListObjectVersions(ctx, bucket, "", "", "", 0)
	require.NoError(t, err)
	require.NotNil(t, out)
	// Fresh bucket: empty page.
	assert.Empty(t, out.Versions)
	assert.Empty(t, out.DeleteMarkers)
}

func TestVersioning_RestoreObjectVersion_MissingKey(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bucket := mustBucketName(t, "ver-restore-missing")
	_, err := p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: bucket})
	require.NoError(t, err)

	// Empty key is rejected before the SDK call.
	err = p.RestoreObjectVersion(ctx, bucket, "", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, seaweedfs.ErrBadRequest)
}
