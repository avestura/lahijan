package seaweedfs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
)

// TestBucketCORS_AppliedOnCreateAndBackfilled guards browser uploads: a
// bucket without a CORS configuration answers the presigned-URL preflight
// with 403, so every bucket must carry the dashboard origin rule.
func TestBucketCORS_AppliedOnCreateAndBackfilled(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A bucket that exists before CORS is configured.
	plain := connectProvider(t, srv)
	old := mustBucketName(t, "legacy")
	_, err := plain.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: old, TenantID: validTenantUUID})
	require.NoError(t, err)
	assert.Nil(t, srv.BucketCORS(old), "no origins configured -> no CORS written")

	p, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient: newHTTPClient(), S3: srv, Presign: srv, Filer: srv,
		S3Endpoint: "https://fake-s3.example", FilerURL: "https://fake-filer.example",
		AdminAccessKey: "k", AdminSecretKey: "s", RequestTimeout: 5 * time.Second,
		CORSAllowedOrigins: []string{"https://app.example.com"},
	})
	require.NoError(t, err)

	fresh := mustBucketName(t, "fresh")
	_, err = p.CreateBucket(ctx, seaweedfs.CreateBucketParams{Bucket: fresh, TenantID: validTenantUUID})
	require.NoError(t, err)
	cfg := srv.BucketCORS(fresh)
	require.NotNil(t, cfg, "create applies CORS")
	require.Len(t, cfg.CORSRules, 1)
	assert.Equal(t, []string{"https://app.example.com"}, cfg.CORSRules[0].AllowedOrigins)
	assert.Contains(t, cfg.CORSRules[0].AllowedMethods, "PUT")

	require.NoError(t, p.EnsureBucketCORS(ctx))
	assert.NotNil(t, srv.BucketCORS(old), "boot backfill covers pre-existing buckets")
}
