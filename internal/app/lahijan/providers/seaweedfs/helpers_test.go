// Package seaweedfs_test: helpers_test.go holds shared test helpers (provider
// construction, fake-server bootstrap, recording event bus) so individual
// test files stay terse.
package seaweedfs_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs/fake"
)

// newFake boots a fresh in-memory SeaweedFS fake for one test. t.Cleanup
// is the caller's responsibility (the fake has no system resources to
// release; the helper exists for shape-uniformity with WS-11/WS-12).
func newFake(t *testing.T) *fake.Server {
	t.Helper()
	return fake.NewServer(t)
}

// newHTTPClient returns the *http.Client the tests hand to NewClient as
// its Filer transport. A short per-request timeout surfaces deadlocks
// quickly.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// connectProvider builds a *seaweedfs.Provider wired to the in-memory
// fake for all three of S3 / Presign / Filer. Tests reuse this so the
// wiring stays consistent.
func connectProvider(t *testing.T, srv *fake.Server) *seaweedfs.Provider {
	t.Helper()
	p, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient:      newHTTPClient(),
		S3:              srv,
		Presign:         srv,
		Filer:           srv,
		S3Endpoint:      "https://fake-s3.example",
		FilerURL:        "https://fake-filer.example",
		Region:          "us-east-1",
		AdminAccessKey:  "lahijan-dev-admin-key",
		AdminSecretKey:  "lahijan-dev-admin-secret",
		RequestTimeout:  5 * time.Second,
		DefaultPresignTTL: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("connectProvider: %v", err)
	}
	return p
}

// connectProviderWithBus wires a provider + a recordingBus so
// event-synthesis tests can assert the topic + payload shape.
func connectProviderWithBus(t *testing.T, srv *fake.Server, bus seaweedfs.EventBus) *seaweedfs.Provider {
	t.Helper()
	p, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient:      newHTTPClient(),
		S3:              srv,
		Presign:         srv,
		Filer:           srv,
		S3Endpoint:      "https://fake-s3.example",
		FilerURL:        "https://fake-filer.example",
		Region:          "us-east-1",
		AdminAccessKey:  "lahijan-dev-admin-key",
		AdminSecretKey:  "lahijan-dev-admin-secret",
		RequestTimeout:  5 * time.Second,
		DefaultPresignTTL: 60 * time.Second,
		Bus:             bus,
	})
	if err != nil {
		t.Fatalf("connectProviderWithBus: %v", err)
	}
	return p
}

// recordingBus is a test-only seaweedfs.EventBus that records every
// Emit call. Safe for concurrent use; emitChange pumps events
// synchronously from the request goroutine.
type recordingBus struct {
	mu     sync.Mutex
	events []seaweedfs.BusEvent
	notify chan struct{}
}

func newRecordingBus() *recordingBus {
	return &recordingBus{notify: make(chan struct{}, 256)}
}

// Emit implements seaweedfs.EventBus.
func (r *recordingBus) Emit(_ context.Context, e seaweedfs.BusEvent) error {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return nil
}

// Snapshot returns a copy of the recorded events.
func (r *recordingBus) Snapshot() []seaweedfs.BusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]seaweedfs.BusEvent, len(r.events))
	copy(out, r.events)
	return out
}

// Topics returns the unique topic list in order of first emission.
func (r *recordingBus) Topics() []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, e := range r.Snapshot() {
		if !seen[e.Topic] {
			seen[e.Topic] = true
			out = append(out, e.Topic)
		}
	}
	return out
}

// validTenantUUID is a stable tenant UUID used across the test suite. The
// canonical bucket name format is "<tenant-uuid>-<slug>"; we hard-code the
// tenant so the assertions on the compound name are deterministic.
const validTenantUUID = "11111111-2222-3333-4444-555555555555"

// mustBucketName composes a canonical bucket name from the test tenant +
// slug, failing the test on validation error.
func mustBucketName(t *testing.T, slug string) string {
	t.Helper()
	name, err := seaweedfs.BucketName(validTenantUUID, slug)
	if err != nil {
		t.Fatalf("BucketName(%q): %v", slug, err)
	}
	return name
}
