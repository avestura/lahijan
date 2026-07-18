// Package eventservice wires the in-process event bus to the plugin
// subscription table + the River-backed async dispatch path. It is the
// "consumer side" of the events host function (WS-10b): plugins subscribe
// via lahijan_events.subscribe (a row in plugin_event_subscriptions);
// this service listens to the bus and enqueues a wasm.plugin.invoke job
// per matched subscription.
//
// Lifecycle:
//
//   - Built once at bootstrap (program.Start).
//   - Start subscribes a single sync listener on the bus that runs the
//     dispatch pipeline. The pipeline is "fast enough for sync" because
//     it only reads the subscription table + enqueues River jobs; the
//     actual plugin invocation happens asynchronously on the queue.
//   - Stop detaches the listener. Idempotent.
//
// Durability (WS-10b DoD item "async event subscription survives plugin
// restart"):
//   - Subscriptions persist in plugin_event_subscriptions.
//   - Emit does not need the plugin to be loaded; the dispatched River
//     job waits until a worker process is up.
//   - Plugin disable removes its mounts + subscriptions (CASCADE on the
//     plugins table); events emitted between disable and uninstall are
//     still matched against the subscription until CASCADE runs.
package eventservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/hostfuncs"
)

// JobInserter is the seam the service uses to enqueue async plugin
// dispatches. *jobs.Client satisfies it; tests inject a fake.
type JobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// SubscriptionLister is the narrow slice of *database.PluginEventSubscriptionsRepository
// the service actually uses. Defined as an interface so tests can
// inject an in-memory fake without spinning up Postgres.
type SubscriptionLister interface {
	ListAll(ctx context.Context) ([]gen.PluginEventSubscription, error)
}

// Service is the consumer side of the events host function. Construct
// one at bootstrap and call Start to attach it to the bus.
type Service struct {
	bus      *eventbus.Bus
	subs     SubscriptionLister
	inserter JobInserter
	log      *slog.Logger

	mu         sync.Mutex
	listenerID uuid.UUID
	started    bool
}

// New builds a Service. inserter may be nil; in that case dispatches
// are silently dropped (plugins can still emit + subscribe; their
// subscriptions just do not fire until a process with the deps wired
// runs).
func New(bus *eventbus.Bus, subs SubscriptionLister, inserter JobInserter, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{bus: bus, subs: subs, inserter: inserter, log: log}
}

// Start attaches the bus listener. Idempotent.
func (s *Service) Start(ctx context.Context) error {
	if s.bus == nil {
		return errors.New("eventservice: bus is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	s.listenerID = s.bus.Register("*", s.dispatch)
	s.started = true
	s.log.Debug("eventservice: started")
	return nil
}

// Stop detaches the bus listener. Idempotent.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.bus.Unregister(s.listenerID)
	s.started = false
}

// dispatch is the bus listener the service registers. It runs the
// dispatch pipeline:
//
//  1. List every subscription via the subscription repo (single query).
//  2. For each subscription whose topic pattern matches the event,
//     enqueue a wasm.plugin.invoke job carrying the plugin id + the
//     configured handler export + the event payload.
//
// Failures inside dispatch are logged but do NOT block the bus; the
// event still happened, and a failing downstream listener is the
// listener's problem (per the bus contract).
func (s *Service) dispatch(ctx context.Context, e eventbus.Event) error {
	if s.subs == nil {
		return nil
	}
	subs, err := s.subs.ListAll(ctx)
	if err != nil {
		s.log.Warn("eventservice: list subscriptions", "error", err)
		return nil
	}
	for _, sub := range subs {
		if !eventbus.MatchSubscription(sub.TopicPattern, e.Topic) {
			continue
		}
		if err := s.enqueue(ctx, sub.PluginID, sub.Handler, e.Metadata); err != nil {
			s.log.Warn("eventservice: enqueue dispatch",
				"plugin", sub.PluginID, "topic", e.Topic, "error", err)
		}
	}
	return nil
}

// enqueue inserts a River wasm.plugin.invoke job. When the inserter is
// nil (jobs subsystem disabled) the dispatch is silently dropped; the
// operator sees this via the missing-jobs metric.
func (s *Service) enqueue(ctx context.Context, pluginID uuid.UUID, handler string, payload []byte) error {
	if s.inserter == nil {
		return nil
	}
	if _, err := s.inserter.Insert(ctx, hostfuncs.PluginInvokeArgs{
		PluginID: pluginID.String(),
		Export:   handler,
		Args:     payload,
	}, nil); err != nil {
		return fmt.Errorf("eventservice: insert wasm.plugin.invoke: %w", err)
	}
	return nil
}
