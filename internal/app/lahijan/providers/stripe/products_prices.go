// Package stripe: products_prices.go implements the Stripe Product +
// Price surface Lahijan uses for subscription plans.
//
// Each billing_plan row maps to one Stripe Product + one Stripe Price.
// The admin "push to Stripe" action creates the upstream Product +
// Price and stores the ids on the billing_plan row. The user
// subscription creates a Subscription referencing the Price id.
package stripe

import (
	"context"
	"net/url"
)

// CreateProduct creates a Stripe Product. idempotencyKey is the
// caller-supplied Stripe Idempotency-Key header value.
func (p *Provider) CreateProduct(
	ctx context.Context,
	req CreateProductRequest,
	idempotencyKey string,
) (Product, error) {
	ctx, span := startSpan(ctx, "products.create")
	defer span.End()
	form := url.Values{}
	form.Set("name", req.Name)
	if req.Description != "" {
		form.Set("description", req.Description)
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", toString(v))
	}
	var out Product
	if err := p.do(ctx, "POST", "/v1/products", form, idempotencyKey, &out); err != nil {
		setStatus(span, err)
		return Product{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// CreatePrice creates a Stripe Price (recurring unit price for a
// Product). idempotencyKey is the caller-supplied Stripe
// Idempotency-Key header value.
func (p *Provider) CreatePrice(
	ctx context.Context,
	req CreatePriceRequest,
	idempotencyKey string,
) (Price, error) {
	ctx, span := startSpan(ctx, "prices.create")
	defer span.End()
	form := url.Values{}
	form.Set("currency", req.Currency)
	form.Set("unit_amount", itoa(int(req.UnitAmount)))
	form.Set("product", req.Product)
	form.Set("type", "recurring")
	if req.Interval == "" {
		req.Interval = "month"
	}
	form.Set("recurring[interval]", req.Interval)
	if req.IntervalCount <= 0 {
		req.IntervalCount = 1
	}
	form.Set("recurring[interval_count]", itoa(req.IntervalCount))
	form.Set("recurring[usage_type]", "licensed")
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", toString(v))
	}
	var out Price
	if err := p.do(ctx, "POST", "/v1/prices", form, idempotencyKey, &out); err != nil {
		setStatus(span, err)
		return Price{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// GetPrice retrieves a Price by id. Used by the admin debug page + by
// the plan-push reconcile.
func (p *Provider) GetPrice(ctx context.Context, id string) (Price, error) {
	ctx, span := startSpan(ctx, "prices.get")
	defer span.End()
	var out Price
	if err := p.do(ctx, "GET", "/v1/prices/"+id, nil, "", &out); err != nil {
		setStatus(span, err)
		return Price{}, err
	}
	setStatus(span, nil)
	return out, nil
}
