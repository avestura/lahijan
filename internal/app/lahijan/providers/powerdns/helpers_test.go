// Package powerdns_test: helpers_test.go holds shared test helpers (provider
// construction, fake-server bootstrap, recording event bus) so individual
// test files stay terse.
package powerdns_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns/fake"
)

// newHTTPClient returns the *http.Client the tests use to talk to the fake
// server. A short per-request timeout surfaces deadlocks quickly.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// newFake boots a fake PowerDNS server with the WS-12 default config. Each
// test gets a fresh fake; t.Cleanup closes the underlying httptest.Server.
func newFake(t *testing.T) *fake.Server {
	t.Helper()
	return fake.NewServer(t)
}

// connectProvider builds an *powerdns.Provider pointed at the fake server.
// Tests reuse this so the wiring stays consistent.
func connectProvider(t *testing.T, srv *fake.Server) *powerdns.Provider {
	t.Helper()
	p, err := powerdns.NewClient(powerdns.Config{
		HTTPClient: newHTTPClient(),
		BaseURL:    srv.HTTP.URL,
		APIKey:     srv.APIKey,
	})
	if err != nil {
		t.Fatalf("connectProvider: %v", err)
	}
	return p
}

// connectProviderWithBus wires a provider + a recordingBus so event-synthesis
// tests can assert the topic + payload shape.
func connectProviderWithBus(t *testing.T, srv *fake.Server, bus powerdns.EventBus) *powerdns.Provider {
	t.Helper()
	p, err := powerdns.NewClient(powerdns.Config{
		HTTPClient: newHTTPClient(),
		BaseURL:    srv.HTTP.URL,
		APIKey:     srv.APIKey,
		Bus:        bus,
	})
	if err != nil {
		t.Fatalf("connectProviderWithBus: %v", err)
	}
	return p
}

// recordingBus is a test-only powerdns.EventBus that records every Emit call.
// Safe for concurrent use; emitChange pumps events synchronously from the
// request goroutine.
type recordingBus struct {
	mu     sync.Mutex
	events []powerdns.BusEvent
	notify chan struct{}
}

func newRecordingBus() *recordingBus {
	return &recordingBus{notify: make(chan struct{}, 256)}
}

// Emit implements powerdns.EventBus.
func (r *recordingBus) Emit(_ context.Context, e powerdns.BusEvent) error {
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
func (r *recordingBus) Snapshot() []powerdns.BusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]powerdns.BusEvent, len(r.events))
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
