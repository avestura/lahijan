// Package incus: provider_test.go covers the Provider interface methods
// (Name, Ping, Capabilities) against the fake Incus server.
package incus_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectProvider builds an *incus.Provider pointed at the fake server. Tests
// reuse this so the wiring stays consistent.
func connectProvider(t *testing.T, srv *fake.Server) *incus.Provider {
	t.Helper()
	cli := &http.Client{Timeout: 5 * time.Second}
	p, err := incus.NewClient(incus.Config{
		HTTPClient:      cli,
		BaseURL:         srv.HTTP.URL,
		ProjectPrefix:   "lahijan-tenant-",
		ProjectFeatures: allFeaturesOn(),
	})
	require.NoError(t, err)
	return p
}

func TestProvider_Name(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)
	assert.Equal(t, "incus", p.Name(), "Name must be the internal identifier")
}

func TestProvider_Ping_Success(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.Ping(ctx), "Ping against the fake must succeed")
}

func TestProvider_Ping_Unreachable(t *testing.T) {
	t.Parallel()
	// Point at an unreachable port. The fake's httptest server is not used;
	// we craft a provider manually to bypass it.
	cli := &http.Client{Timeout: 200 * time.Millisecond}
	p, err := incus.NewClient(incus.Config{
		HTTPClient:     cli,
		BaseURL:        "http://127.0.0.1:1", // port 1 is reserved + unreachable
		RequestTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = p.Ping(ctx)
	require.Error(t, err, "Ping against an unreachable daemon must fail")
}

func TestProvider_Capabilities_Cached(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.SetServerClustered(true)
	srv.SetServerVersion("6.5.0-fake")
	p := connectProvider(t, srv)

	// First call triggers Ping; the cache is then populated.
	caps := p.Capabilities()
	assert.True(t, caps.ClusterMode, "Capabilities should reflect cluster mode")
	assert.Equal(t, "6.5.0-fake", caps.ServerVersion)
	assert.Equal(t, "1.0", caps.APIVersion)

	// Flip the server-side state; Capabilities must still return the cached
	// value because the cache is only refreshed by an explicit Ping.
	srv.SetServerClustered(false)
	caps2 := p.Capabilities()
	assert.True(t, caps2.ClusterMode, "Capabilities must be cached between calls")
}

func TestProvider_Capabilities_ZeroBeforePing(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)
	// Reading Capabilities when the cache is empty triggers an internal Ping.
	// Here we just confirm it returns a non-zero ServerVersion after the call.
	caps := p.Capabilities()
	assert.NotEmpty(t, caps.ServerVersion, "Capabilities should be populated after the implicit Ping")
}
