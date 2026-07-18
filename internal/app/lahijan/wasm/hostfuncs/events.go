// Package hostfuncs: events.go builds the lahijan_events host module:
// emit (for broadcasting events) and subscribe/unsubscribe (for
// registering a listener on a topic pattern). Subscription rows are
// durable — the bus persists them so events emitted while the plugin
// is disabled are delivered when it resumes (WS-10b DoD).
//
// ABI (ADR-0024):
//
//	(import "lahijan_events" "emit"
//	  (func (param i32 i32 i32 i32) (result i32)))
//	(import "lahijan_events" "subscribe"
//	  (func (param i32 i32 i32 i32) (result i32)))
//	(import "lahijan_events" "unsubscribe"
//	  (func (param i32 i32 i32 i32) (result i32)))
//
// emit(topic_ptr, topic_len, payload_ptr, payload_len) -> status
//   - StatusSuccess (0).
//   - StatusDenied (-2)          : enforcer rejected events.emit.
//   - StatusInvalidArgument (-5) : topic empty or payload too large.
//
// subscribe(topic_ptr, topic_len, handler_ptr, handler_len) -> status
//   - StatusSuccess (0). Idempotent; re-subscribing the same tuple is a no-op.
//   - StatusDenied (-2)          : enforcer rejected events.listen:<topic>.
//
// unsubscribe(topic_ptr, topic_len, handler_ptr, handler_len) -> status
//   - StatusSuccess (0).
//
// emit puts the event on the in-process bus. Sync listeners (audit,
// metrics, any loaded plugin's listener) fire immediately. The
// EventService (separate; built on top of the bus) is responsible for
// consulting the subscription table and enqueuing async dispatches via
// River — that's where the durability promise is fulfilled.
package hostfuncs

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const (
	eventsModuleName = "lahijan_events"
	// MaxTopicLen caps a topic string. Topics are dotted identifiers;
	// 256 is generous.
	MaxTopicLen = 256
	// MaxHandlerNameLen caps a WASM export name. Matches wabt's
	// symbol-name cap.
	MaxHandlerNameLen = 256
)

func (r *registrar) buildEventsModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(eventsModuleName)

	emit := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		topicPtr := api.DecodeU32(stack[0])
		topicLen := api.DecodeU32(stack[1])
		payloadPtr := api.DecodeU32(stack[2])
		payloadLen := api.DecodeU32(stack[3])
		stack[0] = api.EncodeI32(r.eventsEmit(ctx, m, topicPtr, topicLen, payloadPtr, payloadLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(emit,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("emit")

	subscribe := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		topicPtr := api.DecodeU32(stack[0])
		topicLen := api.DecodeU32(stack[1])
		handlerPtr := api.DecodeU32(stack[2])
		handlerLen := api.DecodeU32(stack[3])
		stack[0] = api.EncodeI32(r.eventsSubscribe(ctx, m, topicPtr, topicLen, handlerPtr, handlerLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(subscribe,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("subscribe")

	unsubscribe := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		topicPtr := api.DecodeU32(stack[0])
		topicLen := api.DecodeU32(stack[1])
		handlerPtr := api.DecodeU32(stack[2])
		handlerLen := api.DecodeU32(stack[3])
		stack[0] = api.EncodeI32(r.eventsUnsubscribe(ctx, m, topicPtr, topicLen, handlerPtr, handlerLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(unsubscribe,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("unsubscribe")

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate events module: %w", err)
	}
	return nil
}

// eventsEmit is the Go-side implementation of emit.
func (r *registrar) eventsEmit(
	ctx context.Context,
	m api.Module,
	topicPtr, topicLen, payloadPtr, payloadLen uint32,
) int32 {
	pid, code := r.gate(ctx, eventsModuleName, "emit", permission.CapEventsEmit)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Bus == nil {
		return r.end(ctx, eventsModuleName, "emit", pid, permission.CapEventsEmit, StatusUnavailable)
	}
	if topicLen == 0 || topicLen > MaxTopicLen {
		return r.end(ctx, eventsModuleName, "emit", pid, permission.CapEventsEmit, StatusInvalidArgument)
	}
	topicBytes, err := readMemory(m, topicPtr, topicLen)
	if err != nil {
		return r.end(ctx, eventsModuleName, "emit", pid, permission.CapEventsEmit, StatusInvalidMemory)
	}
	payload, err := readMemory(m, payloadPtr, payloadLen)
	if err != nil {
		return r.end(ctx, eventsModuleName, "emit", pid, permission.CapEventsEmit, StatusInvalidMemory)
	}
	err = r.deps.Bus.Emit(ctx, eventbus.Event{
		Topic:    string(topicBytes),
		Metadata: payload,
	})
	if err != nil {
		r.log.Warn("hostfuncs: events.emit bus error", "plugin", pid, "error", err)
		return r.end(ctx, eventsModuleName, "emit", pid, permission.CapEventsEmit, StatusGenericFailure)
	}
	return r.end(ctx, eventsModuleName, "emit", pid, permission.CapEventsEmit, StatusSuccess)
}

// eventsSubscribe is the Go-side implementation of subscribe.
func (r *registrar) eventsSubscribe(
	ctx context.Context,
	m api.Module,
	topicPtr, topicLen, handlerPtr, handlerLen uint32,
) int32 {
	topicBytes, err := readMemory(m, topicPtr, topicLen)
	if err != nil {
		return StatusInvalidMemory
	}
	if topicLen == 0 || topicLen > MaxTopicLen {
		return StatusInvalidArgument
	}
	topicStr := string(topicBytes)
	// The events.listen slug is qualified by the topic pattern, so a
	// plugin that wants to listen to "dns.record.*" must have
	// "events.listen:dns.record.*" granted. The enforcer's wildcard
	// match means a grant of "events.listen:dns.record.*" satisfies a
	// request for "events.listen:dns.record.*" (exact) AND a more
	// specific "events.listen:dns.record.created" (prefix-match).
	slug := permission.CapEventsListen + ":" + topicStr
	pid, code := r.gate(ctx, eventsModuleName, "subscribe", slug)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginSubscriptions == nil {
		return r.end(ctx, eventsModuleName, "subscribe", pid, slug, StatusUnavailable)
	}
	handlerBytes, err := readMemory(m, handlerPtr, handlerLen)
	if err != nil {
		return r.end(ctx, eventsModuleName, "subscribe", pid, slug, StatusInvalidMemory)
	}
	if handlerLen == 0 || handlerLen > MaxHandlerNameLen {
		return r.end(ctx, eventsModuleName, "subscribe", pid, slug, StatusInvalidArgument)
	}
	if _, err := r.deps.Repos.PluginSubscriptions.Subscribe(ctx, database.CreateSubscriptionParams{
		PluginID:     pid,
		TopicPattern: topicStr,
		Handler:      string(handlerBytes),
	}); err != nil {
		r.log.Warn("hostfuncs: events.subscribe repo error", "plugin", pid, "error", err)
		return r.end(ctx, eventsModuleName, "subscribe", pid, slug, StatusGenericFailure)
	}
	return r.end(ctx, eventsModuleName, "subscribe", pid, slug, StatusSuccess)
}

// eventsUnsubscribe is the Go-side implementation of unsubscribe.
// Does not require events.listen permission: a plugin can always
// unsubscribe its own (plugin_id-scoped) row.
func (r *registrar) eventsUnsubscribe(
	ctx context.Context,
	m api.Module,
	topicPtr, topicLen, handlerPtr, handlerLen uint32,
) int32 {
	pid := pidFromContext(ctx)
	topicBytes, err := readMemory(m, topicPtr, topicLen)
	if err != nil {
		return r.end(ctx, eventsModuleName, "unsubscribe", pid, permission.CapEventsListen, StatusInvalidMemory)
	}
	handlerBytes, err := readMemory(m, handlerPtr, handlerLen)
	if err != nil {
		return r.end(ctx, eventsModuleName, "unsubscribe", pid, permission.CapEventsListen, StatusInvalidMemory)
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginSubscriptions == nil {
		return r.end(ctx, eventsModuleName, "unsubscribe", pid, permission.CapEventsListen, StatusUnavailable)
	}
	if err := r.deps.Repos.PluginSubscriptions.Unsubscribe(ctx, pid, string(topicBytes), string(handlerBytes)); err != nil {
		r.log.Warn("hostfuncs: events.unsubscribe repo error", "plugin", pid, "error", err)
		return r.end(ctx, eventsModuleName, "unsubscribe", pid, permission.CapEventsListen, StatusGenericFailure)
	}
	return r.end(ctx, eventsModuleName, "unsubscribe", pid, permission.CapEventsListen, StatusSuccess)
}

// pidFromContext resolves the plugin id from the context; returns
// uuid.Nil when missing (host functions handle that as a denial).
func pidFromContext(ctx context.Context) uuid.UUID {
	id, err := resolvePluginID(ctx)
	if err != nil {
		return uuid.Nil
	}
	return id
}
