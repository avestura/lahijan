// Package program: compute_ip_ptrs.go wires the WS-30 reverse-DNS
// auto-publish seam (ADR-0037 sub-decision A). The compute service
// defines a `ptrPublisher` interface; this file builds the only
// production implementation, which delegates to dns.Service.
//
// The adapter is tenant-aware in a specific way: the operator-owned
// reverse zone lives in some tenant (the "platform tenant"); the
// floating IP itself belongs to a user tenant. The adapter sets up a
// context that scopes the dns.Service call to the tenant that owns
// the reverse zone (looked up via the dns_zones cross-tenant
// canonical-id helper) so the dns_records row lands in the right
// tenant + PowerDNS publishes into the right zone.
//
// Per pillar 1 the user-facing copy never mentions PowerDNS; this
// adapter is the only place the two modules meet.
package program

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
)

// dnsPTRPublisher adapts dns.Service to compute.ptrPublisher. It
// resolves the operator-owned reverse zone, sets up the zone's owning
// tenant in the request context, then delegates to dns.Service.
//
// The userID is set to uuid.Nil because PTR auto-publish is a system
// action; the dns service's audit row carries actor_type=system when
// the user_id is nil (the dns service's audit emit maps this to the
// "auth.user.id" column being NULL).
type dnsPTRPublisher struct {
	dns   *dns.Service
	repos *database.Repos
}

// newDNSPTRPublisher builds the adapter. Returns nil when either
// dependency is nil so the caller can short-circuit cleanly.
func newDNSPTRPublisher(dnsSvc *dns.Service, repos *database.Repos) *dnsPTRPublisher {
	if dnsSvc == nil || repos == nil {
		return nil
	}
	return &dnsPTRPublisher{dns: dnsSvc, repos: repos}
}

// PublishPTR writes a PTR record for ip into the named zone.
//
// Implementation notes:
//   - The record name is the canonical reverse-DNS form
//     (e.g. "5.2.0.192.in-addr.arpa." for IPv4,
//     "5.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa."
//     for IPv6).
//   - The zone's owning tenant is looked up via the dns_zones row's
//     tenant_id (the zone is global-by-canonical-id but the row is
//     tenant-scoped); the dns.Service call is scoped to that tenant.
//   - The audit row carries actor_type=system (userID is uuid.Nil).
func (p *dnsPTRPublisher) PublishPTR(
	ctx context.Context,
	zoneID uuid.UUID,
	ip netip.Addr,
	target string,
) error {
	zone, err := p.repos.DNSZones.GetByIDGlobal(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("compute.ptr_publisher: lookup reverse zone: %w", err)
	}
	zoneCtx := database.WithTenant(ctx, zone.TenantID)
	_, err = p.dns.CreateRecord(zoneCtx, zone.TenantID, uuid.Nil, zone.ID, dns.RecordCreateParams{
		Name:    reverseDNSName(ip, zone.CanonicalID),
		Type:    dns.TypePTR,
		Content: target,
	})
	if err != nil {
		return fmt.Errorf("compute.ptr_publisher: create ptr: %w", err)
	}
	return nil
}

// UnpublishPTR removes the PTR record for ip from the named zone.
//
// Looks up the record by (zone, name, type) via the dns_records
// repository and delegates to dns.Service.DeleteRecord. Tolerates a
// missing record (idempotent — the row may have been removed by a
// concurrent path).
func (p *dnsPTRPublisher) UnpublishPTR(
	ctx context.Context,
	zoneID uuid.UUID,
	ip netip.Addr,
) error {
	zone, err := p.repos.DNSZones.GetByIDGlobal(ctx, zoneID)
	if err != nil {
		return fmt.Errorf("compute.ptr_publisher: lookup reverse zone: %w", err)
	}
	zoneCtx := database.WithTenant(ctx, zone.TenantID)
	name := reverseDNSName(ip, zone.CanonicalID)
	row, err := p.repos.DNSRecords.GetByNameGlobal(ctx, zone.ID, name, dns.TypePTR)
	if err != nil {
		if database.IsNoRows(err) {
			return nil // idempotent
		}
		return fmt.Errorf("compute.ptr_publisher: lookup ptr record: %w", err)
	}
	if err := p.dns.DeleteRecord(zoneCtx, zone.TenantID, uuid.Nil, zone.ID, row.ID); err != nil {
		return fmt.Errorf("compute.ptr_publisher: delete ptr: %w", err)
	}
	return nil
}

// reverseDNSName returns the canonical reverse-DNS name for ip
// truncated to the reverse zone's canonical id. The zone's canonical
// id is something like "2.0.192.in-addr.arpa." (a /24 reverse zone)
// or "0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa." (a /32 IPv6 reverse zone).
//
// For an IPv4 address 192.0.2.5 in zone "2.0.192.in-addr.arpa.": the
// record name is "5.2.0.192.in-addr.arpa.".
//
// For an IPv6 address 2001:db8::5 in zone "...ip6.arpa.": the record
// name is the full nibble-reversed form appended to the zone.
//
// When the IP is not inside the zone (e.g. a v4 address and a v6
// zone), the function still returns a syntactically-valid name so the
// dns service's validator rejects it cleanly. The compute service
// should not call this with a mismatched IP/zone pair.
func reverseDNSName(ip netip.Addr, zoneCanonical string) string {
	// We build the reverse-DNS name by reversing the bytes (v4) or
	// nibbles (v6) and joining with dots, then appending the zone's
	// canonical id. The zone canonical id already ends with a dot so
	// the result is a fully-qualified name.
	if zoneCanonical == "" {
		return ""
	}
	// Strip the trailing dot from the zone canonical for the prefix
	// computation; the result will be "<reversed>.<zone-without-dot>.".
	zoneNoDot := zoneCanonical
	if zoneNoDot[len(zoneNoDot)-1] == '.' {
		zoneNoDot = zoneNoDot[:len(zoneNoDot)-1]
	}

	if ip.Is4() {
		b := ip.As4()
		// 192.0.2.5 -> "5.2.0.192" prefix.
		prefix := fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0])
		// Truncate the prefix so the result belongs to the zone. The
		// zone's "2.0.192.in-addr.arpa" form implies the leading
		// "5." is the host part. We use the zone's leading octet
		// count to decide how many host octets to keep.
		hostLabels := hostLabelsForV4Zone(zoneNoDot)
		prefixLabels := splitDot(prefix)
		if hostLabels > 0 && hostLabels < len(prefixLabels) {
			prefixLabels = prefixLabels[:hostLabels]
		}
		full := joinDot(prefixLabels) + "." + zoneNoDot + "."
		return full
	}
	// IPv6: full nibble reversal.
	b := ip.As16()
	// Build nibbles in reverse: low nibble of byte 15 first.
	nibbles := make([]string, 0, 32)
	for i := len(b) - 1; i >= 0; i-- {
		nibbles = append(nibbles, strconv.FormatUint(uint64(b[i]&0x0f), 16))
		nibbles = append(nibbles, strconv.FormatUint(uint64(b[i]>>4), 16))
	}
	full := joinDot(nibbles) + "." + zoneNoDot + "."
	return full
}

// hostLabelsForV4Zone returns the number of host-octets the zone
// expects in the record name. For "2.0.192.in-addr.arpa" (a /24) the
// answer is 1; for "0.192.in-addr.arpa" (a /16) the answer is 2.
// Returns 1 for an unrecognised shape so the caller still produces a
// valid (if perhaps mismatched) name.
func hostLabelsForV4Zone(zoneNoDot string) int {
	// Strip the trailing ".in-addr.arpa" suffix and count the dots in
	// what remains.
	const suffix = ".in-addr.arpa"
	if len(zoneNoDot) < len(suffix) {
		return 1
	}
	core := zoneNoDot[:len(zoneNoDot)-len(suffix)]
	if core == "" {
		return 1
	}
	dots := 0
	for _, r := range core {
		if r == '.' {
			dots++
		}
	}
	// /24 -> 0 dots in core -> 1 host label.
	// /16 -> 1 dot in core -> 2 host labels.
	return dots + 1
}

func splitDot(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func joinDot(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "."
		}
		out += p
	}
	return out
}
