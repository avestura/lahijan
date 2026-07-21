// Package storage_test: ws29_integration_test.go covers the WS-29
// service-layer surface (versioning / lifecycle / object-lock +
// lifecycle evaluator worker) against the testcontainers Postgres
// harness + the in-memory fakeSW. Mirrors the WS-16
// service_integration_test.go pattern.
//
// Run with:  go test -tags integration ./internal/app/lahijan/storage/...

//go:build integration

package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// createBucketForWS29 is a shared helper that creates a bucket + returns
// it. Most WS-29 tests need a bucket to operate on; the helper keeps
// the per-test setup terse.
func createBucketForWS29(t *testing.T, f *fixture, slug string) database.StorageBucket {
	t.Helper()
	b, err := f.svc.CreateBucket(f.tenantCtx, f.tenantID, f.userID, storage.BucketCreateParams{Slug: slug})
	require.NoError(t, err)
	return b
}

// ---------------------------------------------------------------------------
// Versioning
// ---------------------------------------------------------------------------

func TestVersioning_SetGet(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "ver")

	// Initially unversioned.
	got, err := f.svc.GetBucketVersioning(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.VersioningStatusUnversioned, got)

	// Enable.
	require.NoError(t, f.svc.SetBucketVersioning(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.VersioningStatusEnabled))
	got, err = f.svc.GetBucketVersioning(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.VersioningStatusEnabled, got)

	// The provider saw the push.
	f.sw.mu.Lock()
	last := f.sw.versioningCalls[len(f.sw.versioningCalls)-1]
	f.sw.mu.Unlock()
	assert.Equal(t, bucket.Name, last.Bucket)
	assert.Equal(t, seaweedfs.VersioningStatusEnabled, last.Status)

	// Audit + bus emissions.
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketVersioningSet),
		"SetBucketVersioning must emit an audit row")
	assert.Contains(t, f.bus.topics(), eventbus.S3BucketVersioningSet)
}

func TestVersioning_Set_NoOpWhenSame(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "ver-noop")

	require.NoError(t, f.svc.SetBucketVersioning(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.VersioningStatusEnabled))
	f.sw.mu.Lock()
	callsAfterFirst := len(f.sw.versioningCalls)
	f.sw.mu.Unlock()

	// Set the same status again — should be a no-op (no provider call,
	// no audit row beyond the first).
	require.NoError(t, f.svc.SetBucketVersioning(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.VersioningStatusEnabled))
	f.sw.mu.Lock()
	callsAfterSecond := len(f.sw.versioningCalls)
	f.sw.mu.Unlock()
	assert.Equal(t, callsAfterFirst, callsAfterSecond, "second SetBucketVersioning with same status must be a no-op")
}

func TestVersioning_Set_BucketNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	err := f.svc.SetBucketVersioning(f.tenantCtx, f.tenantID, f.userID, uuid.New(), storage.VersioningStatusEnabled)
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrBucketNotFound)
}

// ---------------------------------------------------------------------------
// Object lock
// ---------------------------------------------------------------------------

func TestObjectLock_SetGet(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "ol")

	// Initially disabled.
	got, err := f.svc.GetObjectLock(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	// Enable.
	cfg := storage.ObjectLockConfig{
		Enabled: true,
		Mode:    storage.ObjectLockModeGovernance,
		Days:    30,
	}
	require.NoError(t, f.svc.SetObjectLock(f.tenantCtx, f.tenantID, f.userID, bucket.ID, cfg))
	got, err = f.svc.GetObjectLock(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.True(t, got.Enabled)
	assert.Equal(t, storage.ObjectLockModeGovernance, got.Mode)
	assert.EqualValues(t, 30, got.Days)

	// Provider saw the push.
	f.sw.mu.Lock()
	last := f.sw.objectLockCalls[len(f.sw.objectLockCalls)-1]
	f.sw.mu.Unlock()
	assert.Equal(t, bucket.Name, last.Bucket)
	assert.True(t, last.Cfg.Enabled)

	// Audit + bus emissions.
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketObjectLockSet))
	assert.Contains(t, f.bus.topics(), eventbus.S3BucketObjectLockSet)
}

func TestObjectLock_Set_InvalidMode(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "ol-bad")

	err := f.svc.SetObjectLock(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.ObjectLockConfig{
		Enabled: true,
		Mode:    storage.ObjectLockMode("BOGUS"),
		Days:    30,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrInvalidObjectLockMode)
}

func TestObjectLock_Set_DisableSkipsProviderPush(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "ol-disable")

	// First enable, then disable.
	require.NoError(t, f.svc.SetObjectLock(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.ObjectLockConfig{
		Enabled: true, Mode: storage.ObjectLockModeCompliance, Days: 365,
	}))
	f.sw.mu.Lock()
	callsAfterEnable := len(f.sw.objectLockCalls)
	f.sw.mu.Unlock()

	// Disable: cache update only; provider is NOT pushed.
	require.NoError(t, f.svc.SetObjectLock(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.ObjectLockConfig{Enabled: false}))
	f.sw.mu.Lock()
	callsAfterDisable := len(f.sw.objectLockCalls)
	f.sw.mu.Unlock()
	assert.Equal(t, callsAfterEnable, callsAfterDisable, "disable must skip the provider push")
}

// ---------------------------------------------------------------------------
// Lifecycle rules
// ---------------------------------------------------------------------------

func TestLifecycle_CreateGet(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc")

	days := int32(30)
	rule, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID:      "expire-logs",
		Enabled: true,
		Action:  storage.LifecycleActionExpiration,
		Days:    &days,
		Prefix:  strPtrLocal("logs/"),
	})
	require.NoError(t, err)
	assert.Equal(t, "expire-logs", rule.RuleID)
	assert.Equal(t, "expiration", rule.Action)
	assert.Equal(t, "enabled", rule.Status)

	got, err := f.svc.GetLifecycleRule(f.tenantCtx, f.tenantID, bucket.ID, "expire-logs")
	require.NoError(t, err)
	assert.Equal(t, rule.ID, got.ID)

	// Audit + bus emissions.
	require.NotEmpty(t, f.auditEm.eventsFor(storage.AuditBucketLifecycleSet))
	assert.Contains(t, f.bus.topics(), eventbus.S3BucketLifecycleSet)

	// The provider saw the reconcile push.
	f.sw.mu.Lock()
	lastLifecycleCall := f.sw.lifecycleCalls[len(f.sw.lifecycleCalls)-1]
	f.sw.mu.Unlock()
	assert.Equal(t, bucket.Name, lastLifecycleCall.Bucket)
	require.Len(t, lastLifecycleCall.Rules, 1)
	assert.Equal(t, "expire-logs", lastLifecycleCall.Rules[0].ID)
}

func TestLifecycle_Create_InvalidTrigger(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-bad")

	// Neither days nor date set.
	_, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID:      "x",
		Enabled: true,
		Action:  storage.LifecycleActionExpiration,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrInvalidLifecycleTrigger)

	// Both days + date set.
	days := int32(7)
	date := time.Now()
	_, err = f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID:      "x",
		Enabled: true,
		Action:  storage.LifecycleActionExpiration,
		Days:    &days,
		Date:    &date,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrInvalidLifecycleTrigger)
}

func TestLifecycle_Create_TransitionRequiresStorageClass(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-t")

	days := int32(30)
	_, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID:      "x",
		Enabled: true,
		Action:  storage.LifecycleActionTransition,
		Days:    &days,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrInvalidLifecycleStorageClass)
}

func TestLifecycle_List(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-list")

	days := int32(7)
	for _, id := range []string{"rule-a", "rule-b", "rule-c"} {
		_, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
			ID: id, Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
		})
		require.NoError(t, err)
	}
	rules, err := f.svc.ListLifecycleRules(f.tenantCtx, f.tenantID, bucket.ID, 50, 0)
	require.NoError(t, err)
	require.Len(t, rules, 3)
	// Ordered by rule_id ASC.
	assert.Equal(t, "rule-a", rules[0].RuleID)
	assert.Equal(t, "rule-b", rules[1].RuleID)
	assert.Equal(t, "rule-c", rules[2].RuleID)

	count, err := f.svc.CountLifecycleRules(f.tenantCtx, f.tenantID, bucket.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 3, count)
}

func TestLifecycle_Update(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-up")

	days := int32(7)
	rule, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID: "r", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
	})
	require.NoError(t, err)

	// Update: bump days to 14, disable.
	newDays := int32(14)
	require.NoError(t, f.svc.UpdateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, rule.ID, storage.LifecycleRuleInput{
		Enabled: false, Days: &newDays,
	}))
	got, err := f.svc.GetLifecycleRule(f.tenantCtx, f.tenantID, bucket.ID, "r")
	require.NoError(t, err)
	assert.EqualValues(t, 14, *got.Days)
	assert.Equal(t, "disabled", got.Status)
}

func TestLifecycle_Delete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-del")

	days := int32(7)
	rule, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID: "r", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
	})
	require.NoError(t, err)

	require.NoError(t, f.svc.DeleteLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, rule.ID))
	_, err = f.svc.GetLifecycleRule(f.tenantCtx, f.tenantID, bucket.ID, "r")
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrLifecycleRuleNotFound)
}

func TestLifecycle_ReplaceAll(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-rep")

	days := int32(7)
	for _, id := range []string{"a", "b"} {
		_, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
			ID: id, Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
		})
		require.NoError(t, err)
	}

	// Replace with a fresh set of 3 rules.
	days30 := int32(30)
	require.NoError(t, f.svc.ReplaceLifecycleRules(f.tenantCtx, f.tenantID, f.userID, bucket.ID, []storage.LifecycleRuleInput{
		{ID: "x", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days30},
		{ID: "y", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days30},
		{ID: "z", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days30},
	}))
	rules, err := f.svc.ListLifecycleRules(f.tenantCtx, f.tenantID, bucket.ID, 50, 0)
	require.NoError(t, err)
	require.Len(t, rules, 3)
	assert.Equal(t, "x", rules[0].RuleID)
}

// ---------------------------------------------------------------------------
// Lifecycle evaluator worker
// ---------------------------------------------------------------------------

func TestLifecycleWorker_ExpiresOldObjects(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-work")
	days := int32(1)
	_, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID: "expire", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
	})
	require.NoError(t, err)

	// Set up the fake provider to return a single object that is well
	// past the 1-day threshold.
	oldTime := time.Now().Add(-48 * time.Hour)
	f.sw.mu.Lock()
	f.sw.listVersionsResult = &seaweedfs.ObjectVersionsPage{
		Versions: []seaweedfs.ObjectVersion{
			{Key: "old.log", VersionID: "v1", IsLatest: true, LastModified: oldTime},
		},
	}
	f.sw.mu.Unlock()

	w := storage.NewLifecycleEvaluateWorker(f.svc, f.sw, nil)
	require.NoError(t, w.Work(f.tenantCtx, newLifecycleJob(f.tenantID, bucket.ID)))

	// The provider saw a DeleteObject for "old.log".
	f.sw.mu.Lock()
	deletes := append([]deleteObjectCall(nil), f.sw.deleteObjectCalls...)
	f.sw.mu.Unlock()
	require.Len(t, deletes, 1, "expected one DeleteObject for the expired key")
	assert.Equal(t, "old.log", deletes[0].Key)
}

func TestLifecycleWorker_SkipsRecentObjects(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-work-skip")
	days := int32(30)
	_, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID: "expire", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
	})
	require.NoError(t, err)

	// Recent object — should NOT be expired.
	f.sw.mu.Lock()
	f.sw.listVersionsResult = &seaweedfs.ObjectVersionsPage{
		Versions: []seaweedfs.ObjectVersion{
			{Key: "fresh.log", VersionID: "v1", IsLatest: true, LastModified: time.Now()},
		},
	}
	f.sw.mu.Unlock()

	w := storage.NewLifecycleEvaluateWorker(f.svc, f.sw, nil)
	require.NoError(t, w.Work(f.tenantCtx, newLifecycleJob(f.tenantID, bucket.ID)))

	f.sw.mu.Lock()
	deletes := len(f.sw.deleteObjectCalls)
	f.sw.mu.Unlock()
	assert.Zero(t, deletes, "fresh object must not be expired")
}

// ---------------------------------------------------------------------------
// Tenant isolation
// ---------------------------------------------------------------------------

func TestWS29_TenantIsolation_LifecycleRule(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	bucket := createBucketForWS29(t, f, "lc-iso")

	days := int32(7)
	rule, err := f.svc.CreateLifecycleRule(f.tenantCtx, f.tenantID, f.userID, bucket.ID, storage.LifecycleRuleInput{
		ID: "r", Enabled: true, Action: storage.LifecycleActionExpiration, Days: &days,
	})
	require.NoError(t, err)

	// A different tenant's context cannot see the rule.
	otherTenantRow := testutil.NewTenant(context.Background(), t, testutil.Pool())
	otherCtx := database.WithTenant(context.Background(), otherTenantRow.ID)
	_, err = f.svc.GetLifecycleRule(otherCtx, otherTenantRow.ID, bucket.ID, "r")
	require.Error(t, err, "cross-tenant lookup must fail")
	assert.ErrorIs(t, err, storage.ErrLifecycleRuleNotFound)

	// Cross-tenant rule access via ID is also blocked.
	_, err = f.svc.repos.StorageLifecycleRules.GetByID(otherCtx, rule.ID)
	require.Error(t, err, "repo-level cross-tenant lookup must fail")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// strPtrLocal returns &s. Local helper to avoid pulling the api
// package's ptrString into the storage_test package.
func strPtrLocal(s string) *string { return &s }

// newLifecycleJob builds a river.Job with the given args. Used by the
// lifecycle worker tests; the worker only reads job.Args.
func newLifecycleJob(tenantID, bucketID uuid.UUID) *river.Job[storage.LifecycleEvaluateArgs] {
	return &river.Job[storage.LifecycleEvaluateArgs]{
		Args: storage.LifecycleEvaluateArgs{
			TenantID: tenantID.String(),
			BucketID: bucketID.String(),
		},
	}
}
