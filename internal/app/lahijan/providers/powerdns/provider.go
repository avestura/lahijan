// Package powerdns: provider.go implements the providers.Provider interface
// (Name / Ping / Capabilities) for the PowerDNS driver.
package powerdns

import (
	"context"
	"fmt"
)

// Name returns "powerdns" — the internal driver identifier. NEVER surfaces to
// end users (per pillar 1 they see "dns").
func (p *Provider) Name() string { return "powerdns" }

// Ping probes the daemon. A nil return means the daemon is reachable and its
// REST API responded with a successful GET /api/v1/servers/localhost. Used by
// program.Start to log backend health at startup and by the future
// /api/v1/healthz.
//
// Ping also caches the server info so Capabilities() can return without a
// second round-trip on the first call.
func (p *Provider) Ping(ctx context.Context) error {
	ctx, span := startSpan(ctx, "ping")
	defer span.End()

	var info server
	if err := p.do(ctx, "GET", "servers/localhost", nil, &info); err != nil {
		setStatus(span, err)
		return err
	}
	if info.DaemonType != "authoritative" {
		err := fmt.Errorf("powerdns: daemon_type=%q (expected authoritative)", info.DaemonType)
		setStatus(span, err)
		return err
	}
	// Cache capabilities from the server info. The PDNS daemon advertises
	// the master/slave topology in its response indirectly via the zonesURL
	// and configURL; we treat the absence of a "type":"Slave" zone list as
	// "single-node MVP topology".
	caps := Capabilities{
		ServerVersion:   info.Version,
		APIVersion:      "1",
		DaemonType:      info.DaemonType,
		DNSSECSupported: true, // PDNS 4.x+ always has DNSSEC.
	}
	p.capabilitiesMu.Lock()
	p.capabilities = caps
	p.capabilitiesOK = true
	p.capabilitiesMu.Unlock()
	setStatus(span, nil)
	return nil
}

// Capabilities returns the cached daemon feature flags. If the cache is empty
// (e.g. before the first Ping), Capabilities calls Ping once to populate it.
// Returns a zero Capabilities when the daemon is unreachable.
func (p *Provider) Capabilities() Capabilities {
	p.capabilitiesMu.RLock()
	if p.capabilitiesOK {
		caps := p.capabilities
		p.capabilitiesMu.RUnlock()
		return caps
	}
	p.capabilitiesMu.RUnlock()

	// Populate the cache via Ping. A failed Ping returns a zero-value
	// Capabilities; the caller can retry by calling Ping explicitly.
	_ = p.Ping(context.Background())

	p.capabilitiesMu.RLock()
	defer p.capabilitiesMu.RUnlock()
	return p.capabilities
}

// ServerInfo returns the full daemon server info (for the admin debug page).
// It's the raw shape returned by GET /api/v1/servers/localhost.
type ServerInfo = server
