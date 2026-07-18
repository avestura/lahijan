// Package seaweedfs_test: provider_test.go covers the Provider interface
// methods (Name, Ping, Capabilities) against the in-memory fake. Also
// exercises the NewClient validation paths.
package seaweedfs_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_Name(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	assert.Equal(t, "seaweedfs", p.Name(), "Name must be the internal identifier")
}

func TestNewClient_Validation(t *testing.T) {
	t.Parallel()
	t.Run("missing HTTPClient", func(t *testing.T) {
		t.Parallel()
		_, err := seaweedfs.NewClient(seaweedfs.Config{
			S3Endpoint: "http://x", FilerURL: "http://f",
			AdminAccessKey: "k", AdminSecretKey: "s",
		})
		require.Error(t, err)
	})
	t.Run("missing admin access key", func(t *testing.T) {
		t.Parallel()
		_, err := seaweedfs.NewClient(seaweedfs.Config{
			HTTPClient: &http.Client{}, S3Endpoint: "http://x",
			FilerURL: "http://f", AdminSecretKey: "s",
		})
		require.Error(t, err)
	})
	t.Run("missing admin secret key", func(t *testing.T) {
		t.Parallel()
		_, err := seaweedfs.NewClient(seaweedfs.Config{
			HTTPClient: &http.Client{}, S3Endpoint: "http://x",
			FilerURL: "http://f", AdminAccessKey: "k",
		})
		require.Error(t, err)
	})
}

func TestProvider_Ping_Success(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.Ping(ctx), "Ping against the fake must succeed")
}

func TestProvider_Ping_S3Fail(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	srv.SetPingerFailures(false, true)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.Ping(ctx)
	require.Error(t, err, "Ping against a failing S3 fake must fail")
}

func TestProvider_Ping_FilerFail(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	srv.SetPingerFailures(true, false)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.Ping(ctx)
	require.Error(t, err, "Ping against a failing Filer fake must fail")
}

func TestProvider_Capabilities_Cached(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	srv.SetFilerVersion("3.99.0-fake")
	p := connectProvider(t, srv)

	// First call triggers Ping; the cache is then populated.
	caps := p.Capabilities()
	assert.Equal(t, "3.99.0-fake", caps.ServerVersion)
	assert.True(t, caps.QuotasEnforced, "fake must report quota enforcement")
	assert.True(t, caps.PresignSupported)
	assert.False(t, caps.ClusterMode, "fake default is single-node `weed mini`")

	// Flip the server-side state; Capabilities must still return the
	// cached value because the cache is only refreshed by an explicit Ping.
	srv.SetFilerVersion("3.0.0-fake")
	caps2 := p.Capabilities()
	assert.Equal(t, "3.99.0-fake", caps2.ServerVersion, "Capabilities must be cached between calls")
}

func TestProvider_Capabilities_ClusterMode(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	srv.SetClusterMode(true)
	p := connectProvider(t, srv)
	caps := p.Capabilities()
	assert.True(t, caps.ClusterMode, "cluster-mode fake must report ClusterMode=true")
}

func TestProvider_Capabilities_PopulatedAfterImplicitPing(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	caps := p.Capabilities()
	assert.NotEmpty(t, caps.ServerVersion, "Capabilities should be populated after the implicit Ping")
}

func TestProvider_GetClusterStatus(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := p.GetClusterStatus(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, info.Version)
	assert.True(t, info.TotalBytes > 0, "fake reports non-zero capacity")
	assert.True(t, info.FreeBytes > 0)
	assert.GreaterOrEqual(t, info.VolumeCount, 1)
}

func TestProvider_PingFiler(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.PingFiler(ctx))
}
