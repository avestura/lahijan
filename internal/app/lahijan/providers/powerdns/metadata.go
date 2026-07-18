// Package powerdns: metadata.go wraps the PDNS zone-metadata API. PDNS
// exposes per-zone metadata as a key → []string map; the driver surfaces it
// as typed Get/Put/Delete helpers.
//
// Common metadata kinds Lahijan uses:
//
//   - "SOA-EDIT"          — controls how the SOA serial is bumped
//     ("DEFAULT", "INCEPTION", "EPOCH", "INCEPTION-WEEK").
//   - "SOA-EDIT-API"      — controls how the API bumps the serial on writes.
//   - "ALLOW-AXFR-FROM"   — list of IPs/CIDRs allowed to AXFR the zone.
//   - "TSIG-ALLOW-AXFR"   — list of TSIG key names allowed to AXFR.
//   - "PUBLISH-CDNSKEY"   — controls whether CDNSKEY records are published.
//   - "PUBLISH-CDS"       — controls whether CDS records are published.
//
// See https://doc.powerdns.com/authoritative/http-api/metadata.html for the
// full list.
package powerdns

import (
	"context"
)

// GetMetadata returns every metadata entry for the zone. The caller filters
// by kind; the driver returns the raw list.
func (p *Provider) GetMetadata(ctx context.Context, zoneID string) ([]Metadata, error) {
	ctx, span := startSpan(ctx, "metadata.list", zoneAttr(zoneID))
	defer span.End()

	var out []Metadata
	if err := p.do(ctx, "GET", metadataPath(zoneID), nil, &out); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return out, nil
}

// GetMetadataKind returns one metadata entry by kind. Returns ErrNotFound
// when the kind is not set on the zone.
func (p *Provider) GetMetadataKind(ctx context.Context, zoneID, kind string) (*Metadata, error) {
	ctx, span := startSpan(ctx, "metadata.get", zoneAttr(zoneID))
	defer span.End()

	var out Metadata
	if err := p.do(ctx, "GET", metadataKindPath(zoneID, kind), nil, &out); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return &out, nil
}

// PutMetadata sets one metadata entry. An empty Metadata slice deletes the
// entry (per PDNS' convention).
func (p *Provider) PutMetadata(ctx context.Context, zoneID string, meta Metadata) error {
	ctx, span := startSpan(ctx, "metadata.put", zoneAttr(zoneID))
	defer span.End()

	if err := p.do(ctx, "PUT", metadataKindPath(zoneID, meta.Kind), meta, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.metadata.updated",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
		Metadata:   asRawJSON(meta),
	})
	setStatus(span, nil)
	return nil
}

// DeleteMetadata removes one metadata entry from the zone.
func (p *Provider) DeleteMetadata(ctx context.Context, zoneID, kind string) error {
	ctx, span := startSpan(ctx, "metadata.delete", zoneAttr(zoneID))
	defer span.End()

	if err := p.do(ctx, "DELETE", metadataKindPath(zoneID, kind), nil, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.metadata.deleted",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
		Metadata:   asRawJSON(map[string]string{"kind": kind}),
	})
	setStatus(span, nil)
	return nil
}

// SetSOAEdit is the canonical helper for the SOA-EDIT policy. Empty value
// clears the metadata entry.
func (p *Provider) SetSOAEdit(ctx context.Context, zoneID, policy string) error {
	return p.PutMetadata(ctx, zoneID, Metadata{
		Kind:     "SOA-EDIT",
		Metadata: []string{policy},
	})
}

// metadataPath builds the metadata REST path for a zone.
func metadataPath(zoneID string) string {
	return zonePath(zoneID) + "/metadata"
}

// metadataKindPath builds the per-kind metadata REST path.
func metadataKindPath(zoneID, kind string) string {
	return metadataPath(zoneID) + "/" + kind
}
