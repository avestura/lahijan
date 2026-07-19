// Package billing: enforce.go implements the zero-balance enforcement
// watcher. Once a user's balance hits zero + the configured grace
// period (24h default per WS-17 Open Questions item 3) expires, the
// watcher calls the Enforcer to stop the user's long-running resources
// (compute instances).
//
// The watcher is invoked by the billing.balance.check River job. It is
// also safe to call directly from integration tests.
package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// EnforceZeroBalances scans every user in the tenant whose cached
// balance is <= 0 and whose last_entry_at is older than the grace
// period. For each, it calls Enforcer.StopAllForUser. Idempotent: a
// second call with the same cutoff does nothing new (the enforcement
// decision is sticky; the cache row stays at balance <= 0 until the
// user tops up).
//
// Returns the number of users acted on + any error from the underlying
// scan. Errors from individual StopAllForUser calls are logged at warn
// level but do not abort the scan.
func (s *Service) EnforceZeroBalances(
	ctx context.Context,
	tenantID uuid.UUID,
) (int, error) {
	cutoff := time.Now().UTC().Add(-s.config.GracePeriod)
	rows, err := s.repos.BillingBalances.ListZeroBalances(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("billing.enforce: list zero balances: %w", err)
	}
	count := 0
	for _, row := range rows {
		// Tenant scoping is enforced at the repository seam; the
		// rows returned here are already scoped to tenantID.
		reason := fmt.Sprintf("zero balance since %s", row.LastEntryAt.Format(time.RFC3339))
		if errStop := s.enforcer.StopAllForUser(ctx, tenantID, row.UserID, reason); errStop != nil {
			slog.WarnContext(ctx, "billing.enforce: stop failed",
				"tenant_id", tenantID,
				"user_id", row.UserID,
				"error", errStop.Error(),
			)
			continue
		}
		count++
		slog.InfoContext(ctx, "billing.enforce: stopped user resources",
			"tenant_id", tenantID,
			"user_id", row.UserID,
			"balance_cents", row.BalanceCents,
			"last_entry_at", row.LastEntryAt.Format(time.RFC3339),
		)
	}
	return count, nil
}
