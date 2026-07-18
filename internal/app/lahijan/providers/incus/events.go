// Package incus: events.go opens a long-lived websocket to the Incus events
// stream (GET /1.0/events, upgraded to websocket) and fans every event into
// the WASM event bus (internal/app/lahijan/wasm/eventbus). The lifecycle is
// owned by an EventListener returned from StartEventListener; the caller
// (typically program.Start) calls Close on shutdown.
//
// The Incus events stream multiplexes four event types:
//
//   - "lifecycle"  — instance/project/image lifecycle transitions
//   - "operation"  — async operation state changes
//   - "logging"    — daemon log lines (Lahijan ignores these)
//   - "metric"     — periodic metric snapshots (Lahijan ignores these)
//
// Only "lifecycle" events are translated into WASM bus events; their action
// strings map directly to the canonical topics in eventbus.events.go
// ("instance-started" -> "compute.instance.started").
package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EventListener is the handle returned by StartEventListener. Call Close to
// stop the listener and release the websocket + goroutine.
type EventListener struct {
	// cancel stops the read loop and triggers a clean close.
	cancel context.CancelFunc

	// done is closed when the read loop has fully exited.
	done chan struct{}

	// wg tracks goroutines spawned by the listener (reconnect loop,
	// dispatcher). Wait returns when all have stopped.
	wg sync.WaitGroup
}

// Close stops the listener. Safe to call multiple times. Returns nil when the
// listener has fully stopped; the first call blocks (briefly) waiting for
// goroutines to exit.
func (l *EventListener) Close() error {
	if l == nil {
		return nil
	}
	if l.cancel != nil {
		l.cancel()
	}
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	return nil
}

// EventListenerConfig carries the few knobs StartEventListener needs.
type EventListenerConfig struct {
	// Bus is the WASM event bus to fan decoded events into. Required.
	Bus EventBus

	// MaxReconnect is the upper bound on the reconnect backoff. Default 30s.
	MaxReconnect time.Duration

	// MaxPayloadBytes caps a single event payload. Default 1 MiB.
	MaxPayloadBytes int

	// Logger receives lifecycle + warn messages. Defaults to slog.Default().
	Logger *slog.Logger
}

// StartEventListener opens the events websocket against the daemon and starts
// a goroutine that decodes each event and fans it into the configured bus.
// Returns immediately; the listener runs until Close or until the provider's
// context is cancelled.
//
// The listener handles transient disconnects with an exponential backoff
// (with jitter) capped at cfg.MaxReconnect. Permanent errors (auth failure,
// config error) abort the loop and are logged at Error.
//
// Per pillar 1 the listener never surfaces the words "Incus" or "incus" to
// the user — only to operator logs. The events it emits to the bus carry
// user-facing topics ("compute.instance.stopped").
func (p *Provider) StartEventListener(cfg EventListenerConfig) (*EventListener, error) {
	if p.bus == nil && cfg.Bus == nil {
		return nil, errors.New("incus: events listener requires an event bus")
	}
	bus := cfg.Bus
	if bus == nil {
		bus = p.bus
	}
	if cfg.MaxReconnect <= 0 {
		cfg.MaxReconnect = 30 * time.Second
	}
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = 1 << 20 // 1 MiB
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())
	listener := &EventListener{cancel: cancel, done: make(chan struct{})}

	// Build the events URL once; the listener reconnects to it on transient
	// disconnects. We pin to the "lifecycle" type to skip the high-volume
	// "logging" + "metric" streams.
	wsURL := p.eventsWebSocketURL()

	dispatcher := newEventDispatcher(bus, p, log, cfg.MaxPayloadBytes)
	listener.wg.Add(1)
	go listener.run(ctx, wsURL, cfg.MaxReconnect, dispatcher, log)
	return listener, nil
}

// run is the main reconnect loop. It opens a websocket, reads events until
// the read fails or the context is cancelled, then either reconnects (with
// backoff) or returns.
func (l *EventListener) run(ctx context.Context, wsURL string, maxBackoff time.Duration, d *eventDispatcher, log *slog.Logger) {
	defer l.wg.Done()
	defer close(l.done)

	consecutive := 0
	for {
		if err := ctx.Err(); err != nil {
			log.Debug("incus events listener: context done", "error", err)
			return
		}
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{
			"User-Agent": {userAgent},
		})
		if err != nil {
			log.Warn("incus events listener: dial failed", "error", err, "attempt", consecutive)
			if !shouldRetry(err) {
				log.Error("incus events listener: permanent dial error; aborting", "error", err)
				return
			}
			consecutive++
			d.backoff(ctx, consecutive, maxBackoff)
			continue
		}
		consecutive = 0
		log.Info("incus events listener: connected", "url", wsURL)

		// Read loop. Exits on read error or context cancel.
		err = d.readLoop(ctx, conn)
		_ = conn.Close()
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Warn("incus events listener: read loop ended", "error", err)
		}
		if err := ctx.Err(); err != nil {
			log.Debug("incus events listener: context done after read loop", "error", err)
			return
		}
		d.backoff(ctx, 1, maxBackoff)
	}
}

// eventsWebSocketURL converts the provider's REST baseURL into the websocket
// URL for the events endpoint.
//
//	http://incus  -> ws://incus/1.0/events?type=lifecycle
//	https://h:8443 -> wss://h:8443/1.0/events?type=lifecycle
func (p *Provider) eventsWebSocketURL() string {
	scheme := "ws"
	host := p.baseURL
	switch {
	case strings.HasPrefix(p.baseURL, "https://"):
		scheme = "wss"
		host = strings.TrimPrefix(p.baseURL, "https://")
	case strings.HasPrefix(p.baseURL, "http://"):
		host = strings.TrimPrefix(p.baseURL, "http://")
	}
	q := url.Values{}
	q.Set("type", "lifecycle")
	return fmt.Sprintf("%s://%s%s/events?%s", scheme, host, apiVersion, q.Encode())
}

// eventDispatcher is the per-listener state that decodes Incus event envelopes
// and emits bus events. Kept unexported — callers use EventListener.
type eventDispatcher struct {
	bus      EventBus
	prov     *Provider
	log      *slog.Logger
	maxBytes int
}

func newEventDispatcher(bus EventBus, p *Provider, log *slog.Logger, maxBytes int) *eventDispatcher {
	return &eventDispatcher{bus: bus, prov: p, log: log, maxBytes: maxBytes}
}

// readLoop reads events one at a time until the read fails or the context is
// cancelled.
func (d *eventDispatcher) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("incus events listener: read: %w", err)
		}
		if len(raw) > d.maxBytes {
			d.log.Warn("incus events listener: event exceeds max payload; dropping",
				"size", len(raw), "max", d.maxBytes)
			continue
		}
		var env EventEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			d.log.Warn("incus events listener: malformed event; dropping", "error", err)
			continue
		}
		if env.Type != "lifecycle" {
			continue
		}
		event, ok := d.translate(env)
		if !ok {
			continue
		}
		// Emit with a short per-call timeout so a wedged bus does not
		// back up the read loop.
		emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := d.bus.Emit(emitCtx, event); err != nil {
			d.log.Warn("incus events listener: bus emit failed", "error", err, "topic", event.Topic)
		}
		cancel()
	}
}

// translate converts an Incus lifecycle EventEnvelope into a BusEvent. Returns
// ok=false for events Lahijan does not care about (image + project lifecycle
// that is not instance-scoped, etc.).
func (d *eventDispatcher) translate(env EventEnvelope) (BusEvent, bool) {
	var lc LifecycleEvent
	if err := json.Unmarshal(env.Metadata, &lc); err != nil {
		d.log.Debug("incus events listener: skipping lifecycle event with malformed metadata", "error", err)
		return BusEvent{}, false
	}
	topic, ok := lifecycleTopic(lc.Action)
	if !ok {
		return BusEvent{}, false
	}
	tenantID := tenantIDFromSourceURL(d.prov, lc.Source)
	resourceID := resourceIDFromSourceURL(lc.Source)
	return BusEvent{
		Topic:      topic,
		TenantID:   tenantID,
		ActorType:  "system",
		ResourceID: resourceID,
		Metadata:   env.Metadata,
	}, true
}

// lifecycleTopic maps an Incus lifecycle action ("instance-started") to a
// canonical WASM bus topic ("compute.instance.started"). Returns ok=false for
// actions Lahijan does not surface.
func lifecycleTopic(action string) (string, bool) {
	switch action {
	case "instance-created":
		return "compute.instance.created", true
	case "instance-started":
		return "compute.instance.started", true
	case "instance-stopped":
		return "compute.instance.stopped", true
	case "instance-restarted":
		return "compute.instance.restarted", true
	case "instance-deleted":
		return "compute.instance.deleted", true
	default:
		return "", false
	}
}

// tenantIDFromSourceURL extracts the tenant id from an Incus source URL like
// "/1.0/instances/foo?project=lahijan-tenant-<uuid>". The tenant id is parsed
// via the provider's TenantIDFromProject. Returns nil when the source is not
// tenant-scoped.
func tenantIDFromSourceURL(p *Provider, source string) *string {
	q := sourceURLQuery(source)
	if q == "" {
		return nil
	}
	vals, err := url.ParseQuery(q)
	if err != nil {
		return nil
	}
	project := vals.Get("project")
	if project == "" {
		return nil
	}
	id, err := p.TenantIDFromProject(project)
	if err != nil {
		return nil
	}
	s := id.String()
	return &s
}

// resourceIDFromSourceURL extracts the trailing segment of an Incus source
// URL ("/1.0/instances/foo?..." -> "foo"). Returns nil when no segment can
// be parsed.
func resourceIDFromSourceURL(source string) *string {
	pathOnly := source
	if idx := strings.Index(source, "?"); idx >= 0 {
		pathOnly = source[:idx]
	}
	idx := strings.LastIndex(pathOnly, "/")
	if idx < 0 || idx == len(pathOnly)-1 {
		return nil
	}
	s := pathOnly[idx+1:]
	return &s
}

// sourceURLQuery returns the substring after the first "?" in source, or "".
func sourceURLQuery(source string) string {
	idx := strings.Index(source, "?")
	if idx < 0 {
		return ""
	}
	return source[idx+1:]
}

// shouldRetry reports whether the dial error is worth retrying.
func shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// gorilla/websocket exposes a small set of "permanent" errors via the
	// *websocket.CloseError type; anything else is treated as transient.
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
		// A close on the very first dial is a permanent server-side reject.
		return false
	}
	return true
}

// backoff sleeps for an exponentially-growing duration with jitter, capped at
// maxBackoff. Returns early if ctx is cancelled.
func (d *eventDispatcher) backoff(ctx context.Context, attempt int, maxBackoff time.Duration) {
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	if base > maxBackoff {
		base = maxBackoff
	}
	// Add up to 30% jitter so a fleet of listeners does not synchronise.
	jitter := time.Duration(rand.Int63n(int64(base) / 3))
	timer := time.NewTimer(base + jitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
