// Package powerdns_test: metadata_test.go covers the per-zone metadata
// helpers against the fake PowerDNS server.
package powerdns_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadata_PutGetDelete(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "meta.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	// SOA-EDIT set + get round-trip.
	require.NoError(t, p.SetSOAEdit(ctx, z.ID, "EPOCH"))
	got, err := p.GetMetadataKind(ctx, z.ID, "SOA-EDIT")
	require.NoError(t, err)
	assert.Equal(t, "SOA-EDIT", got.Kind)
	assert.Equal(t, []string{"EPOCH"}, got.Metadata)

	// List shows the entry.
	list, err := p.GetMetadata(ctx, z.ID)
	require.NoError(t, err)
	require.NotEmpty(t, list)
	assert.Equal(t, "SOA-EDIT", list[0].Kind)

	// Delete clears it.
	require.NoError(t, p.DeleteMetadata(ctx, z.ID, "SOA-EDIT"))
	_, err = p.GetMetadataKind(ctx, z.ID, "SOA-EDIT")
	require.Error(t, err, "deleted metadata must 404")
}

func TestMetadata_PutEmptyDeletes(t *testing.T) {
	t.Parallel()
	srv := newFake(t)
	p := connectProvider(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	z, err := p.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        "meta-empty.example.com.",
		Nameservers: []string{"ns1.example.com."},
	})
	require.NoError(t, err)

	require.NoError(t, p.SetSOAEdit(ctx, z.ID, "INCEPTION"))
	// PDNS convention: an empty value list removes the entry.
	require.NoError(t, p.PutMetadata(ctx, z.ID, powerdns.Metadata{
		Kind:     "SOA-EDIT",
		Metadata: nil,
	}))
	_, err = p.GetMetadataKind(ctx, z.ID, "SOA-EDIT")
	require.Error(t, err, "PutMetadata with empty value must remove the entry")
}
