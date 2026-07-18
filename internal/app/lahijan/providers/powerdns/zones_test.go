// Package powerdns_test: zones_test.go covers zone CRUD + AXFR toggle
// against the fake PowerDNS server.
package powerdns_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZone_CreateAndGet(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "example.com.",
		Kind:        powerdns.ZoneKindNative,
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)
	assert.Equal(t, "example.com.", z.ID)
	assert.Equal(t, "example.com.", z.Name)
	assert.Equal(t, "Native", z.Kind)

	got, err := p.GetZone(ctx, z.ID, false)
	require.NoError(t, err)
	assert.Equal(t, z.ID, got.ID)
	assert.Equal(t, "Native", got.Kind)
	assert.Equal(t, int64(1), got.Serial, "freshly-created zone must have serial 1")
}

func TestZone_Create_ValidationErrors(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name   string
		params powerdns.CreateZoneParams
	}{
		{
			name: "missing trailing dot",
			params: powerdns.CreateZoneParams{
				Name:        "no-dot.example.com",
				Nameservers: []string{"ns1.example.com."},
			},
		},
		{
			name: "uppercase name",
			params: powerdns.CreateZoneParams{
				Name:        "UPPER.Example.com.",
				Nameservers: []string{"ns1.example.com."},
			},
		},
		{
			name: "no nameservers",
			params: powerdns.CreateZoneParams{
				Name: "no-ns.example.com.",
			},
		},
		{
			name: "empty name",
			params: powerdns.CreateZoneParams{
				Nameservers: []string{"ns1.example.com."},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.CreateZone(ctx, tc.params)
			require.Error(t, err, "%s must fail validation", tc.name)
		})
	}
}

func TestZone_Create_AlreadyExists(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	params := powerdns.CreateZoneParams{
		Name:        "dupe.example.com.",
		Nameservers: []string{"ns1.example.com."},
	}
	_, err := p.CreateZone(ctx, params)
	require.NoError(t, err)

	_, err = p.CreateZone(ctx, params)
	require.Error(t, err, "creating a duplicate zone must fail")
	assert.True(t, errors.Is(err, powerdns.ErrAlreadyExists),
		"duplicate zone must surface as ErrAlreadyExists, got: %v", err)
}

func TestZone_List(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, name := range []string{"a.example.com.", "b.example.com.", "c.example.com."} {
		_, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
			Name:        name,
			Nameservers: []string{"ns1.example.com."},
		})
		require.NoError(t, err)
	}
	zones, err := p.ListZones(ctx)
	require.NoError(t, err)
	require.Len(t, zones, 3, "ListZones must return every zone")
}

func TestZone_Delete(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "deleteme.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	require.NoError(t, p.DeleteZone(ctx, z.ID))

	_, err = p.GetZone(ctx, z.ID, false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, powerdns.ErrNotFound),
		"deleted zone GET must surface ErrNotFound, got: %v", err)
}

func TestZone_Update_BumpsSerial(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "bump.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), z.Serial)

	require.NoError(t, p.UpdateZone(ctx, z.ID, powerdns.ZoneUpdate{
		SOAEditAPI: "EPOCH",
	}))

	got, err := p.GetZone(ctx, z.ID, false)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got.Serial, "PATCH must bump the SOA serial")
	assert.Equal(t, "EPOCH", got.SOAEditAPI)
}

func TestZone_AXFR_DefaultsOff(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "axfr.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	// Disabling AXFR on a fresh zone is a no-op success.
	require.NoError(t, p.SetAXFR(ctx, z.ID, false, nil))

	// Enabling AXFR for a list of peers sets the metadata.
	require.NoError(t, p.SetAXFR(ctx, z.ID, true, []string{"192.0.2.1", "192.0.2.2"}))

	meta, err := p.GetMetadataKind(ctx, z.ID, "ALLOW-AXFR-FROM")
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, []string{"192.0.2.1", "192.0.2.2"}, meta.Metadata)

	// Disabling again clears the metadata.
	require.NoError(t, p.SetAXFR(ctx, z.ID, false, nil))
	_, err = p.GetMetadataKind(ctx, z.ID, "ALLOW-AXFR-FROM")
	require.Error(t, err, "disabling AXFR must remove the metadata entry")
}

func TestZone_GetWithRRsets(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "with-rrsets.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	// GetZone without the rrsets flag must NOT populate RRsets.
	got, err := p.GetZone(ctx, z.ID, false)
	require.NoError(t, err)
	assert.Empty(t, got.RRsets, "GetZone without rrsets=true must omit RRsets")

	// GetZone with rrsets=true must include the SOA + NS bootstrapped at
	// create time.
	got, err = p.GetZone(ctx, z.ID, true)
	require.NoError(t, err)
	types := make(map[string]bool)
	for _, rr := range got.RRsets {
		types[rr.Type] = true
	}
	assert.True(t, types["SOA"], "bootstrapped zone must contain a SOA")
	assert.True(t, types["NS"], "bootstrapped zone must contain an NS")
}
