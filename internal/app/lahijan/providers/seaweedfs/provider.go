// Package seaweedfs: provider.go implements the providers.Provider interface
// (Name / Ping / Capabilities) for the SeaweedFS driver.
package seaweedfs

import (
	"context"
	"errors"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// Name returns "seaweedfs" — the internal driver identifier. NEVER surfaces to
// end users (per pillar 1 they see "object storage" / "S3").
func (p *Provider) Name() string { return "seaweedfs" }

// Ping probes the backend. A nil return means both the S3 endpoint and
// the Filer endpoint are reachable and responsive. Used by program.Start
// to log backend health at startup and by the future /api/v1/healthz.
//
// Ping also caches the capabilities so Capabilities() can return without
// a second round-trip on the first call.
func (p *Provider) Ping(ctx context.Context) error {
	ctx, span := startSpan(ctx, "ping")
	defer span.End()

	// Probe the S3 endpoint with a ListBuckets call. This is the
	// cheapest request that still exercises SigV4 + the credential
	// chain; a HeadBucket on a fake name would return 404 (still a
	// successful round-trip) but the SDK treats 404 as an error which
	// complicates the call.
	if _, err := p.s3.ListBuckets(ctx, &awss3.ListBucketsInput{}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: ping s3: %w", translated)
	}

	// Probe the Filer root.
	status, err := p.filer.GetStatus(ctx)
	if err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: ping filer: %w", err)
	}

	// Cache the capabilities. ClusterMode is true when the topology
	// summary reports >1 volume node (best-effort heuristic; the prod
	// split-compose deploy will report a real topology).
	caps := Capabilities{
		ClusterMode:       clusterModeFromStatus(status),
		QuotasEnforced:    true, // SeaweedFS honours Filer quotas when set
		PresignSupported:  true,
		ServerVersion:     status.Version,
		RemoteReplication: false, // Phase 7 (WS-29) lands this
	}
	p.capabilitiesMu.Lock()
	p.capabilities = caps
	p.capabilitiesOK = true
	p.capabilitiesMu.Unlock()
	setStatus(span, nil)
	return nil
}

// Capabilities returns the cached daemon feature flags. If the cache is
// empty (e.g. before the first Ping), Capabilities calls Ping once to
// populate it. Returns a zero Capabilities when the daemon is unreachable.
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

// clusterModeFromStatus is a best-effort heuristic that returns true when
// the Filer's status payload advertises a multi-node topology (>=2
// volume servers). The dev `weed mini` deploy returns 1; a prod
// master+volume+filer+s3 split returns >=3.
func clusterModeFromStatus(status *FilerStatus) bool {
	if status == nil || status.Topology == nil {
		return false
	}
	return status.Topology.VolumeCount >= 2
}

// errProviderIncomplete is returned by NewClient-style helpers when the
// caller injects an inconsistent set of dependencies. Kept unexported so
// callers do not depend on the exact wording.
var errProviderIncomplete = errors.New("seaweedfs: provider dependencies incomplete")
