// Package billing: payments_errors.go adds the new sentinel errors
// the WS-27 payment gateway surface returns. Kept in a separate file
// from errors.go so the diff is obvious + future WS additions land
// cleanly.
package billing

import "errors"

// ErrStripeDisabled is returned when the Stripe provider is not wired.
// The handler maps it to 501 not_implemented.
var ErrStripeDisabled = errors.New("billing: stripe gateway is not enabled")

// ErrInvalidPaymentMethod is returned when the caller passes an empty
// PaymentMethod id (or one that fails the upstream lookup).
var ErrInvalidPaymentMethod = errors.New("billing: payment method id is required")

// ErrPaymentMethodNotFound is returned when the requested
// payment_method row does not exist within the caller's tenant (or
// does not belong to the caller). The handler maps it to 404.
var ErrPaymentMethodNotFound = errors.New("billing: payment method not found")

// ErrPlanNotFound is returned when the requested plan row does not
// exist within the caller's tenant. The handler maps it to 404.
var ErrPlanNotFound = errors.New("billing: plan not found")

// ErrPromoCodeNotFound is returned when the requested promo code does
// not exist within the caller's tenant. The handler maps it to 404.
var ErrPromoCodeNotFound = errors.New("billing: promo code not found")

// ErrPromoCodeExhausted is returned when the promo code is revoked,
// expired, or has reached its max_uses. The handler maps it to 409.
var ErrPromoCodeExhausted = errors.New("billing: promo code is exhausted, revoked, or expired")

// ErrSubscriptionNotFound is returned when the requested subscription
// does not exist within the caller's tenant. The handler maps it to 404.
var ErrSubscriptionNotFound = errors.New("billing: subscription not found")

// ErrWebhookSignatureInvalid is returned when the Stripe-Signature
// header is missing, malformed, or fails the HMAC-SHA256 check. The
// handler maps it to 400.
var ErrWebhookSignatureInvalid = errors.New("billing: webhook signature invalid")
