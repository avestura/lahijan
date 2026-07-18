// Package powerdns_test: events_test.go covers the change-event synthesis
// path. PDNS does not push events; the driver emits one on every mutating
// call so the WASM bus sees a uniform stream. The test asserts the topic
// names + the basic payload shape for each mutation.
package powerdns_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvents_ZoneCreatedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "evt.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(bus.Snapshot()) >= 1 },
		2*time.Second, 25*time.Millisecond)
	events := bus.Snapshot()
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, "dns.zone.created", last.Topic)
	assert.NotNil(t, last.ResourceID)
	assert.Equal(t, z.ID, *last.ResourceID)
	assert.Equal(t, "system", last.ActorType)
}

func TestEvents_ZoneDeletedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "evt-del.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)
	require.NoError(t, p.DeleteZone(ctx, z.ID))
	require.Eventually(t, func() bool {
		return len(bus.Topics()) >= 2
	}, 2*time.Second, 25*time.Millisecond)
	topics := bus.Topics()
	assert.Equal(t, "dns.zone.created", topics[0])
	assert.Equal(t, "dns.zone.deleted", topics[1])
}

func TestEvents_RecordUpdatedAndDeletedEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "evt-rec.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	name := "www.evt-rec.example.com."
	require.NoError(t, p.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID: z.ID, Name: name, Type: powerdns.TypeA, TTL: 60,
		Records: []powerdns.Record{{Content: "192.0.2.1"}},
	}))
	require.NoError(t, p.DeleteRRset(ctx, powerdns.DeleteRRsetParams{
		ZoneID: z.ID, Name: name, Type: powerdns.TypeA,
	}))

	require.Eventually(t, func() bool {
		for _, t := range bus.Topics() {
			if t == "dns.record.updated" {
				return true
			}
		}
		return false
	}, 2*time.Second, 25*time.Millisecond)

	topics := bus.Topics()
	want := map[string]bool{
		"dns.zone.created":   false,
		"dns.record.updated": false,
		"dns.record.deleted": false,
	}
	for _, topic := range topics {
		if _, ok := want[topic]; ok {
			want[topic] = true
		}
	}
	for topic, seen := range want {
		assert.True(t, seen, "expected topic %q in emissions", topic)
	}
}

func TestEvents_NoBus_IsSilentNoOp(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	// Provider built without a bus — the emit path must not panic and must
	// not block the request.
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "no-bus.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err, "CreateZone must succeed with no bus attached")
}

func TestEvents_DNSSECEnabledEmits(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	bus := newRecordingBus()
	p := connectProviderWithBus(t, srv, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "evt-dnssec.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	_, err = p.EnableDNSSEC(ctx, z.ID)
	require.NoError(t, err)
	require.NoError(t, p.DisableDNSSEC(ctx, z.ID))

	require.Eventually(t, func() bool {
		topics := bus.Topics()
		hasEnabled := false
		hasDisabled := false
		for _, topic := range topics {
			if topic == "dns.zone.dnssec.enabled" {
				hasEnabled = true
			}
			if topic == "dns.zone.dnssec.disabled" {
				hasDisabled = true
			}
		}
		return hasEnabled && hasDisabled
	}, 2*time.Second, 25*time.Millisecond, "must emit both enabled and disabled events")
}
