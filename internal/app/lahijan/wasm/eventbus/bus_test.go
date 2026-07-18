// bus_test.go covers the in-process pub/sub + the topic-pattern matcher.
// Tests are pure-Go (no DB) and run as part of the unit suite.

package eventbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopicPattern_Matches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pattern string
		topic   string
		want    bool
	}{
		{"exact", "dns.record.created", "dns.record.created", true},
		{"exact mismatch", "dns.record.created", "dns.record.deleted", false},
		{"prefix wildcard matches child", "dns.record.*", "dns.record.created", true},
		{"prefix wildcard no double match", "dns.record.*", "dns.record.a.b", false},
		{"prefix wildcard empty suffix", "dns.record.*", "dns.record", false},
		{"prefix wildcard different parent", "dns.record.*", "dns.zone.created", false},
		{"scope wildcard matches direct child", "dns.*", "dns.record", true},
		{"scope wildcard not grandchild", "dns.*", "dns.record.created", false},
		{"bare star matches bare topic only", "*", "anything", true},
		{"bare star matches nothing dotted", "*", "dns.record.created", true}, // WS-10b: "*" matches every topic (used by EventService)
		{"empty pattern", "", "anything", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := TopicPattern(tc.pattern).Matches(tc.topic)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMatchSubscription_AlignsWithTopicPattern(t *testing.T) {
	t.Parallel()
	assert.True(t, MatchSubscription("dns.record.*", "dns.record.created"))
	assert.False(t, MatchSubscription("dns.record.*", "dns.zone.created"))
}

func TestBus_EmitDeliversToMatchingListeners(t *testing.T) {
	t.Parallel()
	bus := New(Config{})
	var got []string
	var mu sync.Mutex
	bus.Register("dns.record.*", func(_ context.Context, e Event) error {
		mu.Lock()
		got = append(got, e.Topic)
		mu.Unlock()
		return nil
	})
	bus.Register("compute.instance.*", func(_ context.Context, e Event) error {
		t.Errorf("compute listener should not fire for dns topic")
		return nil
	})

	require.NoError(t, bus.Emit(context.Background(), Event{Topic: "dns.record.created", Metadata: []byte("{}")}))
	require.NoError(t, bus.Emit(context.Background(), Event{Topic: "dns.record.deleted", Metadata: []byte("{}")}))
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"dns.record.created", "dns.record.deleted"}, got)
}

func TestBus_EmitRejectsEmptyTopic(t *testing.T) {
	t.Parallel()
	bus := New(Config{})
	err := bus.Emit(context.Background(), Event{Topic: ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTopicEmpty)
}

func TestBus_EmitRejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	bus := New(Config{MaxPayloadBytes: 8})
	err := bus.Emit(context.Background(), Event{
		Topic:    "dns.record.created",
		Metadata: []byte("this is way too long for 8 bytes"),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPayloadTooLarge)
}

func TestBus_EmitListenerErrorDoesNotStopOthers(t *testing.T) {
	t.Parallel()
	bus := New(Config{})
	var second atomic.Int32
	bus.Register("dns.record.*", func(_ context.Context, _ Event) error {
		return errors.New("boom")
	})
	bus.Register("dns.record.*", func(_ context.Context, _ Event) error {
		second.Add(1)
		return nil
	})
	require.NoError(t, bus.Emit(context.Background(), Event{Topic: "dns.record.created", Metadata: []byte("{}")}))
	assert.Equal(t, int32(1), second.Load(), "second listener must still fire even if first errors")
}

func TestBus_EmitSetsEmittedAtWhenZero(t *testing.T) {
	t.Parallel()
	bus := New(Config{})
	var saw time.Time
	bus.Register("dns.record.*", func(_ context.Context, e Event) error {
		saw = e.EmittedAt
		return nil
	})
	require.NoError(t, bus.Emit(context.Background(), Event{Topic: "dns.record.created", Metadata: []byte("{}")}))
	assert.False(t, saw.IsZero(), "EmittedAt must be populated")
}

func TestBus_UnregisterStopsDelivery(t *testing.T) {
	t.Parallel()
	bus := New(Config{})
	var count atomic.Int32
	id := bus.Register("dns.record.*", func(_ context.Context, _ Event) error {
		count.Add(1)
		return nil
	})
	require.NoError(t, bus.Emit(context.Background(), Event{Topic: "dns.record.created", Metadata: []byte("{}")}))
	bus.Unregister(id)
	require.NoError(t, bus.Emit(context.Background(), Event{Topic: "dns.record.created", Metadata: []byte("{}")}))
	assert.Equal(t, int32(1), count.Load())
}

func TestBus_SetAsyncDispatcher(t *testing.T) {
	t.Parallel()
	bus := New(Config{})
	bus.SetAsyncDispatcher(nil) // must not panic
	assert.True(t, bus.HasListeners() == false, "no listeners registered")
}

func TestIsKnownTopic(t *testing.T) {
	t.Parallel()
	assert.True(t, IsKnownTopic(DNSRecordCreated))
	assert.True(t, IsKnownTopic("customplugin.tick.fired"), "dynamic dotted topic accepted")
	assert.False(t, IsKnownTopic("noDots"))
}
