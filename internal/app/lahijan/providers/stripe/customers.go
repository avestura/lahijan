// Package stripe: customers.go implements the Stripe Customer surface
// Lahijan uses (create + retrieve by email + retrieve by id).
//
// The platform creates one Customer per (tenant, user) pair on first
// payment-method attach. The Stripe-side Customer id is stored in the
// billing_payment_methods table AES-GCM-encrypted (per ADR-0034);
// metadata carries the tenant_id + user_id so the webhook handler
// can resolve them from the event payload.
package stripe

import (
	"context"
	"net/url"
)

// CreateCustomer creates a Stripe Customer. idempotencyKey is the
// caller-supplied Stripe Idempotency-Key header value; reuse the same
// value across retries of the same logical create to deduplicate.
func (p *Provider) CreateCustomer(ctx context.Context, req CreateCustomerRequest, idempotencyKey string) (Customer, error) {
	ctx, span := startSpan(ctx, "customers.create")
	defer span.End()
	form := url.Values{}
	if req.Email != "" {
		form.Set("email", req.Email)
	}
	if req.Name != "" {
		form.Set("name", req.Name)
	}
	if req.Description != "" {
		form.Set("description", req.Description)
	}
	for k, v := range req.Metadata {
		form.Set("metadata["+k+"]", toString(v))
	}
	var out Customer
	if err := p.do(ctx, "POST", "/v1/customers", form, idempotencyKey, &out); err != nil {
		setStatus(span, err)
		return Customer{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// GetCustomer retrieves a Customer by id.
func (p *Provider) GetCustomer(ctx context.Context, id string) (Customer, error) {
	ctx, span := startSpan(ctx, "customers.get")
	defer span.End()
	var out Customer
	if err := p.do(ctx, "GET", "/v1/customers/"+id, nil, "", &out); err != nil {
		setStatus(span, err)
		return Customer{}, err
	}
	setStatus(span, nil)
	return out, nil
}

// FindCustomerByEmail lists customers matching the supplied email.
// Returns the first match (Stripe allows multiple customers with the
// same email; Lahijan treats (tenant, user, email) as unique so the
// first match is the right one). Returns ErrNotFound when no match.
func (p *Provider) FindCustomerByEmail(ctx context.Context, email string) (Customer, error) {
	ctx, span := startSpan(ctx, "customers.find_by_email")
	defer span.End()
	form := url.Values{}
	form.Set("email", email)
	form.Set("limit", "1")
	var list struct {
		Object string    `json:"object"`
		Data   []Customer `json:"data"`
		HasMore bool     `json:"has_more"`
	}
	if err := p.do(ctx, "GET", "/v1/customers?"+form.Encode(), nil, "", &list); err != nil {
		setStatus(span, err)
		return Customer{}, err
	}
	if len(list.Data) == 0 {
		err := ErrNotFound
		setStatus(span, err)
		return Customer{}, err
	}
	setStatus(span, nil)
	return list.Data[0], nil
}

// SetDefaultPaymentMethodForCustomer sets the customer's default
// invoice-payment PaymentMethod. Used by the attach + the
// "make default" paths.
func (p *Provider) SetDefaultPaymentMethodForCustomer(
	ctx context.Context,
	customerID, paymentMethodID string,
) error {
	ctx, span := startSpan(ctx, "customers.set_default_pm")
	defer span.End()
	form := url.Values{}
	form.Set("invoice_settings[default_payment_method]", paymentMethodID)
	if err := p.do(ctx, "POST", "/v1/customers/"+customerID, form, "", nil); err != nil {
		setStatus(span, err)
		return err
	}
	setStatus(span, nil)
	return nil
}

// toString is the minimal any->string helper for metadata values.
// Supports string, int, int64, bool; everything else is fmt.SPrinted.
func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return itoa(t)
	case int64:
		return itoa(int(t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return fmtSPrint(v)
}
