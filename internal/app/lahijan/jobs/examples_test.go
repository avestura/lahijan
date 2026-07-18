// Package jobs: examples_test.go covers the four WS-09 example workers
// without touching Postgres. River's worker dispatch is exercised in
// jobs_integration_test.go; here we only prove each Work() returns nil
// (or a deterministic error for AlwaysFailWorker) under direct invocation.

package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPeriod is a fixed timestamp so the test is deterministic.
var testPeriod = time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)

// jobFrom builds a *river.Job[T] with the minimum fields Work() needs. The
// River row is constructed manually so we don't need a DB to round-trip it.
func jobFrom[T river.JobArgs](args T, attempt int) *river.Job[T] {
	return &river.Job[T]{
		JobRow: &rivertype.JobRow{
			ID:          1,
			Kind:        args.Kind(),
			Attempt:     attempt,
			State:       rivertype.JobStateRunning,
			MaxAttempts: 5,
		},
		Args: args,
	}
}

func TestMeteringRollupWorker_Work(t *testing.T) {
	t.Parallel()
	w := NewMeteringRollupWorker(nil)
	require.NoError(t, w.Work(context.Background(), jobFrom(MeteringRollupArgs{
		TenantID: "t-1", Period: testPeriod,
	}, 1)))
}

func TestAuditLogPruneWorker_Work(t *testing.T) {
	t.Parallel()
	w := NewAuditLogPruneWorker(nil)
	require.NoError(t, w.Work(context.Background(), jobFrom(AuditLogPruneArgs{
		TenantID: "t-1", RetentionDays: 90,
	}, 1)))
}

func TestNotifyEmailSendWorker_Work(t *testing.T) {
	t.Parallel()
	w := NewNotifyEmailSendWorker(nil)
	require.NoError(t, w.Work(context.Background(), jobFrom(NotifyEmailSendArgs{
		To: "u@example.test", Subject: "hi", BodyHTML: "<p>hi</p>",
	}, 1)))
}

func TestComputeInstanceSnapshotWorker_Work(t *testing.T) {
	t.Parallel()
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		w := NewComputeInstanceSnapshotWorker(nil)
		require.NoError(t, w.Work(context.Background(), jobFrom(ComputeInstanceSnapshotArgs{
			TenantID: "t-1", InstanceID: "i-1", SnapshotID: "s-1",
		}, 1)))
	})
	t.Run("missing_instance_id_fails", func(t *testing.T) {
		t.Parallel()
		w := NewComputeInstanceSnapshotWorker(nil)
		err := w.Work(context.Background(), jobFrom(ComputeInstanceSnapshotArgs{
			TenantID: "t-1", // InstanceID intentionally empty
		}, 1))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "instance_id is required")
	})
}

func TestAlwaysFailWorker_Work(t *testing.T) {
	t.Parallel()
	w := AlwaysFailWorker{}
	err := w.Work(context.Background(), jobFrom(AlwaysFailArgs{
		Reason: "boom",
	}, 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "always_fail")
	assert.True(t, errors.Is(err, err)) // sanity: err is itself
}

// TestRegisterExamples_AddsFourKinds proves the helper covers the catalog
// the WS-09 doc names. The integration test builds a real client from the
// resulting registry; here we only assert the kind strings.
func TestRegisterExamples_AddsFourKinds(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	RegisterExamples(r, nil)
	kinds := r.Kinds()
	require.Len(t, kinds, 4)
	assert.Equal(t, "auditlog.prune", kinds[0].Kind)
	assert.Equal(t, "billing.usage.rollup", kinds[1].Kind)
	assert.Equal(t, "compute.instance.snapshot", kinds[2].Kind)
	assert.Equal(t, "notify.email.send", kinds[3].Kind)
}

// TestRegisterExamples_NilRegistryPanics documents the contract.
func TestRegisterExamples_NilRegistryPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { RegisterExamples(nil, nil) })
}
