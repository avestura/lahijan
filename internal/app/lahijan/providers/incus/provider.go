// Package incus: provider.go implements the providers.Provider interface
// (Name / Ping / Capabilities) for the Incus driver.
package incus

import (
	"context"
	"encoding/json"
	"fmt"
)

// Name returns "incus" — the internal driver identifier. NEVER surfaces to
// end users (per pillar 1 they see "compute").
func (p *Provider) Name() string { return "incus" }

// Ping probes the daemon. A nil return means the daemon is reachable and
// its REST API responded with a successful /1.0 GET. Used by program.Start
// to log backend health at startup and by the future /api/v1/healthz.
//
// Ping also caches the server info so Capabilities() can return without a
// second round-trip on the first call.
func (p *Provider) Ping(ctx context.Context) error {
	ctx, span := startSpan(ctx, "ping")
	defer span.End()

	raw, err := p.do(ctx, "GET", "", nil)
	if err != nil {
		setStatus(span, err)
		return err
	}
	var info serverInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		setStatus(span, err)
		return fmt.Errorf("incus: decode server info: %w", err)
	}
	if info.APIStatus != "stable" {
		err := fmt.Errorf("incus: daemon api_status=%q (expected stable)", info.APIStatus)
		setStatus(span, err)
		return err
	}
	// Cache capabilities from the server info. VM support is inferred from
	// the environment qemu availability; the daemon does not advertise it
	// directly, so we set it via the /1.0 virtual-machine probe (a no-op
	// for now; WS-14 can flip this with a real probe).
	caps := Capabilities{
		ClusterMode:   info.ServerClustered,
		ServerVersion: info.ServerVersion,
		APIVersion:    info.APIVersion,
		ServerName:    info.ServerName,
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
	// Capabilities; the caller can Retry by calling Ping explicitly.
	_ = p.Ping(context.Background())

	p.capabilitiesMu.RLock()
	defer p.capabilitiesMu.RUnlock()
	return p.capabilities
}

// ServerInfo returns the full daemon server info (for the admin debug page).
// It's the raw shape returned by GET /1.0.
type ServerInfo = serverInfo
