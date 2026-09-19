// Package incus: client.go is the single HTTP entry point every area file calls
// through. It owns the *http.Client (Unix-socket or HTTPS-remote), the
// per-request timeout, the user-agent, and the OTel span bootstrap for every
// outbound call.
//
// Production wires the client via NewUnixClient or NewRemoteClient from
// program.Start based on conf.providers.incus.*. Tests wire it via
// NewClient against an httptest.Server (see fake/server.go).
package incus

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
)

// userAgent is sent on every request so the daemon's access log can tell
// Lahijan driver traffic apart from the `incus` CLI or other clients.
const userAgent = "lahijan-incus-driver/0.1"

// apiVersion is the REST API version prefix. Incus has historically used
// "/1.0" and has never bumped it (new endpoints are added under /1.0).
const apiVersion = "/1.0"

// Provider implements providers.Provider plus the Incus-specific surface
// (projects, instances, images, ...). The struct holds only immutable fields
// after NewClient, except for the lazily-populated capabilities cache (guarded
// by capabilitiesMu). All methods are safe for concurrent use.
//
// Per pillar 1 the Name() string is "incus" (internal-only); end users see
// "compute".
type Provider struct {
	// httpClient executes every request. In production the Transport is
	// configured with a Unix-socket DialContext (NewUnixClient) or HTTPS +
	// mTLS (NewRemoteClient). In tests it points at httptest.
	httpClient *http.Client

	// baseURL is the scheme + host prefix prepended to every request path.
	// For Unix-socket mode this is "http://incus" (a synthetic host so the
	// http.Client can route via Transport); for HTTPS-remote mode this is
	// the configured URL (e.g. "https://incus.lan:8443").
	baseURL string

	// timeout is the per-request timeout applied via context. Long-running
	// operations (image copy, snapshot) use the async operations API
	// (POST + Wait) instead of a blocking HTTP call.
	timeout time.Duration

	// capabilities caches the daemon's feature flags after the first Ping.
	// Lazy-populated; consumers call Capabilities() which calls Ping once
	// if the cache is empty. Safe for concurrent use via capabilitiesMu.
	capabilitiesMu sync.RWMutex
	capabilities   Capabilities
	capabilitiesOK bool

	// projectPrefix is prepended to the tenant UUID to form the Incus
	// project name. Default "lahijan-tenant-".
	projectPrefix string

	// projectFeatures is the per-feature flag bundle applied at tenant
	// bootstrap (see projects.go).
	projectFeatures ProjectFeatures

	// bus is the optional WASM event bus the events listener fans Incus
	// events into. Nil when events are disabled. The events listener is
	// the only writer to the bus from inside this package.
	bus EventBus

	// wsDialer is the gorilla/websocket dialer used for every per-fd,
	// VNC, and events websocket the driver opens against the daemon. It
	// mirrors the HTTP transport: unix-socket NetDialContext in
	// NewUnixClient, the same TLS config (incl. insecureSkipVerify) in
	// NewRemoteClient. Defaults to websocket.DefaultDialer for tests
	// that construct a Provider against a plain httptest server.
	//
	// This is required because gorilla/websocket's DefaultDialer
	// verifies TLS certs + dials TCP — so without it, wss:// dials
	// against a self-signed-cert daemon (the WSL2 dev Incus, any
	// production daemon using the Incus auto-generated cert) fail with
	// "x509: certificate signed by unknown authority", and ws:// dials
	// against a unix-socket daemon try TCP and time out.
	wsDialer *websocket.Dialer
}

// EventBus is the minimal subset of *eventbus.Bus the driver needs. Keeping
// it as an interface here lets tests substitute a recorder without pulling
// the eventbus package into every test file (and lets us avoid an import
// cycle if eventbus ever needs to look up capabilities from a provider).
type EventBus interface {
	// Emit mirrors eventbus.Bus.Emit. The driver calls it from the events
	// listener goroutine; errors are logged but do not stop the listener.
	Emit(ctx context.Context, e BusEvent) error
}

// BusEvent is the minimal event shape the driver emits into the WASM bus.
// Mirrors eventbus.Event; defined here so callers do not need to import the
// eventbus package to use the driver.
type BusEvent struct {
	// Topic is the dotted identifier ("compute.instance.stopped").
	Topic string

	// TenantID is the optional tenant scope decoded from the Incus project
	// name (the events listener rewrites "lahijan-tenant-<uuid>" -> uuid).
	TenantID *string

	// ActorType is "user", "system", or "plugin" (mirrors audit).
	ActorType string

	// ResourceID is the optional resource id extracted from the event's
	// source URL.
	ResourceID *string

	// Metadata is the free-form JSON blob; the listener decodes the
	// Incus event envelope and forwards the relevant fields.
	Metadata []byte
}

// ProjectFeatures carries the project-feature flags applied at tenant
// bootstrap. Mirrors conf.IncusProjectFeatures so callers do not need to
// import conf when constructing a Provider in tests.
type ProjectFeatures struct {
	Images         bool
	Profiles       bool
	Networks       bool
	StorageVolumes bool
	StorageBuckets bool
}

// Capabilities is the per-Incus feature-flag bundle returned by
// Capabilities(). Mirrors providers.Capabilities but kept as a value type
// (not a pointer) so callers can copy it freely.
type Capabilities struct {
	// ClusterMode is true when the daemon is clustered (multi-node).
	// False for the single-node MVP topology (ADR-0005).
	ClusterMode bool

	// VMSupport is true when the daemon can run virtual machines (qemu
	// installed and CPU supports virtualization). False on hardware that
	// cannot run VMs.
	VMSupport bool

	// ServerVersion is the daemon's version string (e.g. "6.0.0").
	ServerVersion string

	// APIVersion is the daemon's REST API version string (e.g. "1.0").
	APIVersion string

	// ServerName is the cluster member name (or the single-node hostname).
	ServerName string
}

// Config is the bundle passed to NewClient by program.Start.
type Config struct {
	// HTTPClient is the configured *http.Client (Unix socket or HTTPS).
	HTTPClient *http.Client

	// BaseURL is the synthetic host for Unix-socket mode ("http://incus")
	// or the real HTTPS URL for remote mode.
	BaseURL string

	// RequestTimeout is the per-call timeout. Zero means use the default
	// 30s.
	RequestTimeout time.Duration

	// ProjectPrefix is prepended to tenant UUIDs to form the Incus
	// project name. Defaults to "lahijan-tenant-".
	ProjectPrefix string

	// ProjectFeatures carries the per-feature flags applied at tenant
	// bootstrap.
	ProjectFeatures ProjectFeatures

	// Bus is the optional WASM event bus for the events listener.
	// Nil disables the events listener.
	Bus EventBus

	// WSDialer is the gorilla/websocket dialer used for every websocket
	// the driver opens (per-fd exec, VNC, events). NewUnixClient and
	// NewRemoteClient build one that matches their HTTP transport
	// (unix-socket NetDial / TLS config). When nil, NewClient falls back
	// to websocket.DefaultDialer so tests against a plain httptest server
	// (TCP, no TLS) keep working unchanged.
	WSDialer *websocket.Dialer
}

// defaultRequestTimeout is used when Config.RequestTimeout is zero.
const defaultRequestTimeout = 30 * time.Second

// defaultProjectPrefix is used when Config.ProjectPrefix is empty.
const defaultProjectPrefix = "lahijan-tenant-"

// NewClient builds a Provider from the given Config. The returned provider
// has not yet contacted the daemon; the first call to Ping (or the first
// request) will. Returns an error if HTTPClient or BaseURL is missing.
func NewClient(cfg Config) (*Provider, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("incus: Config.HTTPClient is required")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("incus: Config.BaseURL is required")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	prefix := cfg.ProjectPrefix
	if prefix == "" {
		prefix = defaultProjectPrefix
	}
	wsDialer := cfg.WSDialer
	if wsDialer == nil {
		// Tests that wire the provider against a plain httptest server
		// (TCP, no TLS) get the default dialer; production paths
		// (NewUnixClient / NewRemoteClient) always supply a matching one.
		wsDialer = websocket.DefaultDialer
	}
	return &Provider{
		httpClient:      cfg.HTTPClient,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		timeout:         timeout,
		projectPrefix:   prefix,
		projectFeatures: cfg.ProjectFeatures,
		bus:             cfg.Bus,
		wsDialer:        wsDialer,
	}, nil
}

// NewUnixClient builds a Provider that talks to a local Incus daemon over a
// Unix socket. The synthetic host "incus" is used so the http.Client can
// route through its Transport without a real DNS lookup.
func NewUnixClient(socketPath string, cfg Config) (*Provider, error) {
	if socketPath == "" {
		return nil, errors.New("incus: socketPath is required for Unix-socket mode")
	}
	// Shared dialer: both the HTTP transport and the websocket dialer
	// reach the daemon over the SAME unix socket. The synthetic "incus"
	// host is irrelevant to a unix dial (the NetDialContext ignores
	// network+addr and dials the socket directly).
	unixDial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: 5 * time.Second}
		return d.DialContext(ctx, "unix", socketPath)
	}
	transport := &http.Transport{
		DialContext: unixDial,
		// Disable keep-alive pooling — Incus' server does not benefit from
		// it and a long-lived idle conn can mask a daemon restart.
		DisableKeepAlives: true,
	}
	cfg.HTTPClient = &http.Client{Transport: transport}
	cfg.BaseURL = "http://incus"
	if cfg.WSDialer == nil {
		cfg.WSDialer = &websocket.Dialer{
			NetDialContext:   unixDial,
			HandshakeTimeout: 10 * time.Second,
		}
	}
	return NewClient(cfg)
}

// NewRemoteClient builds a Provider that talks to a remote Incus daemon over
// HTTPS with optional mTLS. Used when the daemon lives on a different host
// (e.g. a cluster member) than the Lahijan process.
func NewRemoteClient(remoteURL string, tlsCfg TLSConfig, cfg Config) (*Provider, error) {
	if remoteURL == "" {
		return nil, errors.New("incus: remoteURL is required for HTTPS-remote mode")
	}
	tlsConfig, err := buildTLSConfig(tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("incus: tls config: %w", err)
	}
	transport := &http.Transport{
		TLSClientConfig:   tlsConfig,
		DisableKeepAlives: true,
	}
	cfg.HTTPClient = &http.Client{Transport: transport}
	cfg.BaseURL = strings.TrimRight(remoteURL, "/")
	if cfg.WSDialer == nil {
		// The websocket dialer MUST share the HTTP transport's TLS config
		// (incl. InsecureSkipVerify): the daemon's auto-generated cert is
		// self-signed, so gorilla/websocket's DefaultDialer — which verifies
		// certs — rejects every wss:// per-fd/exec + VNC + events dial with
		// "x509: certificate signed by unknown authority". Sharing the config
		// makes the WS path accept exactly the certs the HTTP path already
		// accepts.
		cfg.WSDialer = &websocket.Dialer{
			TLSClientConfig:  tlsConfig,
			HandshakeTimeout: 10 * time.Second,
		}
	}
	return NewClient(cfg)
}

// TLSConfig carries the mTLS material for HTTPS-remote mode. Mirrors
// conf.IncusTLSConfig so the conf package stays out of the driver package.
type TLSConfig struct {
	ServerCert         string // PEM
	ClientCert         string // PEM
	ClientKey          string // PEM
	InsecureSkipVerify bool
}

// buildTLSConfig assembles the *tls.Config from a TLSConfig.
func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	out := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	if cfg.ServerCert != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(cfg.ServerCert)) {
			return nil, errors.New("incus: failed to parse server cert PEM")
		}
		out.RootCAs = pool
	}
	if cfg.ClientCert != "" && cfg.ClientKey != "" {
		cert, err := tls.X509KeyPair([]byte(cfg.ClientCert), []byte(cfg.ClientKey))
		if err != nil {
			return nil, fmt.Errorf("incus: parse client keypair: %w", err)
		}
		out.Certificates = []tls.Certificate{cert}
	}
	return out, nil
}

// do issues a request against the daemon and decodes the Response envelope.
// path is the /1.0/... path with optional query string. The returned
// RawMessage is the Response.Metadata field; the caller decodes per the
// endpoint shape. If the response body is not the JSON envelope (e.g. a raw
// binary payload), the raw body is returned as RawMessage and the envelope
// decode error is swallowed.
//
// Every call opens an OTel span named "incus.http.<method>" so the upstream
// area-specific spans have a child HTTP-level span.
func (p *Provider) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	ctx, span := startSpan(ctx, "http."+strings.ToLower(method),
		attribute.String("incus.path", path))
	defer span.End()

	// Apply the per-request timeout. We own the canceler via defer so the
	// timer does not leak; the http.Client honors the derived deadline.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := p.buildRequest(ctx, method, path, body)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return nil, fmt.Errorf("incus: http %s %s: %w", method, path, err)
	}

	if classErr := classify(resp); classErr != nil {
		setStatus(span, classErr)
		return nil, fmt.Errorf("incus: http %s %s: %w", method, path, classErr)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		setStatus(span, err)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("incus: read response: %w", err)
	}
	_ = resp.Body.Close()

	var env Response
	if err := json.Unmarshal(raw, &env); err != nil {
		// Some endpoints (image binary upload, exec websocket upgrade) return
		// a raw body, not the JSON envelope. If we cannot decode the envelope
		// we surface the raw body so the caller can decide.
		setStatus(span, nil)
		return json.RawMessage(raw), nil
	}
	setStatus(span, nil)
	return env.Metadata, nil
}

// maxResponseBytes caps a single response body so a misbehaving daemon cannot
// drive the driver OOM. 64 MiB covers every Incus payload Lahijan uses today
// (instance list, image catalog, operation state).
const maxResponseBytes = 64 * 1024 * 1024

// buildRequest constructs the *http.Request with the standard headers. The
// caller owns the lifetime of the context via the canceler from
// context.WithTimeout.
func (p *Provider) buildRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("incus: marshal body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	fullURL := p.baseURL + apiVersion
	// Only append the path separator + path when there IS a sub-path. A bare
	// "GET /1.0" is what Ping sends (path=""); constructing "/1.0/" instead
	// makes real Incus return 404 (its router treats the trailing-slash root
	// as unknown). The in-process fake doesn't enforce this, so the bug only
	// shows against a real daemon.
	if path != "" {
		fullURL += "/" + strings.TrimLeft(path, "/")
	}
	// Validate the URL — belt-and-braces against malformed config.
	if _, err := url.Parse(fullURL); err != nil {
		return nil, fmt.Errorf("incus: invalid url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("incus: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// doAsync issues a request that returns an async operation and waits for it
// to complete using the operations API. path is the resource path; method is
// typically POST or PUT. Returns the final operation state.
//
// The Incus REST API returns status 202 with an "operation" URL on async
// requests; the client polls WaitOperation on that URL.
func (p *Provider) doAsync(ctx context.Context, method, path string, body any) (*Operation, error) {
	raw, err := p.do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	// p.do returns the metadata (the response envelope's Metadata field).
	// For async operations the metadata is the operation itself.
	var op Operation
	if err := json.Unmarshal(raw, &op); err == nil && op.ID != "" {
		return p.WaitOperation(ctx, op.ID)
	}
	// Some Incus responses wrap the operation differently — try as a
	// raw operation reference string.
	var opRef string
	if err := json.Unmarshal(raw, &opRef); err == nil && opRef != "" {
		opID := opIDFromURL(opRef)
		if opID == "" {
			opID = opRef
		}
		return p.WaitOperation(ctx, opID)
	}
	return nil, fmt.Errorf("incus: async %s %s: could not decode operation from response: %s",
		method, path, truncate(string(raw), 256))
}

// opIDFromURL extracts the trailing UUID from an operation URL like
// "/1.0/operations/<uuid>".
func opIDFromURL(s string) string {
	idx := strings.LastIndex(s, "/")
	if idx < 0 {
		return ""
	}
	return s[idx+1:]
}

// truncate is a small helper for capping error-message size so we never embed
// a multi-MB Incus response in a wrapped error.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
