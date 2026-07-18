// Package incus: events_test.go covers the websocket events listener. The
// fake server broadcasts lifecycle events via EmitLifecycleEvent; the test
// asserts that the driver fans them into the WASM bus under the canonical
// topics ("compute.instance.stopped", etc.).
package incus_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingBus is a test-only incus.EventBus that records every Emit call.
// Safe for concurrent use; the read loop pumps events from a goroutine.
type recordingBus struct {
	mu     sync.Mutex
	events []incus.BusEvent
	notify chan struct{}
}

func newRecordingBus() *recordingBus {
	return &recordingBus{notify: make(chan struct{}, 256)}
}

// Emit implements incus.EventBus.
func (r *recordingBus) Emit(_ context.Context, e incus.BusEvent) error {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return nil
}

func (r *recordingBus) Snapshot() []incus.BusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]incus.BusEvent, len(r.events))
	copy(out, r.events)
	return out
}

func TestEventListener_LifecycleEvents_FanIntoBus(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	bus := newRecordingBus()

	cli := newHTTPClient()
	p, err := incus.NewClient(incus.Config{
		HTTPClient:      cli,
		BaseURL:         srv.HTTP.URL,
		ProjectPrefix:   "lahijan-tenant-",
		ProjectFeatures: allFeaturesOn(),
		Bus:             bus,
	})
	require.NoError(t, err)

	listener, err := p.StartEventListener(incus.EventListenerConfig{
		Bus:             bus,
		MaxReconnect:    1 * time.Second,
		MaxPayloadBytes: 1 << 20,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	// Wait for the listener to register with the fake by polling for a
	// connection.
	require.Eventually(t, func() bool {
		return srv.HasEventsListener()
	}, 3*time.Second, 50*time.Millisecond, "listener must connect to the fake")

	// Emit three lifecycle events; the driver should translate each into a
	// canonical topic on the bus.
	tenantID := uuid.New()
	src := "/1.0/instances/web-1?project=lahijan-tenant-" + tenantID.String()
	srv.EmitLifecycleEvent("instance-started", src)
	srv.EmitLifecycleEvent("instance-stopped", src)
	srv.EmitLifecycleEvent("instance-restarted", src)

	require.Eventually(t, func() bool {
		return len(bus.Snapshot()) >= 3
	}, 3*time.Second, 50*time.Millisecond, "all three events must reach the bus")

	events := bus.Snapshot()
	require.GreaterOrEqual(t, len(events), 3)
	wantTopics := map[string]bool{
		"compute.instance.started":   false,
		"compute.instance.stopped":   false,
		"compute.instance.restarted": false,
	}
	for _, e := range events {
		if _, ok := wantTopics[e.Topic]; ok {
			wantTopics[e.Topic] = true
		}
		assert.Equal(t, "system", e.ActorType, "actor type must be system for daemon events")
		if e.TenantID != nil {
			assert.Equal(t, tenantID.String(), *e.TenantID,
				"tenant id must be decoded from the project name")
		}
	}
	for topic, seen := range wantTopics {
		assert.True(t, seen, "topic %q must reach the bus", topic)
	}
}

func TestEventListener_UnknownActions_NotForwarded(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	bus := newRecordingBus()

	cli := newHTTPClient()
	p, err := incus.NewClient(incus.Config{
		HTTPClient:      cli,
		BaseURL:         srv.HTTP.URL,
		ProjectPrefix:   "lahijan-tenant-",
		ProjectFeatures: allFeaturesOn(),
		Bus:             bus,
	})
	require.NoError(t, err)

	listener, err := p.StartEventListener(incus.EventListenerConfig{Bus: bus})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	require.Eventually(t, func() bool {
		return srv.HasEventsListener()
	}, 3*time.Second, 50*time.Millisecond)

	before := len(bus.Snapshot())
	// image-updated is NOT in our canonical action map.
	srv.EmitLifecycleEvent("image-updated", "/1.0/images/abc")
	time.Sleep(200 * time.Millisecond) // give the listener time to drop it

	assert.Equal(t, before, len(bus.Snapshot()),
		"non-canonical actions must not be forwarded to the bus")
}

func TestEventListener_CloseIsIdempotent(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	bus := newRecordingBus()
	cli := newHTTPClient()
	p, err := incus.NewClient(incus.Config{
		HTTPClient: cli, BaseURL: srv.HTTP.URL, Bus: bus,
	})
	require.NoError(t, err)

	listener, err := p.StartEventListener(incus.EventListenerConfig{Bus: bus})
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	require.NoError(t, listener.Close(), "double-close must not error")
}
