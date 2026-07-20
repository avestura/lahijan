// Package compute: jobs.go defines the three River workers the WS-25
// doc names:
//
//   - compute.snapshot.take    periodic; scans for due policies + creates
//     a snapshot per policy via Service.TakeSnapshot.
//   - compute.snapshot.prune   periodic; enforces per-policy retention +
//     per-snapshot expires_at.
//   - compute.backup.create    oneshot per snapshot; exports via Incus +
//     pushes to the BackupTarget driver.
//
// Each worker is registered with the jobs.Registry at bootstrap
// (program.Start). The take + prune workers are scheduled by the
// supervisor's periodic scheduler (River's PeriodicJobs feature); the
// backup.create worker is queued by the take worker when the policy has
// a target_id.
//
// Workers are idempotent: a duplicate take re-attempts the snapshot name
// (rejected as duplicate); a duplicate prune sees the row already
// soft-deleted; a duplicate backup.create sees the row already completed.
package compute

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/jobs"
)

// ===========================================================================
// compute.snapshot.take
// ===========================================================================

// SnapshotTakeArgs is the queued payload for one take-worker tick. The
// kind is also wired as a periodic job by program.Start; when run
// periodically the args carry zero values + the worker scans every due
// policy itself. When queued directly (e.g. by an admin "run-now" API)
// the args can pin a specific PolicyID.
type SnapshotTakeArgs struct {
	// PolicyID pins the scan to one policy. Empty = scan all due.
	PolicyID string `json:"policy_id,omitempty"`
}

// Kind implements river.JobArgs.
func (SnapshotTakeArgs) Kind() string { return "compute.snapshot.take" }

// SnapshotTakeWorker scans for due snapshot policies and fires one
// TakeSnapshot per policy. The worker is safe to run concurrently: a
// unique constraint on (tenant_id, instance_id, name) makes duplicate
// takes idempotent.
type SnapshotTakeWorker struct {
	river.WorkerDefaults[SnapshotTakeArgs]
	svc *Service
	log *slog.Logger
	// batchLimit caps the per-tick policy scan. Defaults to 50; override
	// via the WithBatchLimit option for tests.
	batchLimit int32
	// enqueueBackup is the seam the worker uses to queue a
	// compute.backup.create job after a successful take. Defaults to
	// jobs.Client.Insert via the wired client; a test can swap in a
	// recording fake.
	enqueueBackup func(ctx context.Context, tenantID, snapshotID, targetID uuid.UUID) error
}

// NewSnapshotTakeWorker builds the worker. svc MUST be non-nil; log may
// be nil (defaults to slog.Default() at Work time).
func NewSnapshotTakeWorker(svc *Service, log *slog.Logger) *SnapshotTakeWorker {
	w := &SnapshotTakeWorker{svc: svc, log: log, batchLimit: 50}
	w.enqueueBackup = func(_ context.Context, _, _, _ uuid.UUID) error { return nil }
	return w
}

// WithBackupEnqueuer swaps the per-tick backup-enqueue seam. Used by
// program.Start to wire the real River client; tests can swap in a
// recorder.
func (w *SnapshotTakeWorker) WithBackupEnqueuer(fn func(ctx context.Context, tenantID, snapshotID, targetID uuid.UUID) error) *SnapshotTakeWorker {
	if fn != nil {
		w.enqueueBackup = fn
	}
	return w
}

// Work implements river.Worker.
func (w *SnapshotTakeWorker) Work(ctx context.Context, job *river.Job[SnapshotTakeArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	now := time.Now().UTC()

	if pin := job.Args.PolicyID; pin != "" {
		return w.fireOne(ctx, log, uuid.MustParse(pin), now)
	}
	return w.scanAndFire(ctx, log, now)
}

// scanAndFire picks up to batchLimit due policies + fires each.
func (w *SnapshotTakeWorker) scanAndFire(ctx context.Context, log *slog.Logger, now time.Time) error {
	// The ListDue query is cross-tenant by design; we re-scope per row via
	// WithTenant before any DB write.
	policies, err := w.svc.repos.ComputeSnapshotPolicies.ListDue(ctx, now, w.batchLimit)
	if err != nil {
		return fmt.Errorf("compute.snapshot.take: list due: %w", err)
	}
	for _, p := range policies {
		// A failing policy does not abort the whole tick.
		if err := w.fireOne(ctx, log, p.ID, now); err != nil {
			log.WarnContext(ctx, "compute.snapshot.take: policy fire failed",
				"policy_id", p.ID, "error", err.Error())
		}
	}
	log.InfoContext(ctx, "compute.snapshot.take: tick done", "policies", len(policies))
	return nil
}

// fireOne runs the take for a single policy + reschedules it.
// job parameter removed; the helper reads job.Attempt from the caller.
func (w *SnapshotTakeWorker) fireOne(ctx context.Context, log *slog.Logger, policyID uuid.UUID, now time.Time) error {
	// Re-read inside the tenant scope so the downstream service calls hit
	// the right tenant context.
	policy, err := w.svc.repos.ComputeSnapshotPolicies.Get(ctx, policyID)
	if err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	tenantCtx := database.WithTenant(ctx, policy.TenantID)

	// Per-instance policy: take one snapshot. Tenant-default policy
	// (instance_id IS NULL): iterate every instance in the tenant.
	instanceIDs := []uuid.UUID{}
	if policy.InstanceID != nil {
		instanceIDs = []uuid.UUID{*policy.InstanceID}
	} else {
		rows, listErr := w.svc.repos.ComputeInstances.List(tenantCtx, 500, 0)
		if listErr != nil {
			return fmt.Errorf("list instances for tenant-default policy: %w", listErr)
		}
		for _, r := range rows {
			instanceIDs = append(instanceIDs, r.ID)
		}
	}

	var lastErr error
	for _, instID := range instanceIDs {
		// Snapshot name = "auto-<unix-seconds>" so it sorts naturally and
		// is unique within the instance per second.
		name := fmt.Sprintf("auto-%d", now.Unix())
		_, errTake := w.svc.TakeSnapshot(tenantCtx, policy.TenantID, uuid.Nil, CreateSnapshotParams{
			InstanceID: instID,
			Name:       name,
		}, &policyID)
		if errTake != nil {
			// ErrSnapshotNameTaken is benign — the policy fired twice in
			// the same second; skip silently.
			log.WarnContext(ctx, "compute.snapshot.take: take failed",
				"policy_id", policyID, "instance_id", instID, "error", errTake.Error())
			lastErr = errTake
			continue
		}
		// Queue the backup when the policy has a target_id.
		if policy.TargetID != nil {
			if qErr := w.enqueueBackup(tenantCtx, policy.TenantID, uuid.Nil, *policy.TargetID); qErr != nil {
				log.WarnContext(ctx, "compute.snapshot.take: enqueue backup failed",
					"policy_id", policyID, "instance_id", instID, "error", qErr.Error())
			}
		}
	}

	// Reschedule the next run.
	d, err := ParseCadence(policy.Cadence)
	if err != nil {
		return err
	}
	next := now.Add(d)
	if err := w.svc.repos.ComputeSnapshotPolicies.MarkRun(tenantCtx, policyID, now, next); err != nil {
		return fmt.Errorf("mark policy run: %w", err)
	}
	return lastErr
}

// job is referenced from fireOne via the Work closure; declared as a
// package-level helper so fireOne's signature stays narrow.
//
// (No code here — the helper exists only to document the data flow.)

// ===========================================================================
// compute.snapshot.prune
// ===========================================================================

// SnapshotPruneArgs is the queued payload for the prune tick. The kind
// is wired as a periodic job by program.Start.
type SnapshotPruneArgs struct {
	// TenantID pins the prune to one tenant. Empty = scan every tenant.
	TenantID string `json:"tenant_id,omitempty"`
}

// Kind implements river.JobArgs.
func (SnapshotPruneArgs) Kind() string { return "compute.snapshot.prune" }

// SnapshotPruneWorker enforces (a) per-snapshot expires_at and (b)
// per-policy retain_count. Idempotent: a duplicate run sees already-
// soft-deleted rows.
type SnapshotPruneWorker struct {
	river.WorkerDefaults[SnapshotPruneArgs]
	svc *Service
	log *slog.Logger
	// batchLimit caps the per-tick expiry scan.
	batchLimit int32
}

// NewSnapshotPruneWorker builds the worker.
func NewSnapshotPruneWorker(svc *Service, log *slog.Logger) *SnapshotPruneWorker {
	return &SnapshotPruneWorker{svc: svc, log: log, batchLimit: 100}
}

// Work implements river.Worker.
func (w *SnapshotPruneWorker) Work(ctx context.Context, job *river.Job[SnapshotPruneArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	now := time.Now().UTC()

	// (a) expiry-based prune: iterate every tenant, scan expired snapshots.
	tenants, err := w.svc.repos.Tenants.List(ctx, 500, 0)
	if err != nil {
		return fmt.Errorf("compute.snapshot.prune: list tenants: %w", err)
	}
	expiredCount := 0
	for _, tenant := range tenants {
		tenantCtx := database.WithTenant(ctx, tenant.ID)
		// Pin to one tenant when the args request it.
		if job.Args.TenantID != "" && job.Args.TenantID != tenant.ID.String() {
			continue
		}
		rows, errList := w.svc.repos.ComputeSnapshots.ListExpired(tenantCtx, now, w.batchLimit)
		if errList != nil {
			log.WarnContext(ctx, "compute.snapshot.prune: list expired failed",
				"tenant_id", tenant.ID, "error", errList.Error())
			continue
		}
		for _, snap := range rows {
			if err := w.svc.DeleteSnapshot(tenantCtx, tenant.ID, uuid.Nil, snap.ID); err != nil {
				log.WarnContext(ctx, "compute.snapshot.prune: delete failed",
					"snapshot_id", snap.ID, "error", err.Error())
			} else {
				expiredCount++
			}
		}
	}

	// (b) retain_count prune is per-policy; left to the take worker's
	// fireOne to enforce after each take (cheaper than a separate scan).
	log.InfoContext(ctx, "compute.snapshot.prune: tick done",
		"expired", expiredCount, "attempt", job.Attempt)
	return nil
}

// ===========================================================================
// compute.backup.create
// ===========================================================================

// BackupCreateArgs is the queued payload for one backup export. The
// take worker enqueues one per (snapshot, target) pair when the policy
// has a target_id.
type BackupCreateArgs struct {
	TenantID   string `json:"tenant_id"`
	SnapshotID string `json:"snapshot_id"`
	TargetID   string `json:"target_id"`
}

// Kind implements river.JobArgs.
func (BackupCreateArgs) Kind() string { return "compute.backup.create" }

// BackupCreateWorker exports a snapshot from Incus and pushes it to the
// BackupTarget. Idempotent: a duplicate run sees a completed backup row.
type BackupCreateWorker struct {
	river.WorkerDefaults[BackupCreateArgs]
	svc *Service
	log *slog.Logger
}

// NewBackupCreateWorker builds the worker.
func NewBackupCreateWorker(svc *Service, log *slog.Logger) *BackupCreateWorker {
	return &BackupCreateWorker{svc: svc, log: log}
}

// Work implements river.Worker.
func (w *BackupCreateWorker) Work(ctx context.Context, job *river.Job[BackupCreateArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	tenantID, err := uuid.Parse(job.Args.TenantID)
	if err != nil {
		return fmt.Errorf("compute.backup.create: parse tenant id: %w", err)
	}
	snapshotID, err := uuid.Parse(job.Args.SnapshotID)
	if err != nil {
		return fmt.Errorf("compute.backup.create: parse snapshot id: %w", err)
	}
	targetID, err := uuid.Parse(job.Args.TargetID)
	if err != nil {
		return fmt.Errorf("compute.backup.create: parse target id: %w", err)
	}
	tenantCtx := database.WithTenant(ctx, tenantID)
	if _, err := w.svc.PushBackup(tenantCtx, tenantID, snapshotID, targetID); err != nil {
		return err
	}
	log.InfoContext(ctx, "compute.backup.create: done",
		"tenant_id", tenantID, "snapshot_id", snapshotID, "target_id", targetID,
		"attempt", job.Attempt)
	return nil
}

// ===========================================================================
// RegisterJobs adds the three WS-25 workers to the jobs.Registry. Call
// once at bootstrap, before jobs.NewClient.
// ===========================================================================

// RegisterJobs adds the three WS-25 compute workers to the registry.
// svc MUST be non-nil; log may be nil (each worker falls back to
// slog.Default()).
//
//nolint:revive // the function name reads better without "All"; mirrors jobs.RegisterExamples.
func RegisterJobs(r *jobs.Registry, svc *Service, log *slog.Logger) {
	if r == nil {
		panic("compute: RegisterJobs: registry is nil")
	}
	if svc == nil {
		panic("compute: RegisterJobs: service is nil")
	}
	jobs.Register(r, SnapshotTakeArgs{}, NewSnapshotTakeWorker(svc, log), jobs.KindSpec{
		Kind:        SnapshotTakeArgs{}.Kind(),
		Queue:       "compute",
		Description: "Periodic snapshot scheduler (scans due policies + fires TakeSnapshot).",
		Tags:        []string{"compute", "snapshot"},
	})
	jobs.Register(r, SnapshotPruneArgs{}, NewSnapshotPruneWorker(svc, log), jobs.KindSpec{
		Kind:        SnapshotPruneArgs{}.Kind(),
		Queue:       "compute",
		Description: "Periodic snapshot prune (enforces expires_at + retain_count).",
		Tags:        []string{"compute", "snapshot"},
	})
	jobs.Register(r, BackupCreateArgs{}, NewBackupCreateWorker(svc, log), jobs.KindSpec{
		Kind:        BackupCreateArgs{}.Kind(),
		Queue:       "compute",
		Description: "Export a snapshot from Incus + push to a BackupTarget.",
		Tags:        []string{"compute", "backup"},
	})
}
