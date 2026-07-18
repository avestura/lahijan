// Package dns: errors.go holds the sentinel errors the DNS service surfaces.
// Handlers translate them to HTTP envelopes via api/errors.go.
package dns

import "errors"

// ErrProviderDisabled is returned when the PowerDNS provider is not configured.
// The handler maps it to 501 not_implemented.
var ErrProviderDisabled = errors.New("dns: provider is not enabled on this server")

// ErrZoneNotFound is returned when the zone does not exist within the caller's
// tenant. The handler maps it to 404 not_found.
var ErrZoneNotFound = errors.New("dns: zone not found")

// ErrRecordNotFound is returned when the record does not exist within the
// caller's tenant. The handler maps it to 404 not_found.
var ErrRecordNotFound = errors.New("dns: record not found")

// ErrZoneAlreadyExists is returned when a create call uses a canonical name
// that's already owned by another tenant. The handler maps it to 409 conflict.
var ErrZoneAlreadyExists = errors.New("dns: zone already exists (claimed by another tenant)")

// ErrRecordAlreadyExists is returned when a create call would push the
// tenant over a duplicate (zone_id, name, type, content) identity.
var ErrRecordAlreadyExists = errors.New("dns: record already exists in this zone")

// ErrInvalidZoneName is returned when the zone name is not a canonical DNS
// name (lowercase, trailing dot, no whitespace).
var ErrInvalidZoneName = errors.New("dns: zone name must be a canonical lowercase DNS name ending with a dot")

// ErrInvalidRecordName is returned when the record name is not a canonical
// DNS name or does not belong to the zone.
var ErrInvalidRecordName = errors.New("dns: record name must be a canonical lowercase DNS name in the zone")

// ErrInvalidRecordType is returned when the record type is not in the
// supported allowlist (A, AAAA, CNAME, MX, TXT, NS, SOA, SRV, CAA, PTR).
var ErrInvalidRecordType = errors.New("dns: record type is not supported")

// ErrInvalidRecordContent is returned when the record content does not
// match the type-specific shape (e.g. an A record's content is not a
// valid IPv4). The wrapped message carries the per-type detail.
var ErrInvalidRecordContent = errors.New("dns: record content is invalid for this record type")

// ErrInvalidTTL is returned when the TTL is outside the allowed range
// [MinTTL, MaxTTL] (per WS-15 "Open questions" item 1).
var ErrInvalidTTL = errors.New("dns: ttl must be in [300, 86400]")

// ErrTemplateNotFound is returned when the caller references a template
// id that's not in the catalog. The handler maps it to 404 not_found.
var ErrTemplateNotFound = errors.New("dns: zone template not found")

// ErrCNAMEAtApex is returned when the caller attempts to create a CNAME
// record at the zone apex (per WS-15 "Open questions" item 2: strict RFC
// — no CNAME at apex).
var ErrCNAMEAtApex = errors.New("dns: CNAME at zone apex is not allowed (strict RFC)")
