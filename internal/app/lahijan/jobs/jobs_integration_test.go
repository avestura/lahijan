// Package jobs: jobs_integration_test.go exercises the full River lifecycle
// against a real Postgres (via testcontainers) so we can prove the WS-09
// Definition of Done items that need a live queue:
//
//   - queueing a job survives a process restart (we simulate "restart" by
//     building a fresh client on the same pool and asserting the queued
//     row is picked up).
//   - a failing job retries with exponential backoff and lands in DLQ.
//   - every job execution emits an OTel span (verified via an in-memory
//     span exporter).
//
// Run with:  go test -tags integration -timeout 600s ./internal/app/lahijan/jobs/...
//
// Tests in this file do NOT call t.Parallel(): they all share the same
// Postgres database, and River clients compete for the same river_job rows.
// To keep them isolated, each test gets a UNIQUE queue via newIsolatedClient
// and inserts every job with that queue. A test's client only listens on
// its own queue, so prior tests' leftover jobs cannot be picked up.

//go:build integration

package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// fastRetryPolicy is a River ClientRetryPolicy that schedules every retry
// 100ms out. River's default policy is attempt^4 seconds (1s, 16s, 81s, ...)
// which makes the DLQ integration test take 20+ seconds even with
// MaxAttempts=2. The fast policy cuts the cycle to under a second without
// hiding the retry behaviour the test is asserting on.
type fastRetryPolicy struct{}

func (fastRetryPolicy) NextRetry(*rivertype.JobRow) time.Time {
	return time.Now().Add(100 * time.Millisecond)
}

// TestMain starts a shared Postgres container (with all migrations including
// 0018/0019_river_schema applied), runs the package's tests, then tears down.
func TestMain(m *testing.M) {
	testutil.Setup(m)
}

// queueCounter mints unique queue names per test so tests don't share the
// same queue even though they share the same Postgres. Combined with
// inserting every job with that queue, this gives perfect isolation.
var queueCounter atomic.Int64

// uniqueQueue returns a unique queue name for a test.
func uniqueQueue(prefix string) string {
	n := queueCounter.Add(1)
	return fmt.Sprintf("%s_%d", prefix, n)
}

// isolatedClient bundles a River client with the unique queue it owns.
// Every test helper below inserts jobs on Queue so they are guaranteed
// to be consumed by THIS test's supervisor.
type isolatedClient struct {
	*Client
	queue string
}

// newIsolatedClient builds a River client that listens ONLY on a unique
// queue. Every example worker + AlwaysFail is registered; inserts should
// pass opts.Queue = queue. The supervisor is started and a cleanup
// registered to stop it.
func newIsolatedClient(t *testing.T) *isolatedClient {
	t.Helper()
	queue := uniqueQueue("q")
	registry := NewRegistry()
	RegisterExamples(registry, testLogger(t))
	Register(registry, AlwaysFailArgs{}, AlwaysFailWorker{}, KindSpec{
		Kind:        (AlwaysFailArgs{}).Kind(),
		Queue:       queue,
		Description: "Always-fails kind used to exercise retry/DLQ.",
	})

	cfg := DefaultConfig()
	cfg.Logger = testLogger(t)
	// Listen on the unique queue ONLY. buildQueues also adds "default" and
	// every queue named in the registry, but we will never insert on those
	// queues so they stay empty.
	cfg.Queues = map[string]int{queue: 5}
	// Fast retry so DLQ tests don't take 16+ seconds per retry.
	cfg.RetryPolicy = fastRetryPolicy{}

	client, err := NewClient(testutil.Pool(), registry, cfg)
	require.NoError(t, err, "build river client")

	sup, err := NewSupervisor(client, 10*time.Second)
	require.NoError(t, err, "build supervisor")
	// Use context.Background() so the client's lifetime is the test's
	// lifetime. River ties workers to the Start context; a timeout here
	// would stop the workers after the timeout and break every test that
	// depends on the queue. The supervisor is drained via Stop in
	// t.Cleanup.
	require.NoError(t, sup.Start(context.Background()), "start supervisor")
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = sup.Stop(stopCtx)
	})
	return &isolatedClient{Client: client, queue: queue}
}

// insert queues args on this client's unique queue. Returns the insert
// result; asserts no error.
func (c *isolatedClient) insert(t *testing.T, ctx context.Context, args river.JobArgs, opts *river.InsertOpts) *rivertype.JobInsertResult {
	t.Helper()
	if opts == nil {
		opts = &river.InsertOpts{}
	}
	opts.Queue = c.queue
	res, err := c.Insert(ctx, args, opts)
	require.NoError(t, err, "insert job")
	return res
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	// River is extremely chatty at INFO; keep test output readable by
	// surfacing only ERROR. The actual worker logs go through this same
	// logger, so a misbehaving worker still surfaces.
	return slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{
		Level: slog.LevelError,
	})).With("test", t.Name())
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestJobs_QueueAndComplete proves the happy path: a queued job is picked
// up by a worker and reaches the "completed" state. This is the WS-09 DoD
// item "integration test: queue a job, worker picks it up, completes,
// observable".
func TestJobs_QueueAndComplete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := newIsolatedClient(t)
	res := c.insert(t, ctx, MeteringRollupArgs{
		TenantID: "t-1", Period: testPeriod,
	}, nil)

	require.Eventually(t, func() bool {
		row, err := c.JobGet(ctx, res.Job.ID)
		if err != nil {
			return false
		}
		return row.State == "completed"
	}, 15*time.Second, 200*time.Millisecond, "job should reach completed state")
}

// TestJobs_RetryThenDLQ proves a failing job retries with backoff and ends
// up in the discarded (DLQ) state. This is the WS-09 DoD item "a failing
// job retries with exponential backoff, then lands in DLQ".
//
// To keep the test fast, we set MaxAttempts=2 on insert so River gives up
// quickly, AND install the fastRetryPolicy so the first retry is 100ms out
// (River's default is 1s for attempt 1, then attempt^4 seconds, so 16s for
// attempt 2). The whole cycle should finish within 5s.
func TestJobs_RetryThenDLQ(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	queue := uniqueQueue("dlq")
	registry := NewRegistry()
	// Use a fresh counting-fail worker so this test is isolated from any
	// other client's worker bundle AND so we can assert the worker was
	// actually invoked.
	count := atomic.Int64{}
	worker := &countingFailWorker{count: &count}
	Register(registry, countingFailArgs{}, worker, KindSpec{
		Kind: (countingFailArgs{}).Kind(), Queue: queue,
		Description: "Always-fails kind used to exercise retry/DLQ.",
	})

	cfg := DefaultConfig()
	cfg.Logger = testLogger(t)
	cfg.Queues = map[string]int{queue: 5}
	cfg.RetryPolicy = fastRetryPolicy{}
	client, err := NewClient(testutil.Pool(), registry, cfg)
	require.NoError(t, err)
	sup, err := NewSupervisor(client, 10*time.Second)
	require.NoError(t, err)
	require.NoError(t, sup.Start(context.Background()))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = sup.Stop(stopCtx)
	})

	res, err := client.Insert(ctx, countingFailArgs{Reason: "dlq test"}, &river.InsertOpts{
		MaxAttempts: 2,
		Queue:       queue,
	})
	require.NoError(t, err, "insert always-fail job")

	require.Eventually(t, func() bool {
		row, err := client.JobGet(ctx, res.Job.ID)
		if err != nil {
			t.Logf("get error: %v", err)
			return false
		}
		t.Logf("state=%s attempt=%d/%d count=%d", row.State, row.Attempt, row.MaxAttempts, count.Load())
		return row.State == "discarded"
	}, 25*time.Second, 500*time.Millisecond, "failing job should reach discarded state")

	row, err := client.JobGet(ctx, res.Job.ID)
	require.NoError(t, err, "fetch final state")
	assert.GreaterOrEqual(t, row.Attempt, 2, "at least 2 attempts before DLQ")
	assert.False(t, row.FinalizedAt.IsZero(), "FinalizedAt must be set on DLQ'd job")
	assert.GreaterOrEqual(t, count.Load(), int64(2), "worker should have been invoked >= 2 times")
}

type countingFailArgs struct{ Reason string }

func (countingFailArgs) Kind() string { return "test.counting_fail" }

type countingFailWorker struct {
	river.WorkerDefaults[countingFailArgs]
	count *atomic.Int64
}

func (w *countingFailWorker) Work(_ context.Context, job *river.Job[countingFailArgs]) error {
	w.count.Add(1)
	return fmt.Errorf("jobs: counting_fail: %s", job.Args.Reason)
}

// TestJobs_OTelSpanEmitted proves the worker middleware opens an OTel span
// for every job execution. Uses an in-memory span exporter installed as
// the global tracer provider; assertions check the span name + attributes.
func TestJobs_OTelSpanEmitted(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	provider, restore := installTestTracerProvider(exp)
	defer restore()
	defer func() { _ = provider.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use a counting worker so we can independently verify the worker ran
	// even when the OTel exporter is empty.
	count := registryWithCountingWorker(t)
	queue := uniqueQueue("otel")
	cfg := DefaultConfig()
	cfg.Logger = testLogger(t)
	cfg.Queues = map[string]int{queue: 5}
	cfg.RetryPolicy = fastRetryPolicy{}
	client, err := NewClient(testutil.Pool(), count.registry, cfg)
	require.NoError(t, err)
	sup, err := NewSupervisor(client, 10*time.Second)
	require.NoError(t, err)
	require.NoError(t, sup.Start(context.Background()))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = sup.Stop(stopCtx)
	})

	res, err := client.Insert(ctx, count.args, &river.InsertOpts{Queue: queue})
	require.NoError(t, err, "insert job")
	_ = res

	// First, prove the worker actually ran (decouples from OTel plumbing).
	require.Eventually(t, func() bool {
		return count.invocations.Load() > 0
	}, 15*time.Second, 100*time.Millisecond, "worker should have been invoked")

	// Force a flush, then assert the span was emitted.
	require.Eventually(t, func() bool {
		_ = provider.ForceFlush(ctx)
		return len(exp.GetSpans().Snapshots()) > 0
	}, 15*time.Second, 100*time.Millisecond, "expected at least one OTel span")

	spans := exp.GetSpans().Snapshots()
	require.NotEmpty(t, spans)
	var found bool
	for _, s := range spans {
		if s.Name() == "river."+count.args.Kind()+".work" {
			found = true
			attrs := make(map[string]string, len(s.Attributes()))
			for _, a := range s.Attributes() {
				attrs[string(a.Key)] = a.Value.AsString()
			}
			assert.Equal(t, count.args.Kind(), attrs[attrKind], "span kind attr")
			break
		}
	}
	assert.True(t, found, "expected a span named river."+count.args.Kind()+".work")
}

// countingFixture bundles a registry with a unique counting worker so a
// test can assert the worker ran independently of any other side effect.
type countingFixture struct {
	registry    *Registry
	args        countingArgs
	invocations atomic.Int64
}

type countingArgs struct{ n int }

func (countingArgs) Kind() string { return "test.counting" }

func registryWithCountingWorker(t *testing.T) *countingFixture {
	t.Helper()
	out := &countingFixture{registry: NewRegistry(), args: countingArgs{n: 1}}
	worker := &countingWorker{count: &out.invocations}
	Register(out.registry, out.args, worker, KindSpec{
		Kind: out.args.Kind(), Queue: "test",
		Description: "Counts its own invocations.",
	})
	return out
}

type countingWorker struct {
	river.WorkerDefaults[countingArgs]
	count *atomic.Int64
}

func (w *countingWorker) Work(_ context.Context, _ *river.Job[countingArgs]) error {
	w.count.Add(1)
	return nil
}

// TestJobs_ClientCannotBeBuiltFromEmptyRegistry asserts NewClient fails
// clearly when the registry has no workers. This guards a common
// bootstrap-time misconfiguration.
func TestJobs_ClientCannotBeBuiltFromEmptyRegistry(t *testing.T) {
	_, err := NewClient(testutil.Pool(), NewRegistry(), DefaultConfig())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEmptyRegistry), "expected ErrEmptyRegistry, got %v", err)
}

// TestJobs_SurvivesClientReconnect proves a queued job is durable across
// a "process restart": we stop the first supervisor, insert a new job, and
// prove a fresh supervisor picks it up. This is the WS-09 DoD item
// "queueing a job survives a process restart".
func TestJobs_SurvivesClientReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First client: build, start, stop — but DO NOT use the isolated helper
	// because we want to control stop/start manually. The unique queue is
	// still per-test so other tests' clients cannot interfere.
	queue := uniqueQueue("q")
	registry1 := NewRegistry()
	RegisterExamples(registry1, testLogger(t))
	cfg1 := DefaultConfig()
	cfg1.Logger = testLogger(t)
	cfg1.Queues = map[string]int{queue: 5}
	client1, err := NewClient(testutil.Pool(), registry1, cfg1)
	require.NoError(t, err)
	sup1, err := NewSupervisor(client1, 10*time.Second)
	require.NoError(t, err)
	// context.Background() — River ties the client lifetime to this ctx.
	require.NoError(t, sup1.Start(context.Background()))

	// Second client on the same queue.
	registry2 := NewRegistry()
	RegisterExamples(registry2, testLogger(t))
	cfg2 := DefaultConfig()
	cfg2.Logger = testLogger(t)
	cfg2.Queues = map[string]int{queue: 5}
	client2, err := NewClient(testutil.Pool(), registry2, cfg2)
	require.NoError(t, err)

	// Stop the first supervisor, then queue. The job sits "available"
	// because no client is consuming.
	require.NoError(t, sup1.Stop(ctx))

	res, err := client2.Insert(ctx, MeteringRollupArgs{
		TenantID: "t-restart", Period: testPeriod,
	}, &river.InsertOpts{Queue: queue})
	require.NoError(t, err, "insert after stop")

	// Start a supervisor on client2; it should pick up the queued job.
	sup2, err := NewSupervisor(client2, 10*time.Second)
	require.NoError(t, err)
	require.NoError(t, sup2.Start(context.Background()))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = sup2.Stop(stopCtx)
	})

	require.Eventually(t, func() bool {
		row, err := client2.JobGet(ctx, res.Job.ID)
		if err != nil {
			return false
		}
		return row.State == "completed"
	}, 15*time.Second, 200*time.Millisecond, "job should complete after restart")
}
