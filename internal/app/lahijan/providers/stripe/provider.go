// Package stripe: provider.go implements the providers.Provider
// interface (Name / Ping / Capabilities) for the Stripe driver.
package stripe

import (
	"context"
	"errors"
)

// Name returns "stripe" — the internal driver identifier. NEVER
// surfaces to end users (per pillar 1 they see "billing").
func (p *Provider) Name() string { return "stripe" }

// Ping probes the Stripe API. A nil return means the API is reachable
// and the configured key is valid. Used by program.Start to log
// backend health at startup and by the future /api/v1/healthz.
//
// Ping issues the cheapest authenticated GET Stripe offers
// (`/v1/products?limit=1`); a 200 means the key works.
//
// Error mapping:
//   - API-level error (bad key, rate limit, ...) — returned as-is so
//     the caller can errors.Is against the specific sentinel
//     (ErrUnauthenticated, ErrRateLimited, ...).
//   - Network-level error (timeout, refused, DNS) — wrapped with
//     ErrUnreachable so the health-probe UI can render "backend down".
func (p *Provider) Ping(ctx context.Context) error {
	ctx, span := startSpan(ctx, "ping")
	defer span.End()
	var list struct {
		Object string `json:"object"`
		Data   []any  `json:"data"`
		// HasMore is the only field we read; a 200 + HasMore=false
		// is enough to know the key works.
		HasMore bool   `json:"has_more"`
		URL     string `json:"url"`
	}
	if err := p.do(ctx, "GET", p.pingEndpoint, nil, "", &list); err != nil {
		setStatus(span, err)
		// If this is an API-level error (i.e. we reached Stripe but
		// the API rejected us) the transport is fine — return as-is so
		// the caller can errors.Is against the specific sentinel.
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			return err
		}
		// Otherwise it's a transport-level error — wrap as
		// ErrUnreachable so the health-probe UI renders "backend down".
		wrapped := errors.Join(ErrUnreachable, err)
		setStatus(span, wrapped)
		return wrapped
	}
	setStatus(span, nil)
	return nil
}

// Capabilities returns the cached Stripe feature flags. Unlike the
// PowerDNS / Incus drivers, the Stripe API has no per-account feature
// surface; the cached flags are derived from the config (live/test
// mode inferred from the key prefix + whether the webhook secret was
// supplied). Returns a zero Capabilities when the provider is nil
// (caller's responsibility to guard).
func (p *Provider) Capabilities() Capabilities {
	return Capabilities{
		APIVersion:     p.apiVersion,
		LiveMode:       isLiveKey(p.secretKey),
		WebhooksEnabled: p.webhookSecretSet(),
	}
}

// webhookSecretSet returns whether the webhook secret was configured
// at construction. The Provider holds the secret out-of-band so the
// api handler can pass it to VerifyWebhook without it ever being
// readable from outside the package.
func (p *Provider) webhookSecretSet() bool {
	return p.whSecret != ""
}

// isLiveKey returns true when key starts with the live prefix. Stripe
// live keys are "sk_live_" / "rk_live_"; test keys are "sk_test_" /
// "rk_test_". Any non-matching prefix is treated as test mode (the
// safest default — a misconfigured live key degrades to test rather
// than accidentally charging cards).
func isLiveKey(key string) bool {
	const livePrefix = "sk_live_"
	const liveRestricted = "rk_live_"
	if len(key) < len(livePrefix) {
		return false
	}
	return key[:len(livePrefix)] == livePrefix || key[:len(liveRestricted)] == liveRestricted
}
