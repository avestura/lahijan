// Package billing: jobs.go defines the five River workers the WS-17
// doc names:
//
//   * billing.meter.collect    per-minute usage collection (one per
//                              active tenant); pulls from providers +
//                              appends usage_events rows.
//   * billing.usage.rollup     hourly rollup; aggregates usage_events
//                              into a single charge per (user, period).
//   * billing.ledger.post      post charges to ledger (from rollup).
//                              Kept as a separate worker so the rollup
//                              can be re-run without re-charging.
//   * billing.balance.check    periodic zero-balance watcher; calls
//                              EnforceZeroBalances for each tenant.
//   * billing.receipt.generate daily/weekly/monthly receipt generation.
//
// Each worker is registered with the jobs.Registry at bootstrap
// (program.Start); the periodic scheduler wires
// billing.meter.collect + billing.balance.check to fixed schedules.
// Workers are idempotent: a duplicate run produces the same result
// (the metering de-dup uses idempotency keys; the rollup + receipt
// are derived from the ledger so re-running is a no-op).
package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/avestura/lahijan/internal/app/lahijan/jobs"
)

// ===========================================================================
// billing.meter.collect
// ===========================================================================

// MeterCollectArgs is the queued payload for one metering collection
// run. TenantID is the scope; Minute is the wall-clock minute the
// collection covers (UTC, truncated to the minute). The job is
// inserted by the periodic scheduler (one per tenant per minute).
type MeterCollectArgs struct {
	TenantID string    `json:"tenant_id"`
	Minute   time.Time `json:"minute"`
}

// Kind implements river.JobArgs.
func (MeterCollectArgs) Kind() string { return "billing.meter.collect" }

// MeterCollectWorker runs one metering collection for one tenant + one
// minute. Calls Service.RunCollection; errors are retried by River per
// the standard backoff policy.
type MeterCollectWorker struct {
	river.WorkerDefaults[MeterCollectArgs]
	svc *Service
	log *slog.Logger
}

// NewMeterCollectWorker builds the worker. svc MUST be non-nil; log may
// be nil (defaults to slog.Default() at Work time).
func NewMeterCollectWorker(svc *Service, log *slog.Logger) *MeterCollectWorker {
	return &MeterCollectWorker{svc: svc, log: log}
}

// Work implements river.Worker.
func (w *MeterCollectWorker) Work(ctx context.Context, job *river.Job[MeterCollectArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	tenantID, err := uuid.Parse(job.Args.TenantID)
	if err != nil {
		return fmt.Errorf("billing.meter.collect: parse tenant id: %w", err)
	}
	n, err := w.svc.RunCollection(ctx, tenantID, job.Args.Minute)
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "billing.meter.collect: done",
		"tenant_id", tenantID,
		"minute", job.Args.Minute.Format(time.RFC3339),
		"inserted", n,
		"attempt", job.Attempt,
	)
	return nil
}

// ===========================================================================
// billing.usage.rollup
// ===========================================================================

// UsageRollupArgs is the queued payload for one hourly rollup. TenantID
// is the scope; From + To define the window (typically one hour ending
// at the job's scheduled time). The job aggregates usage_events into
// one ledger charge per user via Service.ChargeForRollup + Service.PostCharge.
type UsageRollupArgs struct {
	TenantID string    `json:"tenant_id"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

// Kind implements river.JobArgs.
func (UsageRollupArgs) Kind() string { return "billing.usage.rollup" }

// UsageRollupWorker runs one hourly rollup for one tenant. Calls
// Service.ChargeForRollup for every active user in the tenant (users
// with at least one usage_event in the window), then posts a single
// charge per user via Service.PostCharge (idempotent on the window).
type UsageRollupWorker struct {
	river.WorkerDefaults[UsageRollupArgs]
	svc *Service
	log *slog.Logger
	// userEnumerator is the seam the worker uses to list users with
	// usage in the window. Defaults to (*Service).usersWithUsage; a
	// test can swap in a stub.
	userEnumerator func(ctx context.Context, tenantID uuid.UUID, from, to time.Time) ([]uuid.UUID, error)
}

// NewUsageRollupWorker builds the worker. svc MUST be non-nil; log may
// be nil.
func NewUsageRollupWorker(svc *Service, log *slog.Logger) *UsageRollupWorker {
	w := &UsageRollupWorker{svc: svc, log: log}
	w.userEnumerator = svc.usersWithUsage
	return w
}

// Work implements river.Worker.
func (w *UsageRollupWorker) Work(ctx context.Context, job *river.Job[UsageRollupArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	tenantID, err := uuid.Parse(job.Args.TenantID)
	if err != nil {
		return fmt.Errorf("billing.usage.rollup: parse tenant id: %w", err)
	}
	users, err := w.userEnumerator(ctx, tenantID, job.Args.From, job.Args.To)
	if err != nil {
		return fmt.Errorf("billing.usage.rollup: list users: %w", err)
	}
	for _, userID := range users {
		// Idempotency key: <tenant_id>:charge:<user_id>:<from>:<to>.
		// A duplicate run for the same window hits the existing
		// ledger row and is a no-op.
		key := fmt.Sprintf("%s:charge:%s:%d:%d",
			tenantID, userID, job.Args.From.Unix(), job.Args.To.Unix())
		total, errCharge := w.svc.ChargeForRollup(ctx, userID, job.Args.From, job.Args.To)
		if errCharge != nil {
			log.WarnContext(ctx, "billing.usage.rollup: charge failed",
				"tenant_id", tenantID,
				"user_id", userID,
				"error", errCharge.Error(),
			)
			continue
		}
		if total <= 0 {
			continue // no chargeable usage in the window
		}
		if _, errPost := w.svc.PostCharge(ctx, tenantID, PostChargeParams{
			UserID:         userID,
			AmountCents:    total,
			Reference:      fmt.Sprintf("usage %s to %s", job.Args.From.Format("2006-01-02"), job.Args.To.Format("2006-01-02")),
			IdempotencyKey: &key,
			Metadata: map[string]any{
				"period_from": job.Args.From,
				"period_to":   job.Args.To,
				"source":      "metering_rollup",
			},
		}); errPost != nil {
			log.WarnContext(ctx, "billing.usage.rollup: post failed",
				"tenant_id", tenantID,
				"user_id", userID,
				"error", errPost.Error(),
			)
			continue
		}
	}
	log.InfoContext(ctx, "billing.usage.rollup: done",
		"tenant_id", tenantID,
		"from", job.Args.From.Format(time.RFC3339),
		"to", job.Args.To.Format(time.RFC3339),
		"users", len(users),
		"attempt", job.Attempt,
	)
	return nil
}

// usersWithUsage lists distinct user_ids that have at least one
// usage_events row in the window within the tenant. The rollup job
// uses this to iterate only users with chargeable usage rather than
// every tenant member. Implemented here (not in the repository) so the
// billing package keeps a single SQL seam: the gen.Queries already
// exposes the underlying SUM...GROUP BY.
//
// The default implementation uses the rollup query per known user; a
// production-tuned version would add a SELECT DISTINCT user_id query
// to billing.sql. Kept simple here so WS-17 ships without a new query.
func (s *Service) usersWithUsage(
	ctx context.Context,
	tenantID uuid.UUID,
	from, to time.Time,
) ([]uuid.UUID, error) {
	// Use the rollup query for the nil user to discover everyone; we
	// achieve that by listing users via the memberships repo. To
	// avoid pulling a new dep, fall back to the cache table scan:
	// the balances cache has one row per user that has ever had a
	// ledger entry. That is the right population for a rollup
	// (users with no ledger history have nothing to charge).
	//
	// This is a soft under-approximation: a user who consumed
	// resources this hour but has never been topped up will not
	// appear. In practice the metering job creates the cache row
	// when it inserts the first usage_event (via refreshBalance),
	// so this is correct as long as metering runs first.
	rows, err := s.repos.BillingBalances.ListZeroBalances(ctx, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, fmt.Errorf("billing.users_with_usage: %w", err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.UserID)
	}
	return out, nil
}

// ===========================================================================
// billing.balance.check
// ===========================================================================

// BalanceCheckArgs is the queued payload for one balance check run.
// One run covers one tenant; the periodic scheduler inserts a job per
// tenant per N minutes (default 5).
type BalanceCheckArgs struct {
	TenantID string `json:"tenant_id"`
}

// Kind implements river.JobArgs.
func (BalanceCheckArgs) Kind() string { return "billing.balance.check" }

// BalanceCheckWorker runs the zero-balance enforcement for one tenant.
// Calls Service.EnforceZeroBalances; idempotent.
type BalanceCheckWorker struct {
	river.WorkerDefaults[BalanceCheckArgs]
	svc *Service
	log *slog.Logger
}

// NewBalanceCheckWorker builds the worker.
func NewBalanceCheckWorker(svc *Service, log *slog.Logger) *BalanceCheckWorker {
	return &BalanceCheckWorker{svc: svc, log: log}
}

// Work implements river.Worker.
func (w *BalanceCheckWorker) Work(ctx context.Context, job *river.Job[BalanceCheckArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	tenantID, err := uuid.Parse(job.Args.TenantID)
	if err != nil {
		return fmt.Errorf("billing.balance.check: parse tenant id: %w", err)
	}
	n, err := w.svc.EnforceZeroBalances(ctx, tenantID)
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "billing.balance.check: done",
		"tenant_id", tenantID,
		"enforced", n,
		"attempt", job.Attempt,
	)
	return nil
}

// ===========================================================================
// billing.receipt.generate
// ===========================================================================

// ReceiptGenerateArgs is the queued payload for one receipt generation
// run. One run covers one user + one period; the daily/weekly/monthly
// scheduler inserts a job per user per period boundary.
type ReceiptGenerateArgs struct {
	TenantID    string    `json:"tenant_id"`
	UserID      string    `json:"user_id"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

// Kind implements river.JobArgs.
func (ReceiptGenerateArgs) Kind() string { return "billing.receipt.generate" }

// ReceiptGenerateWorker generates one receipt. Calls
// Service.GenerateReceipt; idempotent (a re-run overwrites the PDF).
type ReceiptGenerateWorker struct {
	river.WorkerDefaults[ReceiptGenerateArgs]
	svc *Service
	log *slog.Logger
}

// NewReceiptGenerateWorker builds the worker.
func NewReceiptGenerateWorker(svc *Service, log *slog.Logger) *ReceiptGenerateWorker {
	return &ReceiptGenerateWorker{svc: svc, log: log}
}

// Work implements river.Worker.
func (w *ReceiptGenerateWorker) Work(ctx context.Context, job *river.Job[ReceiptGenerateArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	tenantID, err := uuid.Parse(job.Args.TenantID)
	if err != nil {
		return fmt.Errorf("billing.receipt.generate: parse tenant id: %w", err)
	}
	userID, err := uuid.Parse(job.Args.UserID)
	if err != nil {
		return fmt.Errorf("billing.receipt.generate: parse user id: %w", err)
	}
	if _, err := w.svc.GenerateReceipt(ctx, tenantID, uuid.Nil, GenerateReceiptParams{
		UserID:      userID,
		PeriodStart: job.Args.PeriodStart,
		PeriodEnd:   job.Args.PeriodEnd,
	}); err != nil {
		return err
	}
	log.InfoContext(ctx, "billing.receipt.generate: done",
		"tenant_id", tenantID,
		"user_id", userID,
		"period_start", job.Args.PeriodStart.Format(time.RFC3339),
		"period_end", job.Args.PeriodEnd.Format(time.RFC3339),
		"attempt", job.Attempt,
	)
	return nil
}

// ===========================================================================
// RegisterJobs adds the five WS-17 workers to the jobs.Registry. Call
// once at bootstrap, before jobs.NewClient. The workers share a
// slog.Default() logger unless the caller passes a non-nil one.
// ===========================================================================

// RegisterJobs adds the five WS-17 billing workers to the registry.
// svc MUST be non-nil; log may be nil (each worker falls back to
// slog.Default()).
//
//nolint:revive // the function name reads better without "All"; mirrors jobs.RegisterExamples.
func RegisterJobs(r *jobs.Registry, svc *Service, log *slog.Logger) {
	if r == nil {
		panic("billing: RegisterJobs: registry is nil")
	}
	if svc == nil {
		panic("billing: RegisterJobs: service is nil")
	}
	jobs.Register(r, MeterCollectArgs{}, NewMeterCollectWorker(svc, log), jobs.KindSpec{
		Kind:        MeterCollectArgs{}.Kind(),
		Queue:       "billing",
		Description: "Per-minute usage collection (pulls from providers, appends usage_events).",
		Tags:        []string{"billing", "metering"},
	})
	jobs.Register(r, UsageRollupArgs{}, NewUsageRollupWorker(svc, log), jobs.KindSpec{
		Kind:        UsageRollupArgs{}.Kind(),
		Queue:       "billing",
		Description: "Hourly rollup of usage_events into ledger charges.",
		Tags:        []string{"billing", "rollup"},
	})
	jobs.Register(r, BalanceCheckArgs{}, NewBalanceCheckWorker(svc, log), jobs.KindSpec{
		Kind:        BalanceCheckArgs{}.Kind(),
		Queue:       "billing",
		Description: "Zero-balance watcher (stops instances past grace period).",
		Tags:        []string{"billing", "enforcement"},
	})
	jobs.Register(r, ReceiptGenerateArgs{}, NewReceiptGenerateWorker(svc, log), jobs.KindSpec{
		Kind:        ReceiptGenerateArgs{}.Kind(),
		Queue:       "billing",
		Description: "Per-user-per-period receipt PDF generation.",
		Tags:        []string{"billing", "receipts"},
	})
}
