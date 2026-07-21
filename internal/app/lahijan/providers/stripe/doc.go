// Package stripe is the thin internal HTTP client over the Stripe REST
// API (WS-27, ADR-0034). The package implements providers.Provider for
// consistency with the Incus / PowerDNS / SeaweedFS drivers but is
// **opt-in**: unconfigured, it stays nil and every payment endpoint
// degrades to 501, exactly like the other drivers do when their config
// is absent.
//
// Layering:
//
//	api/billing_payments_handlers.go -> billing.PaymentsService -> providers/stripe
//	                                                            \-> database (payment_methods,
//	                                                            \    subscriptions, plans,
//	                                                            \    webhook_events)
//	                                                            \-> billing.Service (ledger posts)
//
// Per ADR-0034 the package:
//   - talks HTTPS to https://api.stripe.com/v1/* via a standard
//     net/http.Client (the base URL is configurable so tests point at
//     httptest).
//   - sends the secret key in the Authorization: Bearer header on every
//     request.
//   - sends Stripe-Version + Idempotency-Key headers per the Stripe
//     API contract.
//   - verifies webhook signatures (HMAC-SHA256 over the raw body +
//     the `t=...,v1=...` Stripe-Signature header format).
//   - imports only stdlib + go.opentelemetry.io/otel — zero new
//     top-level deps.
//
// Per pillar 1 the package is named "stripe" (internal-only); end
// users see "billing" / "payment method" / "subscription".
package stripe
