// Package powerdns: records.go wraps the PDNS RRset API. Every method takes
// the canonical zone id (the caller — typically the DNS service in WS-15 —
// looks up the canonical id from the dns_zones table before calling). All
// methods open an OTel span; REPLACE/DELETE emit synthesized change events
// to the configured WASM bus.
//
// PDNS exposes RRset writes via PATCH /servers/localhost/zones/<id> with a
// body of {"rrsets": [...]}. Each RRset carries a Changetype of "REPLACE"
// (upsert) or "DELETE". This driver surfaces that as a small set of typed
// helpers so callers do not have to remember the wire shape.
package powerdns

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RecordType is the DNS record type. We allowlist the types Lahijan
// supports; everything else is rejected with ErrBadRequest.
type RecordType string

const (
	TypeA     RecordType = "A"
	TypeAAAA  RecordType = "AAAA"
	TypeCAA   RecordType = "CAA"
	TypeCNAME RecordType = "CNAME"
	TypeDS    RecordType = "DS"
	TypeMX    RecordType = "MX"
	TypeNS    RecordType = "NS"
	TypePTR   RecordType = "PTR"
	TypeSOA   RecordType = "SOA"
	TypeSRV   RecordType = "SRV"
	TypeTLSA  RecordType = "TLSA"
	TypeTXT   RecordType = "TXT"
)

// supportedRecordTypes is the allowlist the driver accepts on writes. The
// daemon's own validation runs after this; the driver-side check produces a
// cleaner error and a stable error code for the test suite.
var supportedRecordTypes = map[RecordType]bool{
	TypeA: true, TypeAAAA: true, TypeCAA: true, TypeCNAME: true,
	TypeDS: true, TypeMX: true, TypeNS: true, TypePTR: true,
	TypeSOA: true, TypeSRV: true, TypeTLSA: true, TypeTXT: true,
}

// RRsetUpsertParams is the user-visible shape of an RRset REPLACE call.
type RRsetUpsertParams struct {
	// ZoneID is the canonical zone id (e.g. "example.com.").
	ZoneID string

	// Name is the canonical record name (e.g. "www.example.com.").
	Name string

	// Type is the DNS record type.
	Type RecordType

	// TTL is the record's time-to-live in seconds.
	TTL int

	// Records is the list of records in the set. Empty + Changetype=
	// REPLACE effectively clears the set; use DeleteRRset for an explicit
	// delete.
	Records []Record
}

// ReplaceRRset upserts an RRset into the zone. If the set already exists it
// is replaced atomically; if it does not exist it is created.
//
// Validation: the record type must be in the supported allowlist; the record
// name must be a canonical DNS name (lowercase, trailing dot). The daemon
// performs additional content-shape validation (e.g. an A record's content
// must be a valid IPv4).
func (p *Provider) ReplaceRRset(ctx context.Context, params RRsetUpsertParams) error {
	ctx, span := startSpan(ctx, "rrset.replace",
		zoneAttr(params.ZoneID), recordAttr(params.Name))
	defer span.End()

	if err := validateRRsetName(params.Name, params.ZoneID); err != nil {
		setStatus(span, err)
		return fmt.Errorf("powerdns: rrset.replace: %w", err)
	}
	if !supportedRecordTypes[params.Type] {
		err := fmt.Errorf("powerdns: rrset.replace: unsupported record type %q", params.Type)
		setStatus(span, err)
		return err
	}

	update := ZoneUpdate{
		RRsets: []RRset{{
			Name:       params.Name,
			Type:       string(params.Type),
			TTL:        params.TTL,
			Changetype: "REPLACE",
			Records:    params.Records,
		}},
	}
	if err := p.do(ctx, "PATCH", zonePath(params.ZoneID), update, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.record.updated",
		ActorType:  "system",
		ResourceID: strPtr(params.ZoneID),
		Metadata: asRawJSON(map[string]any{
			"name":    params.Name,
			"type":    string(params.Type),
			"ttl":     params.TTL,
			"records": params.Records,
		}),
	})
	setStatus(span, nil)
	return nil
}

// DeleteRRsetParams is the user-visible shape of an RRset DELETE call.
type DeleteRRsetParams struct {
	ZoneID string
	Name   string
	Type   RecordType
}

// DeleteRRset removes an RRset from the zone. Returns ErrNotFound (via the
// daemon) if the set does not exist.
func (p *Provider) DeleteRRset(ctx context.Context, params DeleteRRsetParams) error {
	ctx, span := startSpan(ctx, "rrset.delete",
		zoneAttr(params.ZoneID), recordAttr(params.Name))
	defer span.End()

	if err := validateRRsetName(params.Name, params.ZoneID); err != nil {
		setStatus(span, err)
		return fmt.Errorf("powerdns: rrset.delete: %w", err)
	}
	if !supportedRecordTypes[params.Type] {
		err := fmt.Errorf("powerdns: rrset.delete: unsupported record type %q", params.Type)
		setStatus(span, err)
		return err
	}

	update := ZoneUpdate{
		RRsets: []RRset{{
			Name:       params.Name,
			Type:       string(params.Type),
			Changetype: "DELETE",
		}},
	}
	if err := p.do(ctx, "PATCH", zonePath(params.ZoneID), update, nil); err != nil {
		setStatus(span, err)
		return err
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "dns.record.deleted",
		ActorType:  "system",
		ResourceID: strPtr(params.ZoneID),
		Metadata: asRawJSON(map[string]any{
			"name": params.Name,
			"type": string(params.Type),
		}),
	})
	setStatus(span, nil)
	return nil
}

// SearchRRsets fetches every RRset in the zone that matches the given
// (optional) name and/or type filters. Empty name = all names; empty type =
// all types. Returns the raw PDNS RRsets for the caller to shape.
func (p *Provider) SearchRRsets(
	ctx context.Context,
	zoneID string,
	nameFilter string,
	typeFilter RecordType,
) ([]RRset, error) {
	ctx, span := startSpan(ctx, "rrset.search", zoneAttr(zoneID))
	defer span.End()

	z, err := p.GetZone(ctx, zoneID, true)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	out := make([]RRset, 0, len(z.RRsets))
	for _, rr := range z.RRsets {
		if nameFilter != "" && rr.Name != nameFilter {
			continue
		}
		if typeFilter != "" && rr.Type != string(typeFilter) {
			continue
		}
		out = append(out, rr)
	}
	setStatus(span, nil)
	return out, nil
}

// validateRRsetName returns an error if name is not a canonical DNS name or
// does not belong to the given zone. The zone id is typically the canonical
// zone name; for a record "www.example.com." in zone "example.com." this
// check passes; the same record in zone "example.org." fails.
func validateRRsetName(name, zoneID string) error {
	if err := validateCanonicalName(name); err != nil {
		return err
	}
	// Allow exact zone-apex match (e.g. SOA at "example.com.") and any
	// sub-domain ("www.example.com." in "example.com.").
	if name == zoneID {
		return nil
	}
	if !strings.HasSuffix(name, "."+zoneID) {
		return fmt.Errorf("name %q does not belong to zone %q", name, zoneID)
	}
	return nil
}

// ReverseZoneName returns the canonical zone name for the reverse (in-addr.arpa
// or ip6.arpa) zone covering the given network. Used by the DNS service to
// look up the right reverse zone when a tenant creates a PTR via SetPTR on
// an A/AAAA record.
//
// Examples:
//
//	ReverseZoneName("192.0.2.0/24")   → "2.0.192.in-addr.arpa."
//	ReverseZoneName("2001:db8::/32")  → "8.b.d.0.1.0.0.2.ip6.arpa."
func ReverseZoneName(cidr string) (string, error) {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("powerdns: reverse zone: parse cidr: %w", err)
	}
	ones, bits := n.Mask.Size()
	if bits == 32 {
		return ipv4ReverseZone(n.IP, ones), nil
	}
	return ipv6ReverseZone(n.IP, ones), nil
}

// ipv4ReverseZone builds the in-addr.arpa canonical name for an IPv4 network.
// PDNS expects the zone to cover an octet boundary; we truncate to the
// nearest /8, /16, or /24 below the prefix length.
func ipv4ReverseZone(ip net.IP, ones int) string {
	ip = ip.To4()
	// Round down to the nearest octet boundary that is fully covered.
	octets := ones / 8
	if octets < 1 {
		octets = 1
	}
	parts := make([]string, octets)
	for i := 0; i < octets; i++ {
		// Reverse byte order: the first octet in the label is the
		// highest-order octet of the address that the zone covers.
		parts[i] = strconv.FormatUint(uint64(ip[octets-1-i]), 10)
	}
	return strings.Join(parts, ".") + ".in-addr.arpa."
}

// ipv6ReverseZone builds the ip6.arpa canonical name for an IPv6 network.
// PDNS expects the zone to cover a nibble boundary; we truncate to the
// nearest 4-bit boundary at or below the prefix length.
//
// For "2001:db8::/32" the prefix covers nibbles 0..7 of the address
// (high-order first: 2, 0, 0, 1, 0, d, b, 8). The reverse zone name lists
// them in REVERSE order, dot-separated, with the .ip6.arpa. suffix:
// "8.b.d.0.1.0.0.2.ip6.arpa.".
func ipv6ReverseZone(ip net.IP, ones int) string {
	// Round down to the nearest nibble boundary.
	nibbles := ones / 4
	if nibbles < 1 {
		nibbles = 1
	}
	out := make([]string, 0, nibbles)
	// Label i is nibble (nibbles-1-i) of the prefix, where nibble 0 is the
	// highest-order nibble of the address (high half of byte 0).
	for i := 0; i < nibbles; i++ {
		j := nibbles - 1 - i
		byteIdx := j / 2
		var nibble byte
		if j%2 == 0 {
			nibble = ip[byteIdx] >> 4
		} else {
			nibble = ip[byteIdx] & 0x0f
		}
		out = append(out, string(hexDigits[nibble]))
	}
	return strings.Join(out, ".") + ".ip6.arpa."
}

// hexDigits is the lowercase hex digit table used by the reverse-zone
// helpers. Kept package-level (rather than inline `const`) so both
// ipv6ReverseZone and PTRName share it.
const hexDigits = "0123456789abcdef"

// PTRName returns the canonical PTR record name for the given IP. Used by
// the DNS service when an instance gets a public IP and the operator has a
// reverse zone for it.
//
// Examples:
//
//	PTRName("192.0.2.5")     → "5.2.0.192.in-addr.arpa."
//	PTRName("2001:db8::1")   → "1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa."
func PTRName(ipStr string) (string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", fmt.Errorf("powerdns: ptr name: invalid ip %q", ipStr)
	}
	if v4 := ip.To4(); v4 != nil {
		return strconv.FormatUint(uint64(v4[3]), 10) + "." +
			strconv.FormatUint(uint64(v4[2]), 10) + "." +
			strconv.FormatUint(uint64(v4[1]), 10) + "." +
			strconv.FormatUint(uint64(v4[0]), 10) + ".in-addr.arpa.", nil
	}
	// IPv6: reverse nibble order.
	parts := make([]string, 0, 32)
	// Walk nibbles in reverse (lowest-order first).
	for i := len(ip) - 1; i >= 0; i-- {
		parts = append(parts, string(hexDigits[ip[i]&0x0f]))
		parts = append(parts, string(hexDigits[ip[i]>>4]))
	}
	return strings.Join(parts, ".") + ".ip6.arpa.", nil
}

// EnsureZoneApexSOA returns the SOA RRset PDNS creates at zone-create time.
// Used by tests + the DNS service's zone-bootstrap helper to assert the SOA
// is correct without a separate GET round-trip.
//
// This is a pure helper; it does not call the daemon.
func EnsureZoneApexSOA(zoneID, primaryNS, contact string) RRset {
	if contact == "" {
		contact = "hostmaster." + zoneID
	}
	if !strings.HasSuffix(contact, ".") {
		contact += "."
	}
	return RRset{
		Name: zoneID,
		Type: string(TypeSOA),
		TTL:  3600,
		Records: []Record{{
			Content: fmt.Sprintf("%s %s 1 10800 3600 604800 3600",
				primaryNS, contact),
		}},
	}
}

// (No additional sentinels: validateCanonicalName covers empty-name rejection
// and surfaces a stable ErrBadRequest-shaped wrap from the daemon.)
