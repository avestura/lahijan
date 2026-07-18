// Package powerdns: client.go is the single HTTP entry point every area file
// calls through. It owns the *http.Client, the per-request timeout, the
// user-agent, the API-key header, and the OTel span bootstrap for every
// outbound call.
//
// Production wires the client via NewClient from program.Start based on
// conf.providers.powerdns.*. Tests wire it against an httptest.Server (see
// fake/server.go).
package powerdns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// userAgent is sent on every request so the daemon's access log can tell
// Lahijan driver traffic apart from other API clients.
const userAgent = "lahijan-powerdns-driver/0.1"

// apiVersion is the REST API version prefix PDNS exposes.
const apiVersion = "/api/v1"

// apiKeyHeader is the HTTP header PDNS' webserver expects the API key in.
const apiKeyHeader = "X-API-Key"

// Provider implements providers.Provider plus the PDNS-specific surface
// (zones, records, cryptokeys, metadata). The struct holds only immutable
// fields after NewClient, except for the lazily-populated capabilities cache
// (guarded by capabilitiesMu). All methods are safe for concurrent use.
//
// Per pillar 1 the Name() string is "powerdns" (internal-only); end users
// see "dns".
type Provider struct {
	// httpClient executes every request. In production the Transport is
	// a plain http.Transport (PDNS exposes HTTP, not Unix sockets); in
	// tests it points at httptest.
	httpClient *http.Client

	// baseURL is the scheme + host prefix prepended to every request path.
	// Typical dev value: "http://powerdns:8081".
	baseURL string

	// apiKey is the secret sent on every request in the X-API-Key header.
	// SENSITIVE — never logged.
	apiKey string

	// timeout is the per-request timeout applied via context.
	timeout time.Duration

	// capabilities caches the daemon's feature flags after the first Ping.
	// Lazy-populated; consumers call Capabilities() which calls Ping once
	// if the cache is empty. Safe for concurrent use via capabilitiesMu.
	capabilitiesMu sync.RWMutex
	capabilities   Capabilities
	capabilitiesOK bool

	// bus is the optional WASM event bus the synthesize-events hook fans
	// change events into. Nil when events are disabled.
	bus EventBus
}

// EventBus is the minimal subset of *eventbus.Bus the driver needs. Keeping
// it as an interface here lets tests substitute a recorder without pulling
// the eventbus package into every test file.
type EventBus interface {
	// Emit mirrors eventbus.Bus.Emit. The driver calls it from the same
	// goroutine as the change; errors are logged but do not revert the
	// change.
	Emit(ctx context.Context, e BusEvent) error
}

// BusEvent is the minimal event shape the driver emits into the WASM bus.
// Mirrors eventbus.Event; defined here so callers do not need to import the
// eventbus package to use the driver.
type BusEvent struct {
	// Topic is the dotted identifier ("dns.zone.created").
	Topic string

	// TenantID is the optional tenant scope; nil when the event is
	// system-level.
	TenantID *string

	// ActorType is "user", "system", or "plugin" (mirrors audit).
	ActorType string

	// ResourceID is the optional id of the resource the event is about.
	ResourceID *string

	// Metadata is the free-form JSON blob; the listener decodes per the
	// event's documented schema in wasm/eventbus/events.go.
	Metadata []byte
}

// Capabilities is the per-PDNS feature-flag bundle returned by
// Capabilities(). Mirrors providers.Capabilities but kept as a value type
// (not a pointer) so callers can copy it freely.
type Capabilities struct {
	// ClusterMode is true when the PDNS daemon is part of a master/slave
	// or multi-node setup. False for the single-node MVP topology
	// (ADR-0005).
	ClusterMode bool

	// RemoteReplication is true when AXFR to secondaries is configured.
	// False by default per the WS-12 "Open questions" resolution.
	RemoteReplication bool

	// ServerVersion is the daemon's version string (e.g. "4.9.0").
	ServerVersion string

	// APIVersion is the daemon's REST API version (e.g. "1").
	APIVersion string

	// DaemonType is "authoritative" or "recursor". Lahijan only uses
	// authoritative; recursor lands in WS-28.
	DaemonType string

	// DNSSECSupported is true when the daemon is built with DNSSEC
	// support (always true for 4.x+).
	DNSSECSupported bool
}

// Config is the bundle passed to NewClient by program.Start.
type Config struct {
	// HTTPClient is the configured *http.Client.
	HTTPClient *http.Client

	// BaseURL is the daemon's HTTP origin (e.g. "http://powerdns:8081").
	BaseURL string

	// APIKey is the secret used to authenticate API calls. Required.
	// SENSITIVE — never logged.
	APIKey string

	// RequestTimeout is the per-call timeout. Zero means use the default
	// 30s.
	RequestTimeout time.Duration

	// Bus is the optional WASM event bus for change-event synthesis.
	// Nil disables event synthesis.
	Bus EventBus
}

// defaultRequestTimeout is used when Config.RequestTimeout is zero.
const defaultRequestTimeout = 30 * time.Second

// NewClient builds a Provider from the given Config. The returned provider
// has not yet contacted the daemon; the first call to Ping (or the first
// request) will. Returns an error if HTTPClient, BaseURL, or APIKey is missing.
func NewClient(cfg Config) (*Provider, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("powerdns: Config.HTTPClient is required")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("powerdns: Config.BaseURL is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("powerdns: Config.APIKey is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &Provider{
		httpClient: cfg.HTTPClient,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		timeout:    timeout,
		bus:        cfg.Bus,
	}, nil
}

// do issues a request against the daemon and decodes the JSON response into
// out. path is the /servers/localhost/... path with optional query string.
// body is the request payload (nil for GET/DELETE).
//
// Every call opens an OTel span named "powerdns.http.<method>" so the
// upstream area-specific spans have a child HTTP-level span.
func (p *Provider) do(ctx context.Context, method, path string, body, out any) error {
	ctx, span := startSpan(ctx, "http."+strings.ToLower(method),
		attribute.String("powerdns.path", path))
	defer span.End()

	// Apply the per-request timeout. We own the canceler via defer so the
	// timer does not leak; the http.Client honors the derived deadline.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := p.buildRequest(ctx, method, path, body)
	if err != nil {
		setStatus(span, err)
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return fmt.Errorf("powerdns: http %s %s: %w", method, path, err)
	}

	if classErr := classify(resp); classErr != nil {
		setStatus(span, classErr)
		return fmt.Errorf("powerdns: http %s %s: %w", method, path, classErr)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		setStatus(span, err)
		_ = resp.Body.Close()
		return fmt.Errorf("powerdns: read response: %w", err)
	}
	_ = resp.Body.Close()

	if out == nil {
		setStatus(span, nil)
		return nil
	}
	// PDNS endpoints that take a body return either JSON (200/201/202) or
	// an empty body (204 No Content on PATCH/DELETE). An empty body with
	// a non-nil out is success without payload.
	if len(raw) == 0 {
		setStatus(span, nil)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		setStatus(span, err)
		return fmt.Errorf("powerdns: decode response %s %s: %w", method, path, err)
	}
	setStatus(span, nil)
	return nil
}

// maxResponseBytes caps a single response body so a misbehaving daemon cannot
// drive the driver OOM. 64 MiB covers the largest zone transfer Lahijan uses.
const maxResponseBytes = 64 * 1024 * 1024

// buildRequest constructs the *http.Request with the standard headers. The
// caller owns the lifetime of the context via the canceler from
// context.WithTimeout.
func (p *Provider) buildRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("powerdns: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	fullURL := p.baseURL + apiVersion + "/" + strings.TrimLeft(path, "/")
	// Validate the URL — belt-and-braces against malformed config.
	if _, err := url.Parse(fullURL); err != nil {
		return nil, fmt.Errorf("powerdns: invalid url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("powerdns: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set(apiKeyHeader, p.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
