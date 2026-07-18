// Package dns: validators.go is the per-type record-content validator the
// service runs BEFORE the row is written. The validator's job is to catch
// malformed content here so PDNS never has to reject the request — a
// PDNS rejection surfaces as an opaque "Bad Request" envelope, while a
// validator rejection surfaces a per-type message that the UI can render
// ("A record content must be a valid IPv4 address").
//
// The validators are intentionally strict: they reject any content the
// PDNS daemon would also reject. They do NOT enforce RFC 1035 byte limits
// (label length, total name length) — the canonical-name helper covers
// those.
package dns

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Supported record types (mirrors providers/powerdns/records.go). Exported
// so handlers can render the allowlist in OpenAPI / docs.
const (
	TypeA     = "A"
	TypeAAAA  = "AAAA"
	TypeCAA   = "CAA"
	TypeCNAME = "CNAME"
	TypeDS    = "DS"
	TypeMX    = "MX"
	TypeNS    = "NS"
	TypePTR   = "PTR"
	TypeSOA   = "SOA"
	TypeSRV   = "SRV"
	TypeTLSA  = "TLSA"
	TypeTXT   = "TXT"
)

// SupportedRecordTypes is the allowlist the service accepts on writes.
// Hand to handlers for OpenAPI enumeration.
var SupportedRecordTypes = []string{
	TypeA, TypeAAAA, TypeCAA, TypeCNAME, TypeDS,
	TypeMX, TypeNS, TypePTR, TypeSOA, TypeSRV, TypeTLSA, TypeTXT,
}

// supportedRecordTypes is the set used internally for fast lookup.
var supportedRecordTypes = func() map[string]bool {
	m := make(map[string]bool, len(SupportedRecordTypes))
	for _, t := range SupportedRecordTypes {
		m[t] = true
	}
	return m
}()

// IsSupportedRecordType reports whether t is in the allowlist.
func IsSupportedRecordType(t string) bool { return supportedRecordTypes[t] }

// TTL bounds per WS-15 "Open questions" item 1: custom TTL allowed, within
// [300, 86400] seconds.
const (
	MinTTL = 300
	MaxTTL = 86400
)

// ValidateTTL returns ErrInvalidTTL when ttl is outside the allowed range.
// A zero value is treated as "use the default" (callers fill the default
// before calling; the validator is a pure check).
func ValidateTTL(ttl int) error {
	if ttl < MinTTL || ttl > MaxTTL {
		return fmt.Errorf("%w: got %d, want [%d, %d]", ErrInvalidTTL, ttl, MinTTL, MaxTTL)
	}
	return nil
}

// ValidateRecordContent checks that content matches the shape expected for
// recordType. Returns ErrInvalidRecordContent with a per-type message
// when the content is malformed. The zone argument is the canonical zone
// name (e.g. "example.com.") used to validate that CNAME / NS / MX / SRV
// targets that MUST be canonical names end with a dot.
//
// The validator does NOT enforce name/RFC 1035 length limits — those are
// covered by ValidateRecordName in zones.go.
func ValidateRecordContent(recordType, content, zone string) error {
	if content == "" {
		return fmt.Errorf("%w: content is empty", ErrInvalidRecordContent)
	}
	switch recordType {
	case TypeA:
		return validateA(content)
	case TypeAAAA:
		return validateAAAA(content)
	case TypeCAA:
		return validateCAA(content)
	case TypeCNAME:
		return validateCNAME(content, zone)
	case TypeDS:
		return validateDS(content)
	case TypeMX:
		return validateMX(content)
	case TypeNS:
		return validateNS(content)
	case TypePTR:
		return validatePTR(content)
	case TypeSOA:
		return validateSOA(content)
	case TypeSRV:
		return validateSRV(content)
	case TypeTLSA:
		return validateTLSA(content)
	case TypeTXT:
		return validateTXT(content)
	default:
		// Unsupported type — surface a clear error so the handler maps
		// to 400 bad_request, not 500.
		return fmt.Errorf("%w: unsupported record type %q", ErrInvalidRecordType, recordType)
	}
}

// validateA rejects anything that's not a valid IPv4 address.
func validateA(content string) error {
	ip := net.ParseIP(content)
	if ip == nil {
		return fmt.Errorf("%w: A record content must be a valid IPv4 address, got %q", ErrInvalidRecordContent, content)
	}
	if ip.To4() == nil {
		return fmt.Errorf("%w: A record content must be IPv4, got IPv6 %q", ErrInvalidRecordContent, content)
	}
	return nil
}

// validateAAAA rejects anything that's not a valid IPv6 address.
func validateAAAA(content string) error {
	ip := net.ParseIP(content)
	if ip == nil {
		return fmt.Errorf("%w: AAAA record content must be a valid IPv6 address, got %q", ErrInvalidRecordContent, content)
	}
	if ip.To4() != nil {
		return fmt.Errorf("%w: AAAA record content must be IPv6, got IPv4 %q", ErrInvalidRecordContent, content)
	}
	return nil
}

// validateCNAME enforces the target is a canonical DNS name (trailing dot,
// lowercase). The zone argument is unused at present but kept on the
// signature so a future iteration can forbid out-of-bailiwick targets if
// an operator chooses to.
func validateCNAME(content, _ string) error {
	if err := mustBeCanonicalName(content); err != nil {
		return fmt.Errorf("%w: CNAME target %s", ErrInvalidRecordContent, err)
	}
	return nil
}

// validateNS enforces the target is a canonical DNS name.
func validateNS(content string) error {
	if err := mustBeCanonicalName(content); err != nil {
		return fmt.Errorf("%w: NS target %s", ErrInvalidRecordContent, err)
	}
	return nil
}

// validatePTR enforces the target is a canonical DNS name.
func validatePTR(content string) error {
	if err := mustBeCanonicalName(content); err != nil {
		return fmt.Errorf("%w: PTR target %s", ErrInvalidRecordContent, err)
	}
	return nil
}

// validateMX enforces the wire form: "10 mail.example.com." — a numeric
// priority followed by a canonical name. PDNS expects the priority as a
// leading integer; we keep the same shape on the wire so the row mirrors
// what PDNS receives.
func validateMX(content string) error {
	prio, rest, found := splitLeadingInt(content)
	if !found {
		return fmt.Errorf("%w: MX content must be \"<priority> <canonical-name>\" (e.g. \"10 mail.example.com.\")", ErrInvalidRecordContent)
	}
	if prio < 0 || prio > 65535 {
		return fmt.Errorf("%w: MX priority must be in [0, 65535], got %d", ErrInvalidRecordContent, prio)
	}
	if err := mustBeCanonicalName(rest); err != nil {
		return fmt.Errorf("%w: MX target %s", ErrInvalidRecordContent, err)
	}
	return nil
}

// validateSRV enforces the wire form: "priority weight port target."
// (e.g. "10 60 5060 sipserver.example.com.").
func validateSRV(content string) error {
	parts := strings.Fields(content)
	if len(parts) != 4 {
		return fmt.Errorf("%w: SRV content must be \"<priority> <weight> <port> <target>\"", ErrInvalidRecordContent)
	}
	prio, err1 := strconv.Atoi(parts[0])
	weight, err2 := strconv.Atoi(parts[1])
	port, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("%w: SRV priority/weight/port must be integers", ErrInvalidRecordContent)
	}
	if prio < 0 || prio > 65535 {
		return fmt.Errorf("%w: SRV priority must be in [0, 65535]", ErrInvalidRecordContent)
	}
	if weight < 0 || weight > 65535 {
		return fmt.Errorf("%w: SRV weight must be in [0, 65535]", ErrInvalidRecordContent)
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("%w: SRV port must be in [0, 65535]", ErrInvalidRecordContent)
	}
	if err := mustBeCanonicalName(parts[3]); err != nil {
		return fmt.Errorf("%w: SRV target %s", ErrInvalidRecordContent, err)
	}
	return nil
}

// validateCAA enforces the wire form: "flags tag \"value\"" (e.g.
// "0 issue \"letsencrypt.org\"").
func validateCAA(content string) error {
	parts := splitCAA(content)
	if len(parts) < 3 {
		return fmt.Errorf("%w: CAA content must be \"<flags> <tag> \\\"<value>\\\"\"", ErrInvalidRecordContent)
	}
	flags, err := strconv.Atoi(parts[0])
	if err != nil || flags < 0 || flags > 255 {
		return fmt.Errorf("%w: CAA flags must be an integer in [0, 255]", ErrInvalidRecordContent)
	}
	tag := parts[1]
	if tag == "" {
		return fmt.Errorf("%w: CAA tag must be non-empty", ErrInvalidRecordContent)
	}
	// Re-join the remainder in case the value contained a space; CAA
	// values are quoted so we strip the leading + trailing quote.
	value := strings.Join(parts[2:], " ")
	value = strings.TrimPrefix(value, `"`)
	value = strings.TrimSuffix(value, `"`)
	if value == "" {
		return fmt.Errorf("%w: CAA value must be non-empty", ErrInvalidRecordContent)
	}
	return nil
}

// splitCAA splits a CAA content line into (flags, tag, "value..."). The
// value can contain spaces, so we use strings.Fields then re-join if
// there are more than three tokens. Quotes are preserved on the value.
func splitCAA(content string) []string {
	return strings.Fields(content)
}

// validateTXT enforces the content is a quoted string (or set of quoted
// strings). PDNS accepts either `"<value>"` or `<value>`; we mirror the
// RFC 1035 shape so the value round-trips through dig cleanly.
func validateTXT(content string) error {
	if content == "" {
		return fmt.Errorf("%w: TXT content must be non-empty", ErrInvalidRecordContent)
	}
	// PDNS accepts unquoted TXT values; we follow RFC 1035 and require
	// quoted strings. A future iteration can lift this if a deployer
	// asks.
	if !strings.HasPrefix(content, `"`) || !strings.HasSuffix(content, `"`) {
		return fmt.Errorf("%w: TXT content must be a quoted string (e.g. \"\\\"hello\\\"\")", ErrInvalidRecordContent)
	}
	// Reject unescaped quotes inside the value (would break PDNS parsing).
	inner := content[1 : len(content)-1]
	for i := 0; i < len(inner); i++ {
		if inner[i] == '"' && (i == 0 || inner[i-1] != '\\') {
			return fmt.Errorf("%w: TXT content has an unescaped quote", ErrInvalidRecordContent)
		}
	}
	return nil
}

// validateDS enforces the wire form: "keytag algorithm digesttype digest"
// (e.g. "12345 13 2 abc123...").
func validateDS(content string) error {
	parts := strings.Fields(content)
	if len(parts) != 4 {
		return fmt.Errorf("%w: DS content must be \"<keytag> <algorithm> <digesttype> <digest>\"", ErrInvalidRecordContent)
	}
	keyTag, err1 := strconv.Atoi(parts[0])
	algo, err2 := strconv.Atoi(parts[1])
	digestType, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("%w: DS keytag/algorithm/digesttype must be integers", ErrInvalidRecordContent)
	}
	if keyTag < 0 || keyTag > 65535 {
		return fmt.Errorf("%w: DS keytag must be in [0, 65535]", ErrInvalidRecordContent)
	}
	if algo < 0 || algo > 255 {
		return fmt.Errorf("%w: DS algorithm must be in [0, 255]", ErrInvalidRecordContent)
	}
	if digestType < 0 || digestType > 255 {
		return fmt.Errorf("%w: DS digesttype must be in [0, 255]", ErrInvalidRecordContent)
	}
	digest := parts[3]
	if len(digest) < 2 || len(digest)%2 != 0 {
		return fmt.Errorf("%w: DS digest must be an even-length hex string", ErrInvalidRecordContent)
	}
	for _, r := range digest {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return fmt.Errorf("%w: DS digest must be hex", ErrInvalidRecordContent)
		}
	}
	return nil
}

// validateTLSA enforces the wire form: "usage selector matchingtype certificate"
// (e.g. "3 1 1 abc123...").
func validateTLSA(content string) error {
	parts := strings.Fields(content)
	if len(parts) != 4 {
		return fmt.Errorf("%w: TLSA content must be \"<usage> <selector> <matchingtype> <certificate>\"", ErrInvalidRecordContent)
	}
	usage, err1 := strconv.Atoi(parts[0])
	sel, err2 := strconv.Atoi(parts[1])
	mt, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return fmt.Errorf("%w: TLSA usage/selector/matchingtype must be integers", ErrInvalidRecordContent)
	}
	if usage < 0 || usage > 255 {
		return fmt.Errorf("%w: TLSA usage must be in [0, 255]", ErrInvalidRecordContent)
	}
	if sel < 0 || sel > 255 {
		return fmt.Errorf("%w: TLSA selector must be in [0, 255]", ErrInvalidRecordContent)
	}
	if mt < 0 || mt > 255 {
		return fmt.Errorf("%w: TLSA matchingtype must be in [0, 255]", ErrInvalidRecordContent)
	}
	cert := parts[3]
	if len(cert) < 2 || len(cert)%2 != 0 {
		return fmt.Errorf("%w: TLSA certificate must be an even-length hex string", ErrInvalidRecordContent)
	}
	return nil
}

// validateSOA enforces the wire form: "primary-ns contact serial refresh
// retry expire minimum" (e.g. "ns1.example.com. hostmaster.example.com.
// 2024010101 10800 3600 604800 3600"). The serial is a 32-bit unsigned int.
func validateSOA(content string) error {
	parts := strings.Fields(content)
	if len(parts) != 7 {
		return fmt.Errorf("%w: SOA content must be \"<primary-ns> <contact> <serial> <refresh> <retry> <expire> <minimum>\"", ErrInvalidRecordContent)
	}
	if err := mustBeCanonicalName(parts[0]); err != nil {
		return fmt.Errorf("%w: SOA primary-ns %s", ErrInvalidRecordContent, err)
	}
	if err := mustBeCanonicalName(parts[1]); err != nil {
		return fmt.Errorf("%w: SOA contact %s", ErrInvalidRecordContent, err)
	}
	serial, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil {
		return fmt.Errorf("%w: SOA serial must be a 32-bit unsigned integer", ErrInvalidRecordContent)
	}
	_ = serial // validated; not used
	for i, label := range []string{"refresh", "retry", "expire", "minimum"} {
		_, part := parts[3+i], label
		_ = part
		v, perr := strconv.ParseUint(parts[3+i], 10, 32)
		if perr != nil {
			return fmt.Errorf("%w: SOA %s must be a 32-bit unsigned integer", ErrInvalidRecordContent, label)
		}
		_ = v
	}
	return nil
}

// mustBeCanonicalName enforces a canonical DNS name (lowercase, ends with
// a dot, no whitespace). Returns a descriptive error so the caller can
// wrap it with record-type context.
func mustBeCanonicalName(s string) error {
	if s == "" {
		return fmt.Errorf("must not be empty")
	}
	if !strings.HasSuffix(s, ".") {
		return fmt.Errorf("must end with a dot (canonical form): %q", s)
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return fmt.Errorf("must not contain whitespace: %q", s)
	}
	if strings.ToLower(s) != s {
		return fmt.Errorf("must be lowercase (canonical form): %q", s)
	}
	return nil
}

// splitLeadingInt splits "10 mail.example.com." into (10, "mail.example.com.",
// true). Returns (0, "", false) when the leading token is not an integer.
func splitLeadingInt(s string) (int, string, bool) {
	s = strings.TrimLeft(s, " \t")
	idx := strings.IndexAny(s, " \t")
	if idx <= 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(s[:idx])
	if err != nil {
		return 0, "", false
	}
	rest := strings.TrimLeft(s[idx+1:], " \t")
	return n, rest, true
}

// ParseMXPriority extracts the leading priority from an MX record content
// for storage in the prio column. Returns 0 when the content is malformed
// (the validator rejects it before this runs).
func ParseMXPriority(content string) int32 {
	p, _, ok := splitLeadingInt(content)
	if !ok {
		return 0
	}
	return int32(p)
}

// ParseSRVPriority extracts the leading priority from an SRV record content.
func ParseSRVPriority(content string) int32 {
	parts := strings.Fields(content)
	if len(parts) != 4 {
		return 0
	}
	p, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return int32(p)
}

// CanonicalizeIP normalises an IP address to its canonical form so two
// equivalent representations (e.g. "2001:db8::1" and "2001:0db8:0000::1")
// collapse to the same row.
func CanonicalizeIP(ipStr string) string {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return ipStr
	}
	return addr.String()
}
