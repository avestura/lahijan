// Package providers is the parent package for the three infrastructure
// backend drivers (Incus, PowerDNS, SeaweedFS). Each concrete driver lives
// under its own subpackage (providers/incus, providers/powerdns,
// providers/seaweedfs) and implements the Provider interface declared here.
//
// Per ADR-0009 ("Multi-node-ready from day 1") provider connections are
// configurable via conf; no driver assumes a fixed backend endpoint. Per
// pillar 1 the provider names "incus" / "powerdns" / "seaweedfs" are NEVER
// surfaced to end users — Lahijan is "compute" / "dns" / "object storage" to
// them. The internal Name() string is for logs, metrics, and admin tooling.
package providers

import (
	"context"
)

// Provider is the minimal surface every infrastructure backend implements.
// Each concrete driver extends this with area-specific methods (instances,
// zones, buckets, ...). Provider calls never carry tenant_id — that mapping
// happens in the service layer (one tenant maps to one Incus project, one
// PowerDNS zone set, one SeaweedFS user).
//
// The interface deliberately stays at three methods so the interfacebloat
// lint cap (max 7) is not blown by aggregating providers in tests or in the
// health-probe loop.
type Provider interface {
	// Name is the driver identifier — "incus", "powerdns", "seaweedfs".
	// Internal-only; never reaches end users (pillar 1).
	Name() string

	// Ping is the health probe. Returns nil if the backend is reachable and
	// responsive. Used by program.Start to log backend health at startup and
	// by the (future) /api/v1/healthz endpoint to compose a 200/503.
	Ping(ctx context.Context) error

	// Capabilities advertises which optional features the backend supports.
	// The compute module (WS-14) reads this to gate UI affordances (e.g.
	// cluster-mode operations, VM support, snapshot scheduling).
	Capabilities() Capabilities
}

// Capabilities is the per-provider feature-flag bundle. Every driver fills
// only the fields relevant to its backend; the rest stay at their zero value.
// Adding a new capability flag does not break existing implementations
// (structural typing on a struct, not a method on the interface).
type Capabilities struct {
	// ClusterMode is true when the backend runs in a multi-node cluster
	// (Incus cluster, PowerDNS master/slave, SeaweedFS master+volume+filer).
	// WS-26 flips this for Incus.
	ClusterMode bool

	// VMSupport is true when the backend can serve virtual machines in
	// addition to system containers (Incus-specific; always false for PDNS
	// and SeaweedFS).
	VMSupport bool

	// RemoteReplication is true when the backend can fan data out to a
	// remote peer (Incus storage copy, PowerDNS zone AXFR, SeaweedFS
	// replication).
	RemoteReplication bool

	// QuotasEnforced is true when the backend itself enforces the per-tenant
	// quotas Lahijan sets (e.g. Incus project limits). When false Lahijan
	// enforces quotas in its own service layer.
	QuotasEnforced bool
}
