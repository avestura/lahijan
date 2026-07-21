// Package stripe: setup_intents.go implements the Stripe SetupIntent
// surface Lahijan uses (create).
//
// A SetupIntent is the "save this card for later" primitive. Lahijan
// creates one when the user adds a card via the billing dashboard
// without an immediate top-up. The SPA confirms the SetupIntent via
// Stripe.js using the returned client secret.
package stripe

import (
	"context"
	"net/url"
)

// CreateSetupIntent creates a SetupIntent. idempotencyKey is the
// caller-supplied Stripe Idempotency-Key header value; reuse the same
// value across retries of the same logical create to deduplicate.
func (p *Provider) CreateSetupIntent(
	ctx context.Context,
	req CreateSetupIntentRequest,
	idempotencyKey string,
) (SetupIntent, error) {
	ctx, span := startSpan(ctx, "setup_intents.create")
	defer span.End()
	form := url.Values{}
	if req.Customer != "" {
		form.Set("customer", req.Customer)
	}
	if req.PaymentMethod != "" {
		form.Set("payment_method", req.PaymentMethod)
		// Off-session flow: confirm immediately so the SetupIntent
		// completes synchronously when the pm is already known.
		form.Set("confirm", "true")
	}
	if req.Description != "" {
		form.Set("description", req.Description)
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", toString(v))
	}
	var out SetupIntent
	if err := p.do(ctx, "POST", "/v1/setup_intents", form, idempotencyKey, &out); err != nil {
		setStatus(span, err)
		return SetupIntent{}, err
	}
	setStatus(span, nil)
	return out, nil
}
