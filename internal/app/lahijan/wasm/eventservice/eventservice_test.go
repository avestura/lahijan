// eventservice_test.go covers the dispatch pipeline against an
// in-memory subscription list + a fake JobInserter. No DB / wazero / River.

package eventservice

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/hostfuncs"
)

func TestService_DispatchMatchesAndEnqueues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pid := uuid.New()
	subs := &fakeSubsRepo{
		subs: []gen.PluginEventSubscription{
			{PluginID: pid, TopicPattern: "dns.record.*", Handler: "on_record"},
		},
	}
	fake := &fakeInserter{calls: make(chan hostfuncs.PluginInvokeArgs, 8)}
	bus := eventbus.New(eventbus.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	svc := New(bus, subs, fake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, svc.Start(ctx))
	defer svc.Stop()

	require.NoError(t, bus.Emit(ctx, eventbus.Event{
		Topic:    "dns.record.created",
		Metadata: []byte(`{"id":"x"}`),
	}))

	select {
	case args := <-fake.calls:
		assert.Equal(t, pid.String(), args.PluginID)
		assert.Equal(t, "on_record", args.Export)
	case <-time.After(time.Second):
		t.Fatal("dispatch did not enqueue a job")
	}
}

func TestService_DispatchIgnoresNonMatchingTopics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pid := uuid.New()
	subs := &fakeSubsRepo{
		subs: []gen.PluginEventSubscription{
			{PluginID: pid, TopicPattern: "dns.record.*", Handler: "on_record"},
		},
	}
	fake := &fakeInserter{calls: make(chan hostfuncs.PluginInvokeArgs, 8)}
	bus := eventbus.New(eventbus.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	svc := New(bus, subs, fake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, svc.Start(ctx))
	defer svc.Stop()

	require.NoError(t, bus.Emit(ctx, eventbus.Event{Topic: "compute.instance.stopped"}))
	select {
	case args := <-fake.calls:
		t.Fatalf("unexpected dispatch: %+v", args)
	case <-time.After(50 * time.Millisecond):
	}
}

// fakeSubsRepo is an in-memory PluginEventSubscriptionsRepository.
// Only the ListAll method is exercised by the tests.
type fakeSubsRepo struct {
	subs []gen.PluginEventSubscription
	mu   sync.Mutex
}

func (f *fakeSubsRepo) ListAll(_ context.Context) ([]gen.PluginEventSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]gen.PluginEventSubscription, len(f.subs))
	copy(out, f.subs)
	return out, nil
}

// fakeInserter satisfies JobInserter; pushes every insert onto a
// channel so tests can assert dispatch occurred.
type fakeInserter struct {
	calls chan hostfuncs.PluginInvokeArgs
}

func (f *fakeInserter) Insert(_ context.Context, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	pia, ok := args.(hostfuncs.PluginInvokeArgs)
	if !ok {
		return nil, nil
	}
	select {
	case f.calls <- pia:
	default:
	}
	return &rivertype.JobInsertResult{}, nil
}
