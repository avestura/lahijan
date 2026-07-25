// Package events provides idiomatic access to the Lahijan in-process event
// bus. Plugins emit events on topics and subscribe to topic patterns
// (e.g. "dns.record.*") by registering a WASM export name the host calls
// on every matching event.
package events

import (
	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// Emit publishes payload to topic on the event bus. The payload is
// opaque bytes; conventions for JSON encoding are the caller's responsibility.
func Emit(topic string, payload []byte) error {
	t := []byte(topic)
	code := eventsEmit(mem.Ptr(t), mem.Len(t), mem.Ptr(payload), mem.Len(payload))
	return status.FromCode(code)
}

// Subscribe registers handlerName (a WASM export in the same plugin) to be
// called on every event matching topicPattern. The pattern may end with
// ".*" to match sub-topics (e.g. "dns.record.*" matches
// "dns.record.created"). Idempotent: re-subscribing the same tuple is a
// no-op.
func Subscribe(topicPattern, handlerName string) error {
	t := []byte(topicPattern)
	h := []byte(handlerName)
	code := eventsSubscribe(mem.Ptr(t), mem.Len(t), mem.Ptr(h), mem.Len(h))
	return status.FromCode(code)
}

// Unsubscribe removes a previously registered subscription. A plugin can
// always unsubscribe its own (plugin_id-scoped) subscriptions.
func Unsubscribe(topicPattern, handlerName string) error {
	t := []byte(topicPattern)
	h := []byte(handlerName)
	code := eventsUnsubscribe(mem.Ptr(t), mem.Len(t), mem.Ptr(h), mem.Len(h))
	return status.FromCode(code)
}
