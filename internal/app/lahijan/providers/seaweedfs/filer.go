// Package seaweedfs: filer.go exposes the SeaweedFS Filer REST surface
// Lahijan needs: a status probe (used by Ping + GetClusterStatus) and a
// raw-metadata read helper (used by the admin debug page in WS-16).
//
// IAM and per-bucket quota configuration are handled in iam.go and
// quotas.go respectively; this file is concerned with the Filer root
// surface only.
package seaweedfs

import (
	"context"
	"fmt"
)

// GetClusterStatus probes the Filer root and returns a simplified
// volume / capacity summary. Used by Ping + the admin debug page.
func (p *Provider) GetClusterStatus(ctx context.Context) (*FilerVolumeInfo, error) {
	ctx, span := startSpan(ctx, "filer.status")
	defer span.End()

	status, err := p.filer.GetStatus(ctx)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: filer.status: %w", err)
	}
	info := &FilerVolumeInfo{
		Version: status.Version,
	}
	if status.Topology != nil {
		info.FreeBytes = status.Topology.Free
		info.TotalBytes = status.Topology.Max
		info.VolumeCount = status.Topology.VolumeCount
		info.ActiveVolumeCount = status.Topology.ActiveVolumeCount
	}
	setStatus(span, nil)
	return info, nil
}

// PingFiler is the Filer-only half of Ping. It is a separate method so
// callers that already know the S3 endpoint is up (e.g. a previous Ping
// cached the capabilities) can probe just the Filer half. Used by the
// healthz endpoint to produce a per-subsystem status.
func (p *Provider) PingFiler(ctx context.Context) error {
	ctx, span := startSpan(ctx, "filer.ping")
	defer span.End()

	if _, err := p.GetClusterStatus(ctx); err != nil {
		setStatus(span, err)
		return err
	}
	setStatus(span, nil)
	return nil
}
