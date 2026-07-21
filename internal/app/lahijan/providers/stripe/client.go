// Package stripe: client.go is the single HTTP entry point every area
// file calls through. It owns the *http.Client, the per-request
// timeout, the user-agent, the Bearer Authorization header, the
// Stripe-Version header, and the OTel span bootstrap for every
// outbound call.
//
// Production wires the client via NewClient from program.Start based
// on conf.billing.stripe.*. Tests wire it against an httptest.Server
// (see fake/server.go).
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// userAgent is sent on every request so Stripe's logs can tell Lahijan
// driver traffic apart from other API clients.
const userAgent = "lahijan-stripe-driver/0.1"

// defaultAPIBaseURL is the production Stripe API origin.
const defaultAPIBaseURL = "https://api.stripe.com"

// defaultAPIVersion is the Stripe API version this driver pins. Stripe
// sends a `Stripe-Version` header on every request so the response
// shape stays stable across upstream minor releases. Bump this when
// adopting a new API version (a coordinated test pass is required).
const defaultAPIVersion = "2024-06-20"

// defaultRequestTimeout is used when Config.RequestTimeout is zero.
// Stripe API calls typically return in <1s; 30s is a generous cap that
// accommodates idempotent retries without leaving the caller hanging.
const defaultRequestTimeout = 30 * time.Second

// maxResponseBytes caps a single response body so a misbehaving
// Stripe (or a faulty proxy) cannot drive the driver OOM. 16 MiB is
// larger than any single object Lahijan reads (webhook events top out
// at a few KB).
const maxResponseBytes = 16 * 1024 * 1024

// Provider implements providers.Provider plus the Stripe-specific
// surface (customers, payment_intents, setup_intents, subscriptions,
// invoices, webhooks). All methods are safe for concurrent use: the
// struct holds only immutable fields after NewClient.
//
// Per pillar 1 the Name() string is "stripe" (internal-only); end
// users see "billing".
type Provider struct {
	httpClient   *http.Client
	baseURL      string // scheme + host, no trailing slash
	secretKey    string // SENSITIVE — never logged
	apiVersion   string
	timeout      time.Duration
	pingEndpoint string // overridable for the fake server
	whSecret     string // webhook signing secret (SENSITIVE); empty = disabled
}

// Capabilities is the per-Stripe feature-flag bundle returned by
// Capabilities(). Mirrors providers.Capabilities but kept as a value
// type so callers can copy it freely.
type Capabilities struct {
	// LiveMode is true when the API key is a live key (production
	// money); false for test keys. Stripe's /v1/charge-style objects
	// carry their own livemode flag; this is the cached value from
	// the first successful Ping.
	LiveMode bool

	// APIVersion is the Stripe API version the driver pins.
	APIVersion string

	// WebhooksEnabled is true when Config.WebhookSecret was supplied
	// at construction. The webhook handler is wired but VerifyWebhook
	// returns ErrInvalidSignature when no secret is configured.
	WebhooksEnabled bool
}

// Config is the bundle passed to NewClient by program.Start.
type Config struct {
	// HTTPClient is the configured *http.Client.
	HTTPClient *http.Client

	// BaseURL is the Stripe API origin. Defaults to
	// "https://api.stripe.com" when empty. Tests override to point at
	// an httptest server.
	BaseURL string

	// SecretKey is the Stripe API key ("sk_live_..." or "sk_test_...").
	// Required. SENSITIVE — never logged.
	SecretKey string

	// APIVersion pins the Stripe API version. Defaults to
	// defaultAPIVersion when empty.
	APIVersion string

	// WebhookSecret is the `whsec_...` secret used to verify webhook
	// signatures. Required when the webhook receiver is enabled; empty
	// otherwise (VerifyWebhook returns ErrInvalidSignature).
	WebhookSecret string

	// RequestTimeout is the per-call timeout. Zero means use the
	// default 30s.
	RequestTimeout time.Duration

	// PingEndpoint overrides the path NewClient probes during Ping.
	// Used by the fake server to skip the live /v1/products round-trip.
	// Empty means use the production endpoint.
	PingEndpoint string
}

// NewClient builds a Provider from the given Config. The returned
// provider has not yet contacted the Stripe API; the first call to
// Ping (or the first request) will. Returns an error if HTTPClient or
// SecretKey is missing.
func NewClient(cfg Config) (*Provider, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("stripe: Config.HTTPClient is required")
	}
	if cfg.SecretKey == "" {
		return nil, errors.New("stripe: Config.SecretKey is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	ping := cfg.PingEndpoint
	if ping == "" {
		// GET /v1/products?limit=1 is the cheapest authenticated probe.
		ping = "/v1/products?limit=1"
	}
	return &Provider{
		httpClient:   cfg.HTTPClient,
		baseURL:      strings.TrimRight(baseURL, "/"),
		secretKey:    cfg.SecretKey,
		apiVersion:   apiVersion,
		timeout:      timeout,
		pingEndpoint: ping,
		whSecret:     cfg.WebhookSecret,
	}, nil
}

// WebhookSecret returns the configured webhook secret. Exposed so the
// webhook handler in api/ can pass it to VerifyWebhook without
// re-reading config. The returned string is SENSITIVE — never log it.
// Returns "" when no webhook secret was configured; callers should
// treat that as "webhooks are disabled" and return 501 from the
// receiver endpoint.
func (p *Provider) WebhookSecret() string { return p.whSecret }

// do issues a request against the Stripe API and decodes the JSON
// response into out. path is the /v1/... path with optional query
// string. body is the form-encoded request payload (nil for GET/DELETE).
//
// Every call opens an OTel span named "stripe.http.<method>" so the
// upstream area-specific spans have a child HTTP-level span.
func (p *Provider) do(ctx context.Context, method, path string, body url.Values, idempotencyKey string, out any) error {
	ctx, span := startSpan(ctx, "http."+strings.ToLower(method),
		attribute.String("stripe.path", path))
	defer span.End()

	// Apply the per-request timeout.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	req, err := p.buildRequest(ctx, method, path, body, idempotencyKey)
	if err != nil {
		setStatus(span, err)
		return err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return fmt.Errorf("stripe: http %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := readResponse(ctx, resp.Body)
		classifyErr := classify(resp, raw)
		setStatus(span, classifyErr)
		return classifyErr
	}

	raw, err := readResponse(ctx, resp.Body)
	if err != nil {
		setStatus(span, err)
		return err
	}

	if out == nil {
		setStatus(span, nil)
		return nil
	}
	// Stripe returns JSON for every 2xx with a body; empty 2xx (rare
	// for our surface) is a no-op.
	if len(raw) == 0 {
		setStatus(span, nil)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		err = fmt.Errorf("stripe: decode response %s %s: %w", method, path, err)
		setStatus(span, err)
		return err
	}
	setStatus(span, nil)
	return nil
}

// buildRequest constructs the *http.Request with the standard headers.
// Stripe's REST API is form-encoded (NOT JSON) for write endpoints —
// `application/x-www-form-urlencoded` body with nested params via `[`
// and `]` (e.g. `metadata[tenant_id]=...`). The body url.Values is
// encoded by the stdlib which handles the nesting correctly when the
// caller uses the `metadata[tenant_id]` keys directly.
func (p *Provider) buildRequest(ctx context.Context, method, path string, body url.Values, idempotencyKey string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(body.Encode())
	}
	fullURL := p.baseURL + "/" + strings.TrimLeft(path, "/")
	// Validate the URL — belt-and-braces against malformed config.
	if _, err := url.Parse(fullURL); err != nil {
		return nil, fmt.Errorf("stripe: invalid url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("stripe: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.secretKey)
	req.Header.Set("Stripe-Version", p.apiVersion)
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req, nil
}

// wrapCtxErr maps a context-cancellation error to a sentinel-ish
// wrapping that callers can errors.Is against context.Canceled /
// context.DeadlineExceeded.
func wrapCtxErr(err error) error {
	if err == nil {
		return nil
	}
	// http.Client returns *url.Error wrapping the underlying error;
	// unwrap once so the caller's errors.Is(ctx.Canceled) works.
	if uerr := errors.Unwrap(err); uerr != nil {
		return uerr
	}
	return err
}
