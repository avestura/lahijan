// Package stripe: payment_intents.go implements the Stripe PaymentIntent
// surface Lahijan uses (create + retrieve).
//
// A PaymentIntent is the synchronous "collect this amount from this
// user" primitive. Lahijan creates one when the user clicks "Top up
// $X"; the SPA confirms the intent via Stripe.js using the returned
// client secret. The webhook handler reacts to
// `payment_intent.succeeded` to post the ledger credit.
package stripe

import (
	"context"
	"net/url"
)

// CreatePaymentIntent creates a PaymentIntent. idempotencyKey is the
// caller-supplied Stripe Idempotency-Key header value; reuse the same
// value across retries of the same logical create to deduplicate.
func (p *Provider) CreatePaymentIntent(
	ctx context.Context,
	req CreatePaymentIntentRequest,
	idempotencyKey string,
) (PaymentIntent, error) {
	ctx, span := startSpan(ctx, "payment_intents.create")
	defer span.End()
	form := url.Values{}
	form.Set("amount", itoa(int(req.Amount)))
	form.Set("currency", req.Currency)
	if req.Customer != "" {
		form.Set("customer", req.Customer)
	}
	if req.PaymentMethod != "" {
		form.Set("payment_method", req.PaymentMethod)
	}
	if req.Description != "" {
		form.Set("description", req.Description)
	}
	if req.ReceiptEmail != "" {
		form.Set("receipt_email", req.ReceiptEmail)
	}
	if req.StatementDescriptorSuffix != "" {
		form.Set("statement_descriptor_suffix", req.StatementDescriptorSuffix)
	}
	// Off-session payments use automatic_payment_methods=false +
	// payment_method already set; on-session (SPA-confirmable)
	// payments use the default. Lahijan always goes through the SPA,
	// so we leave automatic_payment_methods to Stripe's default
	// (which is "enabled" as of the 2024 API version).
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", toString(v))
	}
	var out PaymentIntent
	if err := p.do(ctx, "POST", "/v1/payment_intents", form, idempotencyKey, &out); err != nil {
		setStatus(span, err)
		return PaymentIntent{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// GetPaymentIntent retrieves a PaymentIntent by id. Used by the
// reconcile job + admin ops.
func (p *Provider) GetPaymentIntent(ctx context.Context, id string) (PaymentIntent, error) {
	ctx, span := startSpan(ctx, "payment_intents.get")
	defer span.End()
	var out PaymentIntent
	if err := p.do(ctx, "GET", "/v1/payment_intents/"+id, nil, "", &out); err != nil {
		setStatus(span, err)
		return PaymentIntent{}, err
	}
	setStatus(span, nil)
	return out, nil
}
