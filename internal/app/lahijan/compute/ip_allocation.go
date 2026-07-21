// Package compute: ip_allocation.go implements the next-free-IP
// allocation logic the floating-IP allocate path uses (WS-30, ADR-0037).
//
// The algorithm is intentionally simple + deterministic:
//
//  1. Read every range in the pool (already ordered by the repo's
//     ListAllForPool helper — family ASC, cidr ASC).
//  2. Read every already-allocated address in the pool (the global
//     ListAllAddressesInPool helper; admin-only at the HTTP boundary).
//  3. For each range, walk host addresses from network+1 to broadcast-1
//     (skipping IPv4 network/broadcast automatically; IPv6 has no
//     broadcast, so we only skip the network address). Skip addresses
//     in the range's excluded list (gateway, reserved, ...) and
//     addresses already allocated.
//  4. Return the first free address. If no range has a free address,
//     return ErrIPPoolExhausted.
//
// The walk is O(range_size) per allocation which is fine for v4 and
// small v6 prefixes. The operator is expected to register v6 ranges
// with a tight prefix (/118 = 1024 hosts, /120 = 256 hosts) so the walk
// stays cheap. A future WS can replace this with a SQL-level
// generate_series + LEFT JOIN if performance becomes a concern.
package compute

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// pickNextFreeAddress returns the lowest free host address across every
// range in the pool, or ErrIPPoolExhausted if no range has a free
// address. The caller is expected to hold the per-tenant advisory lock
// (compute/advisory_lock.go) so two concurrent allocations from the
// same pool do not race; the unique index on floating_ips.address is
// the last-line defence.
func pickNextFreeAddress(
	ctx context.Context,
	repos *database.Repos,
	poolID uuid.UUID,
) (netip.Addr, int32, error) {
	ranges, err := repos.IPPoolRanges.ListAllForPool(ctx, poolID)
	if err != nil {
		return netip.Addr{}, 0, fmt.Errorf("compute: list pool ranges: %w", err)
	}
	if len(ranges) == 0 {
		return netip.Addr{}, 0, ErrIPPoolExhausted
	}

	allocated, err := repos.FloatingIPs.ListAllAddressesInPool(ctx, poolID)
	if err != nil {
		return netip.Addr{}, 0, fmt.Errorf("compute: list allocated addresses: %w", err)
	}
	allocatedSet := make(map[string]struct{}, len(allocated))
	for _, a := range allocated {
		// Normalise to canonical netip.Addr.String() form. The DB
		// stores inet values with the prefix (e.g. "198.51.100.1/32")
		// because PostgreSQL's inet type carries the netmask; the
		// picker walks host addresses (no prefix) so the comparison
		// must strip the prefix before keying the set.
		if addr, pErr := netip.ParseAddr(strings.TrimSuffix(a, "/32")); pErr == nil {
			allocatedSet[addr.String()] = struct{}{}
			continue
		}
		// IPv6 hosts carry "/128"; strip that too.
		if addr, pErr := netip.ParseAddr(strings.TrimSuffix(a, "/128")); pErr == nil {
			allocatedSet[addr.String()] = struct{}{}
		}
	}

	for _, r := range ranges {
		prefix, errParse := netip.ParsePrefix(r.Cidr)
		if errParse != nil {
			// Skip malformed ranges; the add-range path validates so
			// this should never happen, but we do not want one bad
			// row to wedge the whole pool.
			continue
		}
		excluded, _ := database.ParseExcludedAddresses(r.ExcludedAddresses)
		excludedSet := make(map[string]struct{}, len(excluded))
		for _, e := range excluded {
			excludedSet[e] = struct{}{}
		}

		if addr, ok := firstFreeInPrefix(prefix, excludedSet, allocatedSet); ok {
			return addr, familyOf(prefix), nil
		}
	}
	return netip.Addr{}, 0, ErrIPPoolExhausted
}

// firstFreeInPrefix walks host addresses in the prefix and returns the
// first one not in excluded or allocated. Returns ok=false if every
// host address is taken.
//
// For IPv4 the walk skips the network address (.0 in /24) and the
// broadcast address (.255 in /24) automatically. For IPv6 only the
// network address is skipped (IPv6 has no broadcast).
//
// For prefixes smaller than /31 (IPv4) or /127 (IPv6) the walk skips
// network + broadcast. For /31 + /127 (point-to-point links, RFC 3021)
// both addresses are usable; the walk does not skip them.
func firstFreeInPrefix(
	prefix netip.Prefix,
	excluded map[string]struct{},
	allocated map[string]struct{},
) (netip.Addr, bool) {
	net := prefix.Masked().Addr()
	bits := prefix.Bits()
	isV4 := net.Is4()
	hostBits := 128 - bits
	if isV4 {
		hostBits = 32 - bits
	}
	// /32 + /128: only one address (the network). For these prefixes
	// the network IS the host; skip the auto-skip-network rule.
	if hostBits == 0 {
		if !isExcludedOrAllocated(net, excluded, allocated) {
			return net, true
		}
		return netip.Addr{}, false
	}
	// For v4 hostBits==1 (i.e. /31, RFC 3021 point-to-point) both
	// addresses are usable. For v6 /127 same. The "skip network +
	// broadcast" rule only kicks in when hostBits >= 2.
	skipNetwork := hostBits >= 2
	skipBroadcast := isV4 && hostBits >= 2

	// Iterate by adding 1 to the address. Cap the iteration count so a
	// malicious or fat-fingered /8 does not wedge the allocator (the
	// operator is expected to register tight prefixes; we cap at 65536
	// iterations as a safety valve).
	const maxIter = 65536
	cur := net
	for i := 0; i < maxIter; i++ {
		if (i != 0 || !skipNetwork) && !isExcludedOrAllocated(cur, excluded, allocated) {
			// cur is a candidate; but if this is the broadcast
			// address, skip it.
			if !skipBroadcast || !isBroadcast(cur, prefix) {
				return cur, true
			}
		}
		next := cur.Next()
		if !next.IsValid() {
			break
		}
		// Stop when we walk past the prefix.
		if !prefix.Contains(next) {
			break
		}
		cur = next
	}
	return netip.Addr{}, false
}

// isExcludedOrAllocated reports whether addr appears in either set. The
// sets hold the canonical netip.Addr.String() form.
func isExcludedOrAllocated(addr netip.Addr, excluded, allocated map[string]struct{}) bool {
	s := addr.String()
	if _, ok := excluded[s]; ok {
		return true
	}
	_, ok := allocated[s]
	return ok
}

// isBroadcast returns true when addr is the broadcast address of the
// prefix (IPv4 only; IPv6 has no broadcast). Computed as the OR of the
// network address with the inverse of the netmask.
func isBroadcast(addr netip.Addr, prefix netip.Prefix) bool {
	if !addr.Is4() {
		return false
	}
	net := prefix.Masked().Addr()
	bits := prefix.Bits()
	if bits >= 32 {
		return false
	}
	// Walk from network up; broadcast is the last address.
	cur := net
	hostCount := 1 << (32 - bits)
	for i := 0; i < hostCount-1; i++ {
		next := cur.Next()
		if !next.IsValid() {
			break
		}
		cur = next
	}
	return addr == cur
}
