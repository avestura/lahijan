// Package powerdns_test: provider_test.go covers the Provider interface
// methods (Name, Ping, Capabilities) against the fake PowerDNS server.
package powerdns_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_Name(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	assert.Equal(t, "powerdns", p.Name(), "Name must be the internal identifier")
}

func TestNewClient_Validation(t *testing.T) {
	t.Parallel()
	t.Run("missing HTTPClient", func(t *testing.T) {
		t.Parallel()
		_, err := powerdns.NewClient(powerdns.Config{
			BaseURL: "http://x",
			APIKey:  "k",
		})
		require.Error(t, err)
	})
	t.Run("missing BaseURL", func(t *testing.T) {
		t.Parallel()
		_, err := powerdns.NewClient(powerdns.Config{
			HTTPClient: &http.Client{},
			APIKey:     "k",
		})
		require.Error(t, err)
	})
	t.Run("missing APIKey", func(t *testing.T) {
		t.Parallel()
		_, err := powerdns.NewClient(powerdns.Config{
			HTTPClient: &http.Client{},
			BaseURL:    "http://x",
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

func TestProvider_Ping_Unreachable(t *testing.T) {
	t.Parallel()
	cli := &http.Client{Timeout: 200 * time.Millisecond}
	p, err := powerdns.NewClient(powerdns.Config{
		HTTPClient:     cli,
		BaseURL:        "http://127.0.0.1:1", // port 1 is reserved + unreachable
		APIKey:         "test-key",
		RequestTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err = p.Ping(ctx)
	require.Error(t, err, "Ping against an unreachable daemon must fail")
}

func TestProvider_Ping_BadAPIKey(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	cli := &http.Client{Timeout: 5 * time.Second}
	p, err := powerdns.NewClient(powerdns.Config{
		HTTPClient: cli,
		BaseURL:    srv.HTTP.URL,
		APIKey:     "wrong-key",
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = p.Ping(ctx)
	require.Error(t, err, "Ping with a wrong API key must fail")
}

func TestProvider_Ping_RecursorRejected(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	srv.SetServerDaemonType("recursor")
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.Ping(ctx)
	require.Error(t, err, "Ping against a non-authoritative daemon must fail")
}

func TestProvider_Capabilities_Cached(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.SetServerVersion("4.10.0-fake")
	p := connectProvider(t, srv)

	// First call triggers Ping; the cache is then populated.
	caps := p.Capabilities()
	assert.Equal(t, "4.10.0-fake", caps.ServerVersion)
	assert.Equal(t, "authoritative", caps.DaemonType)
	assert.True(t, caps.DNSSECSupported, "PDNS 4.x+ must report DNSSEC supported")

	// Flip the server-side state; Capabilities must still return the cached
	// value because the cache is only refreshed by an explicit Ping.
	srv.SetServerVersion("4.99.0-fake")
	caps2 := p.Capabilities()
	assert.Equal(t, "4.10.0-fake", caps2.ServerVersion, "Capabilities must be cached between calls")
}

func TestProvider_Capabilities_PopulatedAfterImplicitPing(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	// Reading Capabilities when the cache is empty triggers an internal Ping.
	caps := p.Capabilities()
	assert.NotEmpty(t, caps.ServerVersion, "Capabilities should be populated after the implicit Ping")
}
