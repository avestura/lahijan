// Package billing: meter.go is the per-minute metering entry point.
// The billing.meter.collect River job calls RunCollection once per
// tenant per minute; the configured Meter implementation reads the
// current usage snapshot from each provider (Incus / PowerDNS /
// SeaweedFS) and appends usage_events rows.
//
// The default Meter implementation in WS-17 is NoopMeter (returns 0
// rows). A follow-up will wire the real per-provider meters; the seam
// is in place so the metering pipeline (collect -> rollup -> ledger)
// can be exercised end-to-end without standing up a real Incus.
package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RunCollection invokes the configured Meter for the tenant + minute.
// Returns the number of usage_events rows the meter inserted. Zero is
// a normal "nothing changed since last minute" result.
//
// The minute is aligned to the wall-clock minute by the caller (the
// River job); the Meter implementation decides the started_at /
// ended_at of each row.
func (s *Service) RunCollection(
	ctx context.Context,
	tenantID uuid.UUID,
	minute time.Time,
) (int, error) {
	if minute.IsZero() {
		minute = time.Now().UTC().Truncate(time.Minute)
	}
	n, err := s.meter.Collect(ctx, tenantID, minute)
	if err != nil {
		return 0, fmt.Errorf("billing.meter.collect: %w", err)
	}
	return n, nil
}
