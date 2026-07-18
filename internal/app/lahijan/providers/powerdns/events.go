// Package powerdns: events.go synthesizes change events. PDNS does NOT emit
// push events on its own — every change goes through this driver, so we
// emit a BusEvent into the WASM bus at the boundary. This keeps the event
// shape uniform across providers (Incus' real events vs PDNS' synthesized
// events look identical to a plugin subscribed to "dns.record.*").
//
// Topic names follow the canonical registry in
// internal/app/lahijan/wasm/eventbus/events.go. The metadata payload is the
// JSON-marshalled change set the caller supplied; the listener decodes per
// the event's documented schema.
//
// When no bus is configured (cfg.Bus == nil at NewClient) the emit is a
// silent no-op. This keeps unit tests + the dev path simple.
package powerdns

import (
	"context"
	"log/slog"
	"time"
)

// emitTimeout caps how long emitChange waits for the bus to accept an event.
// Matches the Incus driver's per-event cap so a wedged bus cannot back up
// the PDNS request pipeline.
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
	// Honour a caller-supplied deadline but cap at emitTimeout so a missing
	// deadline (e.g. background reconciler) cannot wedge the request.
	emitCtx, cancel := context.WithTimeout(ctx, emitTimeout)
	defer cancel()
	if err := p.bus.Emit(emitCtx, e); err != nil {
		slog.Warn("powerdns: event emit failed",
			"topic", e.Topic, "error", err)
	}
}
