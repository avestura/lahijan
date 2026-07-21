// Package stripe: payment_methods.go implements the Stripe PaymentMethod
// surface Lahijan uses (retrieve + attach + detach + list for customer).
//
// PaymentMethods of type "card" are attached to a Customer; the card
// data itself is hosted by Stripe (the SPA confirms a SetupIntent via
// Stripe.js which produces a pm_* id; Lahijan only ever sees that id).
package stripe

import (
	"context"
	"net/url"
)

// GetPaymentMethod retrieves a PaymentMethod by id.
func (p *Provider) GetPaymentMethod(ctx context.Context, id string) (PaymentMethod, error) {
	ctx, span := startSpan(ctx, "payment_methods.get")
	defer span.End()
	var out PaymentMethod
	if err := p.do(ctx, "GET", "/v1/payment_methods/"+id, nil, "", &out); err != nil {
		setStatus(span, err)
		return PaymentMethod{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// AttachPaymentMethod attaches a PaymentMethod to a Customer. After
// attach the PaymentMethod can be charged via PaymentIntents or used
// as the customer's default for subscriptions.
func (p *Provider) AttachPaymentMethod(
	ctx context.Context,
	paymentMethodID, customerID string,
) (PaymentMethod, error) {
	ctx, span := startSpan(ctx, "payment_methods.attach")
	defer span.End()
	form := url.Values{}
	form.Set("customer", customerID)
	var out PaymentMethod
	if err := p.do(ctx, "POST", "/v1/payment_methods/"+paymentMethodID+"/attach", form, "", &out); err != nil {
		setStatus(span, err)
		return PaymentMethod{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// DetachPaymentMethod detaches a PaymentMethod from its Customer. The
// PaymentMethod can no longer be charged; the Lahijan row is
// soft-deleted by the service layer after the detach lands.
func (p *Provider) DetachPaymentMethod(ctx context.Context, paymentMethodID string) error {
	ctx, span := startSpan(ctx, "payment_methods.detach")
	defer span.End()
	if err := p.do(ctx, "POST", "/v1/payment_methods/"+paymentMethodID+"/detach", url.Values{}, "", nil); err != nil {
		setStatus(span, err)
		return err
	}
	setStatus(span, nil)
	return nil
}

// ListPaymentMethodsForCustomer lists the active PaymentMethods of
// the given type attached to the customer. Returns the cards in
// newest-first order.
func (p *Provider) ListPaymentMethodsForCustomer(
	ctx context.Context,
	customerID, pmType string,
	limit int,
) ([]PaymentMethod, error) {
	ctx, span := startSpan(ctx, "payment_methods.list")
	defer span.End()
	if pmType == "" {
		pmType = "card"
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("type", pmType)
	form.Set("limit", itoa(limit))
	var list struct {
		Object  string          `json:"object"`
		Data    []PaymentMethod `json:"data"`
		HasMore bool            `json:"has_more"`
	}
	if err := p.do(ctx, "GET", "/v1/payment_methods?"+form.Encode(), nil, "", &list); err != nil {
		setStatus(span, err)
		return nil, err
	}
	setStatus(span, nil)
	return list.Data, nil
}
