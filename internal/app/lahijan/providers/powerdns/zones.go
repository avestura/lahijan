// Package powerdns: zones.go wraps the PDNS zones API. Every method takes an
// explicit canonical zone id (the caller — typically the DNS service in
// WS-15 — looks up the canonical id from the dns_zones table before calling).
// All methods open an OTel span; create/delete emit synthesized change events
// to the configured WASM bus.
package powerdns

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ZoneKind is the PDNS zone topology.
type ZoneKind string

const (
	// ZoneKindNative tells PDNS to store the zone and serve it without
	// any master/slave replication. The default for Lahijan (the only NS).
	ZoneKindNative ZoneKind = "Native"

	// ZoneKindMaster tells PDNS this zone is authoritative for a set of
	// slave secondaries. Used only in WS-28 (multi-secondary deploys).
	ZoneKindMaster ZoneKind = "Master"

	// ZoneKindSlave tells PDNS this zone is a slave of an upstream master.
	// Not used in MVP.
	ZoneKindSlave ZoneKind = "Slave"
)

// CreateZoneParams is the user-visible shape of a zone-create call.
type CreateZoneParams struct {
	// Name is the canonical zone name. Must end with a dot ("example.com.").
	Name string

	// Kind is "Native", "Master", or "Slave". Defaults to "Native".
	Kind ZoneKind

	// SOAEditAPI is the SOA-EDIT-API policy. Defaults to "DEFAULT".
	SOAEditAPI string

	// SOAEdit is the SOA-EDIT policy. Defaults to "" (disabled).
	SOAEdit string

	// Account is the PDNS "account" tag. Lahijan uses it to record the
	// tenant id for out-of-band diagnostics. NEVER surfaced to end users.
	Account string

	// Nameservers is the list of NS target names written into the
	// initial SOA + NS RRsets. Each entry must be a canonical name with
	// a trailing dot. Required when the PDNS API is asked to bootstrap
	// the zone (Lahijan always asks).
	Nameservers []string

	// RRsets is an optional initial RRset change set applied at create
	// time. Useful when bootstrapping a zone with its MX/TXT records.
	RRsets []RRset
}

// CreateZone creates a zone and returns the freshly-created Zone record.
// Returns ErrAlreadyExists if the zone already exists; ErrBadRequest if the
// name is malformed or missing the trailing dot.
func (p *Provider) CreateZone(ctx context.Context, params CreateZoneParams) (*Zone, error) {
	ctx, span := startSpan(ctx, "zone.create", zoneAttr(params.Name))
	defer span.End()

	if err := validateCanonicalName(params.Name); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("powerdns: zone.create: %w", err)
	}
	if len(params.Nameservers) == 0 {
		// PDNS requires at least one NS target when bootstrapping a zone.
		err := errors.New("powerdns: zone.create: at least one nameserver is required")
		setStatus(span, err)
		return nil, err
	}

	kind := params.Kind
	if kind == "" {
		kind = ZoneKindNative
	}
	soaEditAPI := params.SOAEditAPI
	if soaEditAPI == "" {
		soaEditAPI = "DEFAULT"
	}
	body := ZoneCreate{
		Name:        params.Name,
		Kind:        string(kind),
		SOAEditAPI:  soaEditAPI,
		SOAEdit:     params.SOAEdit,
		Account:     params.Account,
		Nameservers: params.Nameservers,
		RRsets:      params.RRsets,
	}
	var created Zone
	if err := p.do(ctx, "POST", "servers/localhost/zones", body, &created); err != nil {
		setStatus(span, err)
		return nil, err
	}
	// Synthesize a change event for the WASM bus.
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.created",
		ActorType:  "system",
		ResourceID: strPtr(created.ID),
		Metadata:   asRawJSON(created),
	})
	setStatus(span, nil)
	return &created, nil
}

// GetZone fetches the current state of a zone by its canonical id. The
// rrsets flag controls whether PDNS includes the RRsets array in the response.
func (p *Provider) GetZone(ctx context.Context, zoneID string, rrsets bool) (*Zone, error) {
	ctx, span := startSpan(ctx, "zone.get", zoneAttr(zoneID))
	defer span.End()

	path := zonePath(zoneID)
	if rrsets {
		path += "?rrsets=true"
	}
	var z Zone
	if err := p.do(ctx, "GET", path, nil, &z); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return &z, nil
}

// ListZones lists every zone the daemon serves. Lahijan scopes this list at
// the service layer via the dns_zones table; the driver returns the raw
// PDNS view.
func (p *Provider) ListZones(ctx context.Context) ([]Zone, error) {
	ctx, span := startSpan(ctx, "zone.list")
	defer span.End()

	var zones []Zone
	if err := p.do(ctx, "GET", "servers/localhost/zones", nil, &zones); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return zones, nil
}

// UpdateZone applies a partial update (SOA-EDIT-API, account, kind, masters,
// or a batched RRset change set). The PATCH is atomic on the PDNS side.
func (p *Provider) UpdateZone(ctx context.Context, zoneID string, update ZoneUpdate) error {
	ctx, span := startSpan(ctx, "zone.update", zoneAttr(zoneID))
	defer span.End()

	if err := p.do(ctx, "PATCH", zonePath(zoneID), update, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.updated",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
		Metadata:   asRawJSON(update),
	})
	setStatus(span, nil)
	return nil
}

// DeleteZone removes a zone and all of its RRsets from the daemon. Returns
// ErrNotFound if the zone does not exist.
func (p *Provider) DeleteZone(ctx context.Context, zoneID string) error {
	ctx, span := startSpan(ctx, "zone.delete", zoneAttr(zoneID))
	defer span.End()

	if err := p.do(ctx, "DELETE", zonePath(zoneID), nil, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.zone.deleted",
		ActorType:  "system",
		ResourceID: strPtr(zoneID),
	})
	setStatus(span, nil)
	return nil
}

// SetAXFR configures whether the zone can be AXFR-transferred by secondaries.
// When allow is false (the WS-12 default), ALLOW-AXFR-FROM is set to the
// empty list; when true, the caller passes the list of allowed CIDRs / IPs.
//
// Per WS-12 "Open questions" the default is no AXFR (Lahijan is the only NS
// by default). This method is the canonical entry point for the future
// "add a secondary" workflow.
func (p *Provider) SetAXFR(ctx context.Context, zoneID string, allow bool, from []string) error {
	ctx, span := startSpan(ctx, "zone.axfr.set", zoneAttr(zoneID))
	defer span.End()

	var values []string
	if allow {
		values = append(values, from...)
	}
	// PDNS exposes ALLOW-AXFR-FROM via the metadata endpoint; setting it
	// to an empty list removes the metadata entry entirely.
	if err := p.PutMetadata(ctx, zoneID, Metadata{
		Kind:     "ALLOW-AXFR-FROM",
		Metadata: values,
	}); err != nil {
		setStatus(span, err)
		return err
	}
	setStatus(span, nil)
	return nil
}

// zonePath builds the zone REST path.
func zonePath(zoneID string) string {
	return "servers/localhost/zones/" + url.PathEscape(zoneID)
}

// validateCanonicalName returns an error if the name is not a canonical DNS
// name (lowercase, ends with a dot, no embedded whitespace).
func validateCanonicalName(name string) error {
	if name == "" {
		return errors.New("name is empty")
	}
	if !strings.HasSuffix(name, ".") {
		return fmt.Errorf("name %q must end with a dot (canonical form)", name)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("name %q must not contain whitespace", name)
	}
	if strings.ToLower(name) != name {
		return fmt.Errorf("name %q must be lowercase (canonical form)", name)
	}
	return nil
}

// strPtr returns a pointer to a freshly-allocated copy of s. Used for the
// optional ResourceID field on synthesized events.
func strPtr(s string) *string { return &s }
