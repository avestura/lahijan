// Package jobs: examples.go contains the four no-op example workers the
// WS-09 doc names: a metering rollup, an audit-log prune, an email send, and
// a snapshot placeholder. Their Work() bodies are intentionally minimal —
// real implementations land in their respective domain modules (WS-17
// billing, audit retention follow-up, WS-06 email, WS-25 snapshots).
//
// They exist so that:
//
//  1. The River client always has at least one worker to execute, which
//     lets the integration test prove the queue + retry + DLQ + admin API
//     loop works end-to-end before any domain module ships.
//  2. The admin UI has a non-empty catalog to render during development.
//  3. The OTel middleware has real traffic to emit spans/metrics for.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// ---------------------------------------------------------------------------
// Metering rollup — placeholder for WS-17 (billing & metering).
// ---------------------------------------------------------------------------

// MeteringRollupArgs is the queued payload for periodic usage aggregation.
// TenantID carries the scope so the worker can call database.WithTenant
// before any DB write; zero means "global rollup across all tenants".
type MeteringRollupArgs struct {
	TenantID string    `json:"tenant_id,omitempty"`
	Period   time.Time `json:"period"`
}

// Kind implements river.JobArgs.
func (MeteringRollupArgs) Kind() string { return "billing.usage.rollup" }

// MeteringRollupWorker is the no-op example worker. WS-17 replaces Work()
// with the real aggregation logic.
type MeteringRollupWorker struct {
	river.WorkerDefaults[MeteringRollupArgs]
	log *slog.Logger
}

// NewMeteringRollupWorker builds the worker. log may be nil; defaults to
// slog.Default() at Work() time.
func NewMeteringRollupWorker(log *slog.Logger) *MeteringRollupWorker {
	return &MeteringRollupWorker{log: log}
}

// Work implements river.Worker. Logs the period and returns nil; the real
// implementation lives in WS-17.
func (w *MeteringRollupWorker) Work(ctx context.Context, job *river.Job[MeteringRollupArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(
		ctx, "jobs: metering rollup placeholder",
		"tenant_id", job.Args.TenantID,
		"period", job.Args.Period.Format(time.RFC3339),
		"attempt", job.Attempt,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Audit log prune — example for periodic audit retention pruning.
// ---------------------------------------------------------------------------

// AuditLogPruneArgs is the queued payload for periodic audit-log pruning.
// RetentionDays is how many days of audit rows to keep; the worker deletes
// rows older than now - RetentionDays. The real implementation must respect
// the audit_log append-only trigger by calling a dedicated truncate path
// (a future audit-specific migration adds a SUPERUSER-only procedure for it).
type AuditLogPruneArgs struct {
	TenantID      string `json:"tenant_id,omitempty"`
	RetentionDays int    `json:"retention_days"`
}

// Kind implements river.JobArgs.
func (AuditLogPruneArgs) Kind() string { return "auditlog.prune" }

// AuditLogPruneWorker is the no-op example worker.
type AuditLogPruneWorker struct {
	river.WorkerDefaults[AuditLogPruneArgs]
	log *slog.Logger
}

// NewAuditLogPruneWorker builds the worker.
func NewAuditLogPruneWorker(log *slog.Logger) *AuditLogPruneWorker {
	return &AuditLogPruneWorker{log: log}
}

// Work implements river.Worker.
func (w *AuditLogPruneWorker) Work(ctx context.Context, job *river.Job[AuditLogPruneArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(
		ctx, "jobs: audit log prune placeholder",
		"tenant_id", job.Args.TenantID,
		"retention_days", job.Args.RetentionDays,
		"attempt", job.Attempt,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Async email send — used by WS-06 when SMTP delivery is async.
// ---------------------------------------------------------------------------

// NotifyEmailSendArgs is the queued payload for an async email send. The
// fields mirror what notify/email.SMTPSender already needs; the worker hands
// them back to the SMTP client. BodyHTML is sent as-is; templates are
// rendered by the caller so the worker stays payload-agnostic.
type NotifyEmailSendArgs struct {
	To       string `json:"to"`
	Subject  string `json:"subject"`
	BodyHTML string `json:"body_html"`
	BodyText string `json:"body_text,omitempty"`
}

// Kind implements river.JobArgs.
func (NotifyEmailSendArgs) Kind() string { return "notify.email.send" }

// NotifyEmailSendWorker is the no-op example worker. WS-06 already sends
// email synchronously via SMTP; this kind exists for callers that prefer the
// async path (e.g. high-volume notifications).
type NotifyEmailSendWorker struct {
	river.WorkerDefaults[NotifyEmailSendArgs]
	log *slog.Logger
}

// NewNotifyEmailSendWorker builds the worker.
func NewNotifyEmailSendWorker(log *slog.Logger) *NotifyEmailSendWorker {
	return &NotifyEmailSendWorker{log: log}
}

// Work implements river.Worker.
func (w *NotifyEmailSendWorker) Work(ctx context.Context, job *river.Job[NotifyEmailSendArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	log.InfoContext(
		ctx, "jobs: notify email send placeholder",
		"to", job.Args.To,
		"subject", job.Args.Subject,
		"attempt", job.Attempt,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Compute instance snapshot — the WS-09 doc listed this as a placeholder
// for WS-25. The real implementation lives in
// internal/app/lahijan/compute/jobs_snapshots.go (WS-25) and ships three
// kinds: compute.snapshot.take, compute.snapshot.prune, compute.backup.create.
// The placeholder Args/Worker types + the registration call were removed
// from RegisterExamples so the WS-25 workers own the kind namespace.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// AlwaysFailArgs is a tiny kind used by integration tests to prove the
// retry -> DLQ -> admin-API retry loop. Not used outside tests; declared here
// (not in a _test.go file) because the admin API integration test lives in
// the api package and needs to import it.
// ---------------------------------------------------------------------------

// AlwaysFailArgs always fails; used to exercise retry / DLQ paths.
type AlwaysFailArgs struct{ Reason string }

// Kind implements river.JobArgs.
func (AlwaysFailArgs) Kind() string { return "test.always_fail" }

// AlwaysFailWorker fails every Work call with the args' Reason.
type AlwaysFailWorker struct {
	river.WorkerDefaults[AlwaysFailArgs]
}

// Work implements river.Worker.
func (AlwaysFailWorker) Work(_ context.Context, job *river.Job[AlwaysFailArgs]) error {
	return errors.New("jobs: always_fail: " + job.Args.Reason)
}

// ---------------------------------------------------------------------------
// RegisterExamples registers the four WS-09 example kinds on the registry.
// Call once at bootstrap, before NewClient. Workers share a slog.Default()
// logger unless the caller passes a non-nil one.
// ---------------------------------------------------------------------------

// RegisterExamples adds the three WS-09 example workers to the registry. log
// may be nil; each worker falls back to slog.Default(). The WS-09 doc
// originally listed four examples including a placeholder snapshot kind;
// WS-25 replaces that placeholder with the real workers in the compute
// package (compute.snapshot.take / prune / backup.create).
func RegisterExamples(r *Registry, log *slog.Logger) {
	if r == nil {
		panic("jobs: RegisterExamples: registry is nil")
	}
	Register(r, MeteringRollupArgs{}, NewMeteringRollupWorker(log), KindSpec{
		Kind:        MeteringRollupArgs{}.Kind(),
		Queue:       "billing",
		Description: "Periodic usage aggregation (filled in by WS-17).",
		Tags:        []string{"billing", "metering"},
	})
	RegisterNonBillingExamples(r, log)
}

// RegisterNonBillingExamples adds the example workers that no domain module
// has replaced yet (audit prune, email send). program.Start uses this instead
// of RegisterExamples because billing.RegisterJobs registers the real WS-17
// "billing.usage.rollup" worker, and registering the placeholder too panics
// with a duplicate-kind error at boot.
func RegisterNonBillingExamples(r *Registry, log *slog.Logger) {
	if r == nil {
		panic("jobs: RegisterNonBillingExamples: registry is nil")
	}
	Register(r, AuditLogPruneArgs{}, NewAuditLogPruneWorker(log), KindSpec{
		Kind:        AuditLogPruneArgs{}.Kind(),
		Queue:       "maintenance",
		Description: "Periodic audit-log pruning per retention policy.",
		Tags:        []string{"audit", "maintenance"},
	})
	Register(r, NotifyEmailSendArgs{}, NewNotifyEmailSendWorker(log), KindSpec{
		Kind:        NotifyEmailSendArgs{}.Kind(),
		Queue:       "notifications",
		Description: "Asynchronous outbound email send (used by WS-06 async path).",
		Tags:        []string{"notify", "email"},
	})
}
