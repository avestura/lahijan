// Package incus_test: helpers_test.go holds shared test helpers (provider
// construction, fake-server bootstrap) so individual test files stay terse.
package incus_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus/fake"
)

// newHTTPClient returns the *http.Client the tests use to talk to the fake
// server. A short per-request timeout surfaces deadlocks quickly.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// newFakeWithDefaults boots a fake Incus server with the WS-11 default config
// (restricted-project defaults, all features.* on). Each test gets a fresh
// fake; t.Cleanup closes the underlying httptest.Server.
func newFakeWithDefaults(t *testing.T) *fake.Server {
	t.Helper()
	srv := fake.NewServer(t)
	return srv
}

// allFeaturesOn is the default ProjectFeatures the tests use. Mirrors the
// defaults baked into conf.providers.incus.projectFeatures.*.
func allFeaturesOn() incus.ProjectFeatures {
	return incus.ProjectFeatures{
		Images:         true,
		Profiles:       true,
		Networks:       true,
		StorageVolumes: true,
		StorageBuckets: true,
	}
}
