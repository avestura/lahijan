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

// TestRegisterExamples_AddsThreeKinds proves the helper covers the catalog
// the WS-09 doc names (minus the snapshot placeholder, which WS-25
// replaces with compute.snapshot.take / prune / backup.create in the
// compute package). The integration test builds a real client from the
// resulting registry; here we only assert the kind strings.
func TestRegisterExamples_AddsThreeKinds(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	RegisterExamples(r, nil)
	kinds := r.Kinds()
	require.Len(t, kinds, 3)
	assert.Equal(t, "auditlog.prune", kinds[0].Kind)
	assert.Equal(t, "billing.usage.rollup", kinds[1].Kind)
	assert.Equal(t, "notify.email.send", kinds[2].Kind)
}

// TestRegisterExamples_NilRegistryPanics documents the contract.
func TestRegisterExamples_NilRegistryPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { RegisterExamples(nil, nil) })
}
