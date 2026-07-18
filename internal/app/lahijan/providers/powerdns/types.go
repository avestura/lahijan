// Package powerdns: types.go holds the JSON request/response types for the
// PowerDNS REST API surface this driver needs. The shapes are intentionally
// narrow — Lahijan does not use every PDNS field. Types are documented
// against the PowerDNS HTTP API version they target (PDNS Authoritative
// 4.x / 5.x — we pin to a specific image tag in compose).
//
// Reference: https://doc.powerdns.com/authoritative/http-api/
package powerdns

import "encoding/json"

// serverPath is the REST path segment PDNS exposes for the "default" local
// server. PDNS' API URL shape is /api/v1/servers/<server-id>/zones; the
// daemon always answers as "localhost" when run as a single-node
// authoritative server.
const serverPath = "servers/localhost"

// Zone is a PDNS authoritative zone (a Lahijan DNS zone). Mirrors the JSON
// returned by GET /api/v1/servers/localhost/zones/<canonical>.
type Zone struct {
	// ID is the canonical zone identifier PDNS uses internally
	// (typically the canonical name with a trailing dot, lowercased).
	ID string `json:"id"`

	// Name is the canonical zone name (e.g. "example.com.").
	Name string `json:"name"`

	// Type is "Native", "Master", or "Slave" (PDNS' terms — Lahijan
	// always uses "Native" because we are the only NS by default).
	Type string `json:"type"`

	// URL is the relative REST URL PDNS exposes for the zone.
	URL string `json:"url,omitempty"`

	// Kind is an alias PDNS surfaces in some responses; mirrors Type.
	Kind string `json:"kind,omitempty"`

	// Serial is the current SOA serial.
	Serial int64 `json:"serial,omitempty"`

	// NotifiedSerial is the last serial pushed to secondaries.
	NotifiedSerial int64 `json:"notified_serial,omitempty"`

	// Masters is the list of upstream masters for slave zones. Lahijan
	// does not configure slave zones in MVP; left for WS-28.
	Masters []string `json:"masters,omitempty"`

	// RRsets is the list of record sets in the zone. PDNS only populates
	// this when the caller asks with rrsets=true; otherwise it is nil.
	RRsets []RRset `json:"rrsets,omitempty"`

	// SOAEditAPI is the per-zone SOA-EDIT-API policy PDNS uses when
	// updating the SOA serial on writes.
	SOAEditAPI string `json:"soa_edit_api,omitempty"`

	// SOAEdit is the per-zone SOA-EDIT policy PDNS uses when serving the
	// SOA record.
	SOAEdit string `json:"soa_edit,omitempty"`

	// Account is the free-form "account" tag PDNS supports; Lahijan uses
	// it to record the tenant id for out-of-band diagnostics. Set by the
	// driver at zone-create time, never surfaced to end users.
	Account string `json:"account,omitempty"`

	// DNSsec reports whether DNSSEC is enabled for the zone.
	DNSsec bool `json:"dnssec,omitempty"`

	// EditedSerial is the SOA serial after applying the SOA-EDIT policy.
	EditedSerial int64 `json:"edited_serial,omitempty"`

	// MasterTsec is the master TSIG key id; not used in MVP.
	MasterTsec string `json:"master_tsig_key_id,omitempty"`
}

// ZoneCreate is the body of POST /api/v1/servers/localhost/zones. Name is
// the canonical zone name (must end with a dot); Type is "Native" by default.
type ZoneCreate struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SOAEditAPI string `json:"soa_edit_api,omitempty"`
	SOAEdit    string `json:"soa_edit,omitempty"`
	Account    string `json:"account,omitempty"`
	// Nameservers is the list of NS target names written into the initial
	// SOA + NS RRsets. Each entry must be a canonical name with a trailing
	// dot. Required when the PDNS API is asked to bootstrap the zone.
	Nameservers []string `json:"nameservers,omitempty"`
	// RRsets lets the caller bootstrap RRsets at create time. Optional.
	RRsets []RRset `json:"rrsets,omitempty"`
}

// ZoneUpdate is the body of PATCH /api/v1/servers/localhost/zones/<id>. Only
// the fields the caller supplies are changed.
type ZoneUpdate struct {
	// SOAEditAPI overrides the SOA-EDIT-API policy ("DEFAULT",
	// "INCEPTION", "EPOCH", "INCEPTION-WEEK", etc.). Empty leaves the
	// value untouched.
	SOAEditAPI string `json:"soa_edit_api,omitempty"`
	// SOAEdit overrides the SOA-EDIT policy.
	SOAEdit string `json:"soa_edit,omitempty"`
	// Account overrides the account tag (tenant id).
	Account string `json:"account,omitempty"`
	// Kind overrides the zone kind ("Native", "Master", "Slave").
	Kind string `json:"kind,omitempty"`
	// Masters overrides the upstream masters list (slave zones only).
	Masters []string `json:"masters,omitempty"`
	// RRsets is the batched RRset change set PDNS applies atomically.
	// Each RRset carries a Changetype ("REPLACE" or "DELETE").
	RRsets []RRset `json:"rrsets,omitempty"`
}

// RRset is a PowerDNS record set: one (name, type) tuple carrying zero or
// more records plus an optional TTL.
type RRset struct {
	// Name is the canonical record name (e.g. "www.example.com.").
	Name string `json:"name,omitempty"`

	// Type is the DNS record type ("A", "AAAA", "CNAME", "MX", "TXT",
	// "NS", "SOA", "SRV", "CAA", "PTR", ...).
	Type string `json:"type,omitempty"`

	// TTL is the record's time-to-live in seconds.
	TTL int `json:"ttl,omitempty"`

	// Changetype is "REPLACE" (upsert) or "DELETE". Used in PATCH
	// payloads; ignored when the RRset is read from the daemon.
	Changetype string `json:"changetype,omitempty"`

	// Records is the list of records in the set. Empty + Changetype=
	// "DELETE" deletes the whole set.
	Records []Record `json:"records,omitempty"`

	// Comments is the list of PDNS per-record comments.
	Comments []Comment `json:"comments,omitempty"`
}

// Record is one RR within an RRset.
type Record struct {
	// Content is the zone-file-format value (e.g. "192.0.2.1" for an A
	// record, "10 mail.example.com." for an MX).
	Content string `json:"content"`

	// Disabled marks the record as temporarily inactive. PDNS still
	// serves the rest of the set.
	Disabled bool `json:"disabled,omitempty"`

	// SetPTR, when true, instructs PDNS to auto-create the matching PTR
	// record in the appropriate reverse zone. Lahijan uses this from the
	// reverse-zone helpers in records.go.
	SetPTR bool `json:"set-ptr,omitempty"`
}

// Comment is a PDNS per-record comment. Lahijan does not surface these to
// end users (DNS records are not annotatable in the MVP UI); the type exists
// so the GET response decodes cleanly.
type Comment struct {
	Content    string `json:"content"`
	Account    string `json:"account,omitempty"`
	ModifiedAt int64  `json:"modified_at,omitempty"`
}

// CryptoKey is a PDNS DNSSEC signing key for a zone. Returned by
// /api/v1/servers/localhost/zones/<id>/cryptokeys.
type CryptoKey struct {
	// ID is the numeric key id PDNS assigns.
	ID int64 `json:"id"`

	// KeyType is "ksk" (key-signing key) or "zsk" (zone-signing key).
	KeyType string `json:"keytype"`

	// Active reports whether PDNS is currently signing with this key.
	Active bool `json:"active"`

	// DNSsec reports whether DNSSEC is published in the zone's DS / DNSKEY.
	DNSsec bool `json:"dnssec,omitempty"`

	// Bits is the key length in bits (2048 or 4096 for ksk, 1024+ for zsk).
	Bits int `json:"bits,omitempty"`

	// Algorithm is the DNSSEC algorithm number (8 = RSASHA256, 13 = ECDSAP256SHA256, ...).
	Algorithm string `json:"algorithm,omitempty"`

	// Content is the private-key material PDNS surfaces when requested.
	// SENSITIVE — never log.
	Content string `json:"content,omitempty"`

	// Published reports whether PDNS publishes the DNSKEY in the zone.
	Published bool `json:"published,omitempty"`
}

// CryptoKeyCreate is the body of POST /api/v1/servers/localhost/zones/<id>/cryptokeys.
type CryptoKeyCreate struct {
	// KeyType is "ksk" or "zsk" (also "csk" for combined signing keys).
	KeyType string `json:"keytype"`

	// Bits is the key length in bits. Defaults to algorithm-appropriate.
	Bits int `json:"bits,omitempty"`

	// Algorithm is the DNSSEC algorithm (string, e.g. "ecdsaP256SHA256").
	Algorithm string `json:"algorithm,omitempty"`

	// Active flags whether the key is active immediately.
	Active bool `json:"active,omitempty"`

	// Published flags whether the DNSKEY is published immediately.
	Published bool `json:"published,omitempty"`
}

// Metadata is a PDNS zone-metadata entry. PDNS stores metadata as
// key → []string; the API exposes it as the kind + values pair.
type Metadata struct {
	// Kind is the metadata key ("SOA-EDIT", "ALLOW-AXFR-FROM",
	// "TSIG-ALLOW-AXFR", "PUBLISH-CDNSKEY", "PUBLISH-CDS", ...).
	Kind string `json:"kind"`

	// Metadata is the value list. PDNS uses a single-element list for
	// most kinds; multi-element for ACL lists.
	Metadata []string `json:"metadata"`
}

// server is the body of GET /api/v1/servers/localhost — used by Ping +
// Capabilities.
type server struct {
	// Type is always "Server" for PDNS Authoritative.
	Type string `json:"type"`

	// ID is "localhost".
	ID string `json:"id"`

	// URL is the relative REST URL.
	URL string `json:"url"`

	// DaemonType is "recursor" or "authoritative".
	DaemonType string `json:"daemon_type"`

	// Version is the daemon version string (e.g. "4.9.0").
	Version string `json:"version"`

	// ConfigURL is the URL of the config endpoint.
	ConfigURL string `json:"config_url"`

	// ZonesURL is the URL of the zones endpoint.
	ZonesURL string `json:"zones_url"`
}

// asRawJSON marshals v to a json.RawMessage; on marshal error it returns
// an empty object so the caller never has to handle a nil byte slice.
func asRawJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
