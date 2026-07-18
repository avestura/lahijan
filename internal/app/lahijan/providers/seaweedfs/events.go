// Package seaweedfs: events.go synthesizes change events. SeaweedFS does NOT
// emit push events on its own — every change goes through this driver, so
// we emit a BusEvent into the WASM bus at the boundary. This keeps the
// event shape uniform across providers (Incus' real events vs PowerDNS'
// synthesized events vs SeaweedFS' synthesized events look identical to a
// plugin subscribed to "storage.bucket.*").
//
// Topic names follow the canonical registry in
// internal/app/lahijan/wasm/eventbus/events.go. The metadata payload is the
// JSON-marshalled change set the caller supplied; the listener decodes per
// the event's documented schema.
//
// When no bus is configured (cfg.Bus == nil at NewClient) the emit is a
// silent no-op. This keeps unit tests + the dev path simple.
package seaweedfs

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// emitTimeout caps how long emitChange waits for the bus to accept an event.
// Matches the Incus + PowerDNS drivers' per-event cap so a wedged bus cannot
// back up the SeaweedFS request pipeline.
const emitTimeout = 5 * time.Second

// emitChange fans the given event into the WASM bus (if configured). Errors
// are logged but never returned to the caller: a wedged bus must not roll
// back a successful change to the daemon.
//
// The emit uses a short per-call timeout so a wedged bus does not block the
// HTTP response forever. The timeout matches the bus's documented behaviour
// (5s for synchronous listeners; the async path enqueues immediately).
func (p *Provider) emitChange(ctx context.Context, e BusEvent) {
	if p == nil || p.bus == nil {
		return
	}
	emitCtx, cancel := context.WithTimeout(ctx, emitTimeout)
	defer cancel()
	if err := p.bus.Emit(emitCtx, e); err != nil {
		slog.Warn("seaweedfs: event emit failed",
			"topic", e.Topic, "error", err)
	}
}

// asRawJSON marshals v to a json.RawMessage; on marshal error it returns
// an empty object so the caller never has to handle a nil byte slice.
func asRawJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// strPtr returns a pointer to a freshly-allocated copy of s. Used for the
// optional ResourceID / TenantID fields on synthesized events.
func strPtr(s string) *string { return &s }
