// Package stripe: subscriptions.go implements the Stripe Subscription
// surface Lahijan uses (create + retrieve + cancel).
//
// A Subscription links a Customer to a recurring Price; Stripe bills
// the customer automatically and emits `invoice.paid` webhook events
// that the Lahijan webhook handler turns into ledger credits.
package stripe

import (
	"context"
	"net/url"
)

// CreateSubscription creates a Subscription. idempotencyKey is the
// caller-supplied Stripe Idempotency-Key header value.
func (p *Provider) CreateSubscription(
	ctx context.Context,
	req CreateSubscriptionRequest,
	idempotencyKey string,
) (Subscription, error) {
	ctx, span := startSpan(ctx, "subscriptions.create")
	defer span.End()
	form := url.Values{}
	form.Set("customer", req.Customer)
	// Subscription item: price + quantity=1.
	form.Set("items[0][price]", req.Price)
	form.Set("items[0][quantity]", "1")
	if req.DefaultPaymentMethod != "" {
		form.Set("default_payment_method", req.DefaultPaymentMethod)
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", toString(v))
	}
	var out Subscription
	if err := p.do(ctx, "POST", "/v1/subscriptions", form, idempotencyKey, &out); err != nil {
		setStatus(span, err)
		return Subscription{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// GetSubscription retrieves a Subscription by id. Used by the webhook
// handler + admin ops.
func (p *Provider) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	ctx, span := startSpan(ctx, "subscriptions.get")
	defer span.End()
	var out Subscription
	if err := p.do(ctx, "GET", "/v1/subscriptions/"+id, nil, "", &out); err != nil {
		setStatus(span, err)
		return Subscription{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// CancelSubscription cancels a Subscription. When cancelAtPeriodEnd is
// true the subscription stays active until the end of the current
// period; false cancels immediately.
func (p *Provider) CancelSubscription(
	ctx context.Context,
	id string,
	cancelAtPeriodEnd bool,
) (Subscription, error) {
	ctx, span := startSpan(ctx, "subscriptions.cancel")
	defer span.End()
	form := url.Values{}
	form.Set("cancel_at_period_end", toString(cancelAtPeriodEnd))
	var out Subscription
	if err := p.do(ctx, "DELETE", "/v1/subscriptions/"+id, form, "", &out); err != nil {
		setStatus(span, err)
		return Subscription{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// ListSubscriptionsForCustomer lists the active subscriptions for the
// given customer.
func (p *Provider) ListSubscriptionsForCustomer(
	ctx context.Context,
	customerID string,
	limit int,
) ([]Subscription, error) {
	ctx, span := startSpan(ctx, "subscriptions.list")
	defer span.End()
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("limit", itoa(limit))
	form.Set("status", "all")
	var list struct {
		Object  string        `json:"object"`
		Data    []Subscription `json:"data"`
		HasMore bool          `json:"has_more"`
	}
	if err := p.do(ctx, "GET", "/v1/subscriptions?"+form.Encode(), nil, "", &list); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return list.Data, nil
}
