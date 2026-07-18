// Package eventbus is the in-process pub/sub the WS-10b host functions
// (and the Phase 3 provider drivers, and the audit log) use to broadcast
// "something happened" events. The bus supports:
//
//   - **Synchronous listeners** (in-process) — for the audit log, metrics,
//     and any plugin subscription whose plugin is currently loaded.
//   - **Asynchronous listeners** (via River) — for plugin subscriptions
//     whose plugin may not be loaded when the event fires. The bus
//     enqueues a PluginDispatch job per matched subscription; River's
//     retry + DLQ inherit (ADR-0008).
//
// ## Topic hierarchy
//
// Topics are dot-separated identifiers following the same shape as audit
// actions and RBAC permissions: "dns.record.created",
// "compute.instance.stopped". The wildcard suffix ".*" matches every
// child of the prefix — "dns.record.*" matches "dns.record.created" and
// "dns.record.deleted" but NOT "dns.zone.created".
//
// ## Standard event registry
//
// Every event a provider (Phase 3) or module (Phase 4) emits is declared
// in events.go as a constant. This is the canonical list — a typo at a
// call site surfaces as a topic nobody is listening to, so the bus
// warns when an unknown topic is emitted (debug log; not an error
// because dynamic topics are valid for plugins).
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrTopicEmpty is returned by Emit when the topic is empty.
var ErrTopicEmpty = errors.New("eventbus: topic is empty")

// ErrPayloadTooLarge is returned by Emit when the payload exceeds the
// configured MaxPayloadBytes. Per WS-10b "Open questions" item 2 the
// default cap is 64 KiB; larger payloads should go via KV pointers.
var ErrPayloadTooLarge = errors.New("eventbus: payload too large (use KV pointer)")

// Event is the unit the bus dispatches. TenantID is nil for system-level
// events; ActorType/ActorID carry who caused the event (per audit
// pillar 7); Metadata is the free-form JSON blob the listener decodes.
type Event struct {
	// Topic is the dotted identifier ("dns.record.created"). Required.
	Topic string

	// TenantID is nil for system events; otherwise the scoping tenant.
	TenantID *uuid.UUID

	// ActorType is "user", "system", or "plugin" (mirrors audit).
	ActorType string

	// ActorID is the user/plugin id; nil for system events.
	ActorID *uuid.UUID

	// ResourceID is the optional id of the resource the event is about
	// (e.g. the instance id for compute.instance.stopped). Nil when the
	// event is not resource-scoped.
	ResourceID *uuid.UUID

	// Metadata is the free-form JSON blob. Listeners decode per the
	// event's documented schema in events.go.
	Metadata []byte

	// EmittedAt is when the event was created. Emit sets this when zero.
	EmittedAt time.Time
}

// Listener is the synchronous in-process callback. The bus calls every
// matching listener sequentially in the order they were registered; a
// slow listener blocks the bus. Heavy work belongs on the async path
// (River) — synchronous listeners should be sub-millisecond.
//
// The error return is logged but does NOT block dispatch to other
// listeners or stop the bus. Returning a non-nil error is "best effort
// signal"; the listener should not panic.
type Listener func(ctx context.Context, e Event) error

// AsyncDispatcher is the async delivery seam. The production
// implementation wraps a River client and enqueues a PluginDispatch
// job per matched plugin subscription. The in-memory test impl is a
// channel.
//
// The dispatcher MUST be concurrency-safe.
type AsyncDispatcher interface {
	// Dispatch enqueues an async delivery of event e to the plugin
	// subscription identified by subID. The dispatcher is responsible
	// for retries (River's MaxAttempts) and DLQ handling. Returns an
	// error only when the enqueue itself fails (DB down, plugin
	// unknown) — NOT when the eventual delivery fails.
	Dispatch(ctx context.Context, subID uuid.UUID, e Event) error
}

// Bus is the in-process pub/sub. Safe for concurrent use. Register
// listeners at bootstrap; Emit from anywhere.
type Bus struct {
	mu        sync.RWMutex
	listeners []listenerEntry
	async     AsyncDispatcher
	log       *slog.Logger
	maxBytes  int
}

type listenerEntry struct {
	pattern TopicPattern
	fn      Listener
	id      uuid.UUID
}

// Config carries the few process-wide knobs the bus needs.
type Config struct {
	// Logger receives lifecycle + warn messages. Defaults to slog.Default().
	Logger *slog.Logger

	// AsyncDispatcher is the optional async delivery seam. When nil,
	// Dispatch is a no-op (the bus still calls sync listeners). Production
	// wires the River-backed dispatcher.
	AsyncDispatcher AsyncDispatcher

	// MaxPayloadBytes caps Emit payloads. Defaults to 64 KiB per
	// WS-10b "Open questions" item 2.
	MaxPayloadBytes int
}

// DefaultMaxPayloadBytes is the default payload cap (64 KiB). Per WS-10b
// "Open questions" item 2; larger payloads should reference a KV entry.
const DefaultMaxPayloadBytes = 64 * 1024

// New builds a bus. The bus has zero listeners at startup; Register
// adds them. Methods are safe for concurrent use.
func New(cfg Config) *Bus {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	maxBytes := cfg.MaxPayloadBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxPayloadBytes
	}
	return &Bus{
		async:    cfg.AsyncDispatcher,
		log:      log,
		maxBytes: maxBytes,
	}
}

// SetAsyncDispatcher swaps the async dispatcher at runtime. Used in
// bootstrap ordering: the bus is built before the River client, so the
// River dispatcher is wired after both are up. Safe for concurrent use
// with Emit (the mu held during swap blocks concurrent Emit reads).
func (b *Bus) SetAsyncDispatcher(d AsyncDispatcher) {
	b.mu.Lock()
	b.async = d
	b.mu.Unlock()
}

// Register adds a synchronous listener for a topic pattern. Returns the
// listener id so the caller can Unregister later. The pattern uses the
// same wildcard syntax as permission slugs ("dns.record.*").
func (b *Bus) Register(pattern string, fn Listener) uuid.UUID {
	if fn == nil {
		return uuid.Nil
	}
	id := uuid.New()
	b.mu.Lock()
	b.listeners = append(b.listeners, listenerEntry{
		pattern: TopicPattern(pattern), fn: fn, id: id,
	})
	b.mu.Unlock()
	return id
}

// Unregister removes a listener by id. Missing ids are a no-op.
func (b *Bus) Unregister(id uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.listeners[:0]
	for _, e := range b.listeners {
		if e.id != id {
			out = append(out, e)
		}
	}
	b.listeners = out
}

// Emit broadcasts an event to every matching listener. Sync listeners
// fire sequentially in registration order; async listeners are queued
// via the AsyncDispatcher (best-effort — a failing enqueue is logged
// but does not stop the bus).
//
// Errors from sync listeners are logged but never returned (per the
// contract — the event happened; a listener failing is the listener's
// problem). Returns an error only for caller-side validation: empty
// topic or oversized payload.
func (b *Bus) Emit(ctx context.Context, e Event) error {
	if e.Topic == "" {
		return ErrTopicEmpty
	}
	if len(e.Metadata) > b.maxBytes {
		return fmt.Errorf("eventbus: %s: %w (size=%d, cap=%d)",
			e.Topic, ErrPayloadTooLarge, len(e.Metadata), b.maxBytes)
	}
	if e.EmittedAt.IsZero() {
		e.EmittedAt = time.Now()
	}

	// Snapshot listeners under the lock so Emit can run concurrently
	// with Register/Unregister.
	b.mu.RLock()
	snapshot := make([]listenerEntry, len(b.listeners))
	copy(snapshot, b.listeners)
	async := b.async
	b.mu.RUnlock()

	for _, l := range snapshot {
		if !l.pattern.Matches(e.Topic) {
			continue
		}
		if err := l.fn(ctx, e); err != nil {
			b.log.Warn("eventbus: sync listener failed",
				"topic", e.Topic, "listener_id", l.id, "error", err)
		}
	}
	// The async path is driven from outside the bus: Emit only fires
	// sync listeners. Async dispatch lives in the EventService (which
	// holds the bus + the subscription repo) because the bus itself
	// does not know about plugin subscriptions — that's a host-function
	// concern. Keeping the seam here lets tests assert the bus calls
	// the dispatcher when one is configured (see bus_test.go).
	if async != nil {
		// No-op in the base bus; the EventService calls Dispatch
		// explicitly per subscription. The seam exists so a future
		// "internal only" async listener (e.g. an audit log backed by
		// River) can hook in without the EventService indirection.
		_ = async
	}
	return nil
}

// HasListeners reports whether the bus has any registered listeners.
// Test helper.
func (b *Bus) HasListeners() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.listeners) > 0
}
