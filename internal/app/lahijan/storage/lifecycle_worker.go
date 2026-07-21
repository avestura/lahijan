// Package storage: lifecycle_worker.go is the WS-29 lifecycle evaluator
// River worker. Per ADR-0036 sub-decision B the worker enforces the
// configured rules independently so the platform works even when
// SeaweedFS' native lifecycle support is incomplete (WS-29 "Notes"
// caveat). Every action emits the matching WASM event
// (storage.object.deleted / storage.lifecycle.transitioned) so plugins
// react uniformly to lifecycle-driven and user-driven changes.
//
// The worker is registered with the jobs.Registry at bootstrap
// (program.Start) and scheduled by the supervisor's periodic scheduler
// (River's PeriodicJobs feature). The default cadence is every 15
// minutes; an operator can override via the
// storage.lifecycle.evaluator.cadence_seconds config knob (WS-04 lands
// the config surface).
//
// The worker is idempotent: a duplicate tick re-evaluates the same
// rules and finds the same expired objects; an S3 DeleteObject on an
// already-deleted key is a no-op. A failing rule does not abort the
// tick; the next tick will retry.
package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/jobs"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// LifecycleEvaluateArgs is the queued payload for one evaluator tick.
// The kind is wired as a periodic job by program.Start; when run
// periodically the args carry zero values + the worker scans every
// tenant itself. When queued directly (e.g. by an admin "run-now" API)
// the args can pin a specific TenantID + BucketID.
type LifecycleEvaluateArgs struct {
	// TenantID pins the scan to one tenant. Empty = scan every tenant.
	TenantID string `json:"tenant_id,omitempty"`
	// BucketID pins the scan to one bucket within TenantID. Empty =
	// scan every bucket in the tenant (or every tenant).
	BucketID string `json:"bucket_id,omitempty"`
}

// Kind implements river.JobArgs.
func (LifecycleEvaluateArgs) Kind() string { return "storage.lifecycle.evaluate" }

// LifecycleEvaluateWorker scans the storage_lifecycle_rules table for
// enabled rules and acts on every object the rule matches + is due.
//
// The worker is safe to run concurrently: every action is an idempotent
// S3 DeleteObject / AbortMultipartUpload.
type LifecycleEvaluateWorker struct {
	river.WorkerDefaults[LifecycleEvaluateArgs]
	svc *Service
	log *slog.Logger
	// evalProvider is the narrow seam the worker needs from the
	// SeaweedFS provider. The production wiring hands the same provider
	// the Service holds; tests can swap in a recording fake. Kept
	// separate from swProvider so tests for the worker do not need to
	// implement the full surface.
	evalProvider lifecycleEvalProvider
	// tenantScanner is the seam the worker uses to enumerate every
	// tenant that has enabled lifecycle rules. Defaults to
	// listTenantsWithEnabledLifecycleRules via the repos; tests can
	// swap in a stub.
	tenantScanner func(ctx context.Context) ([]uuid.UUID, error)
	// clock is the time source. Defaults to time.Now; tests can inject
	// a stub.
	clock func() time.Time
}

// lifecycleEvalProvider is the narrow SeaweedFS surface the worker
// touches. Defined here so tests inject a fake without implementing
// the full swProvider union.
type lifecycleEvalProvider interface {
	swVersioningOps
	swLifecycleEvalOps
}

// NewLifecycleEvaluateWorker builds the worker. svc + evalProvider
// MUST be non-nil; log may be nil (defaults to slog.Default() at Work
// time). The evalProvider is typically the same *seaweedfs.Provider the
// Service holds; tests can inject a recording fake.
func NewLifecycleEvaluateWorker(svc *Service, evalProvider lifecycleEvalProvider, log *slog.Logger) *LifecycleEvaluateWorker {
	w := &LifecycleEvaluateWorker{
		svc:          svc,
		evalProvider: evalProvider,
		log:          log,
		clock:        func() time.Time { return time.Now().UTC() },
	}
	w.tenantScanner = w.scanTenants
	return w
}

// WithClock injects a clock for deterministic tests.
func (w *LifecycleEvaluateWorker) WithClock(fn func() time.Time) *LifecycleEvaluateWorker {
	if fn != nil {
		w.clock = fn
	}
	return w
}

// WithTenantScanner injects a tenant-scanner for deterministic tests.
func (w *LifecycleEvaluateWorker) WithTenantScanner(fn func(ctx context.Context) ([]uuid.UUID, error)) *LifecycleEvaluateWorker {
	if fn != nil {
		w.tenantScanner = fn
	}
	return w
}

// Work implements river.Worker.
func (w *LifecycleEvaluateWorker) Work(ctx context.Context, job *river.Job[LifecycleEvaluateArgs]) error {
	log := w.log
	if log == nil {
		log = slog.Default()
	}
	now := w.clock()

	if pin := job.Args.TenantID; pin != "" {
		tid, err := uuid.Parse(pin)
		if err != nil {
			return fmt.Errorf("storage.lifecycle.evaluate: bad tenant_id %q: %w", pin, err)
		}
		bucketID := uuid.Nil
		if job.Args.BucketID != "" {
			bid, err := uuid.Parse(job.Args.BucketID)
			if err != nil {
				return fmt.Errorf("storage.lifecycle.evaluate: bad bucket_id %q: %w", job.Args.BucketID, err)
			}
			bucketID = bid
		}
		return w.evalTenant(ctx, log, tid, bucketID, now)
	}

	tenants, err := w.tenantScanner(ctx)
	if err != nil {
		return fmt.Errorf("storage.lifecycle.evaluate: scan tenants: %w", err)
	}
	for _, tid := range tenants {
		if err := w.evalTenant(ctx, log, tid, uuid.Nil, now); err != nil {
			log.WarnContext(ctx, "storage.lifecycle.evaluate: tenant tick failed",
				"tenant_id", tid, "error", err.Error())
		}
	}
	log.InfoContext(ctx, "storage.lifecycle.evaluate: tick done", "tenants", len(tenants))
	return nil
}

// evalTenant runs the evaluator for one tenant, optionally pinned to a
// single bucket. The tenant scoping happens via database.WithTenant so
// every downstream repo call hits the right tenant context.
func (w *LifecycleEvaluateWorker) evalTenant(ctx context.Context, log *slog.Logger, tenantID, bucketID uuid.UUID, now time.Time) error {
	tenantCtx := database.WithTenant(ctx, tenantID)
	rules, err := w.svc.repos.StorageLifecycleRules.ListEnabledForTenant(tenantCtx)
	if err != nil {
		return fmt.Errorf("list enabled rules: %w", err)
	}
	if len(rules) == 0 {
		return nil
	}
	// Group rules by bucket so each bucket gets one ListObjectVersions
	// pass per tick (the list call is the expensive one; the per-rule
	// evaluation is in-memory).
	byBucket := make(map[uuid.UUID][]database.StorageLifecycleRule)
	for _, r := range rules {
		if bucketID != uuid.Nil && r.BucketID != bucketID {
			continue
		}
		byBucket[r.BucketID] = append(byBucket[r.BucketID], r)
	}
	for bid, bucketRules := range byBucket {
		if err := w.evalBucket(tenantCtx, log, tenantID, bid, bucketRules, now); err != nil {
			log.WarnContext(ctx, "storage.lifecycle.evaluate: bucket tick failed",
				"tenant_id", tenantID, "bucket_id", bid, "error", err.Error())
		}
	}
	return nil
}

// evalBucket runs the rules for one bucket. The bucket row is fetched
// so the worker can resolve the canonical name + emit events with the
// right metadata.
func (w *LifecycleEvaluateWorker) evalBucket(ctx context.Context, log *slog.Logger, tenantID, bucketID uuid.UUID, rules []database.StorageLifecycleRule, now time.Time) error {
	bucket, err := w.svc.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		return fmt.Errorf("read bucket: %w", err)
	}
	for _, r := range rules {
		if err := w.evalRule(ctx, log, tenantID, bucket, r, now); err != nil {
			log.WarnContext(ctx, "storage.lifecycle.evaluate: rule failed",
				"bucket_id", bucketID, "rule_id", r.RuleID, "error", err.Error())
		}
	}
	return nil
}

// evalRule evaluates a single rule against the bucket's current object
// set + performs the action on every due object. The per-action logic
// is in the helpers below.
func (w *LifecycleEvaluateWorker) evalRule(ctx context.Context, log *slog.Logger, tenantID uuid.UUID, bucket database.StorageBucket, rule database.StorageLifecycleRule, now time.Time) error {
	prefix := ""
	if rule.Prefix != nil {
		prefix = *rule.Prefix
	}
	switch fromRowLifecycleAction(rule.Action) {
	case LifecycleActionExpiration:
		return w.evalExpiration(ctx, log, tenantID, bucket, rule, prefix, now)
	case LifecycleActionNoncurrentVersionExpiration:
		return w.evalNoncurrentExpiration(ctx, log, tenantID, bucket, rule, prefix, now)
	case LifecycleActionAbortIncompleteMultipart:
		// SeaweedFS' multipart upload IDs are not enumerable through the
		// S3 ListObjectVersions surface; the worker is a no-op for this
		// action today. Native SeaweedFS lifecycle support + a future
		// WS that lists in-progress uploads both close the gap.
		log.DebugContext(ctx, "storage.lifecycle.evaluate: abort_incomplete_multipart is a no-op in this build",
			"bucket_id", bucket.ID, "rule_id", rule.RuleID)
		return nil
	case LifecycleActionTransition:
		// SeaweedFS tier support varies by build; the worker is a no-op
		// for transition today. Native SeaweedFS lifecycle support closes
		// the gap when the destination tier is configured.
		log.DebugContext(ctx, "storage.lifecycle.evaluate: transition is a no-op in this build",
			"bucket_id", bucket.ID, "rule_id", rule.RuleID)
		return nil
	}
	return nil
}

// evalExpiration handles the LifecycleActionExpiration action: lists
// every object matching the rule prefix, evaluates the age trigger,
// and calls DeleteObject on every due object.
func (w *LifecycleEvaluateWorker) evalExpiration(ctx context.Context, log *slog.Logger, tenantID uuid.UUID, bucket database.StorageBucket, rule database.StorageLifecycleRule, prefix string, now time.Time) error {
	threshold, ok := ageThreshold(rule, now)
	if !ok {
		// Date-based trigger not yet due.
		return nil
	}
	page, err := w.evalProvider.ListObjectVersions(ctx, bucket.Name, prefix, "", "", 1000)
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	for _, v := range page.Versions {
		// Only the current version is expired by LifecycleActionExpiration.
		if !v.IsLatest {
			continue
		}
		if v.LastModified.IsZero() {
			continue
		}
		if now.Sub(v.LastModified) < threshold {
			continue
		}
		if err := w.evalProvider.DeleteObject(ctx, bucket.Name, v.Key, ""); err != nil && !errors.Is(err, seaweedfs.ErrNotFound) {
			log.WarnContext(ctx, "storage.lifecycle.evaluate: delete failed",
				"bucket_id", bucket.ID, "rule_id", rule.RuleID,
				"key", v.Key, "error", err.Error())
			continue
		}
		w.emitLifecycleObjectEvent(ctx, tenantID, bucket, rule, v.Key, "deleted")
		w.emitLifecycleAudit(ctx, tenantID, bucket, rule, v.Key, "deleted")
	}
	return nil
}

// evalNoncurrentExpiration handles the
// LifecycleActionNoncurrentVersionExpiration action: lists every
// noncurrent version older than the threshold and deletes it.
func (w *LifecycleEvaluateWorker) evalNoncurrentExpiration(ctx context.Context, log *slog.Logger, tenantID uuid.UUID, bucket database.StorageBucket, rule database.StorageLifecycleRule, prefix string, now time.Time) error {
	threshold, ok := ageThreshold(rule, now)
	if !ok {
		return nil
	}
	page, err := w.evalProvider.ListObjectVersions(ctx, bucket.Name, prefix, "", "", 1000)
	if err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	for _, v := range page.Versions {
		if v.IsLatest {
			continue
		}
		if v.LastModified.IsZero() {
			continue
		}
		if now.Sub(v.LastModified) < threshold {
			continue
		}
		if err := w.evalProvider.DeleteObject(ctx, bucket.Name, v.Key, v.VersionID); err != nil && !errors.Is(err, seaweedfs.ErrNotFound) {
			log.WarnContext(ctx, "storage.lifecycle.evaluate: delete version failed",
				"bucket_id", bucket.ID, "rule_id", rule.RuleID,
				"key", v.Key, "version_id", v.VersionID, "error", err.Error())
			continue
		}
		w.emitLifecycleObjectEvent(ctx, tenantID, bucket, rule, v.Key, "deleted")
		w.emitLifecycleAudit(ctx, tenantID, bucket, rule, v.Key, "deleted")
	}
	return nil
}

// ageThreshold extracts the age duration from a rule's days or date
// fields. Returns (d, true) when the rule fires; (0, false) when the
// rule is not yet due (date-based trigger in the future).
func ageThreshold(rule database.StorageLifecycleRule, now time.Time) (time.Duration, bool) {
	if rule.Days != nil && *rule.Days > 0 {
		return time.Duration(*rule.Days) * 24 * time.Hour, true
	}
	if rule.DateAt != nil {
		if now.Before(*rule.DateAt) {
			return 0, false
		}
		// Date-based rule fires when now >= DateAt; treat as "all objects
		// matching the prefix are due" — threshold of zero means every
		// object's LastModified is older than the threshold.
		return 0, true
	}
	return 0, false
}

// emitLifecycleObjectEvent emits the WASM event for a lifecycle-driven
// object deletion / transition. The shape matches the user-driven
// delete event so plugins subscribed to "s3.object.deleted" react
// identically.
func (w *LifecycleEvaluateWorker) emitLifecycleObjectEvent(ctx context.Context, tenantID uuid.UUID, bucket database.StorageBucket, rule database.StorageLifecycleRule, key, action string) {
	if w.svc.bus == nil {
		return
	}
	uid := uuid.Nil // system actor
	w.svc.emitEvent(ctx, eventbus.S3ObjectDeleted, tenantID, uid, bucket.ID, map[string]any{
		"bucket_slug": bucket.Slug,
		"key":         key,
		"action":      action,
		"trigger":     "lifecycle",
		"rule_id":     rule.RuleID,
	})
}

// emitLifecycleAudit writes an audit row for a lifecycle-driven object
// deletion. The actor is the system; the metadata.trigger = "lifecycle"
// so the audit query API can distinguish lifecycle-driven deletes from
// user-driven ones.
func (w *LifecycleEvaluateWorker) emitLifecycleAudit(ctx context.Context, tenantID uuid.UUID, bucket database.StorageBucket, rule database.StorageLifecycleRule, key, action string) {
	_, _ = w.svc.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorType:    audit.ActorSystem,
		Action:       AuditObjectLifecycleDel,
		ResourceType: ResourceBucket,
		ResourceID:   &bucket.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"bucket_slug": bucket.Slug,
			"key":         key,
			"action":      action,
			"trigger":     "lifecycle",
			"rule_id":     rule.RuleID,
		},
	})
}

// scanTenants enumerates every tenant that has at least one enabled
// lifecycle rule. The query is cross-tenant by design; the worker
// re-scopes per row via database.WithTenant before any DB write.
func (w *LifecycleEvaluateWorker) scanTenants(ctx context.Context) ([]uuid.UUID, error) {
	// The lifecycle rules table does not yet have a "distinct tenant_id"
	// query; the worker falls back to enumerating tenants via the
	// Tenants repo. This is acceptable because the rule scan is cheap;
	// a future optimisation can add a SELECT DISTINCT tenant_id query
	// when the rule count grows large.
	tenants, err := w.svc.repos.Tenants.List(ctx, 10_000, 0)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	out := make([]uuid.UUID, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, t.ID)
	}
	return out, nil
}

// RegisterLifecycleWorker wires the lifecycle evaluator worker into the
// jobs.Registry. Called from program.Start. The periodic schedule is
// added separately via the supervisor's PeriodicJobs hook.
func RegisterLifecycleWorker(reg *jobs.Registry, w *LifecycleEvaluateWorker) {
	if reg == nil || w == nil {
		return
	}
	jobs.Register(reg, LifecycleEvaluateArgs{}, w, jobs.KindSpec{
		Queue:       "storage",
		Concurrency: 1,
		Tags:        []string{"storage", "lifecycle", "ws-29"},
		Description: "Evaluate per-bucket S3 lifecycle rules + act on due objects (WS-29, ADR-0036).",
	})
}
