// Package registrar: errors.go holds the sentinel errors the registrar
// service surfaces. Handlers translate them to HTTP envelopes via
// api/errors.go.
package registrar

import "errors"

// ErrProviderDisabled is returned when no registrar driver was built.
// The handler maps it to 501 not_implemented.
var ErrProviderDisabled = errors.New("registrar: provider is not enabled on this server")

// ErrDomainNotFound is returned when the dns_domains row does not exist
// within the caller's tenant. The handler maps it to 404 not_found.
var ErrDomainNotFound = errors.New("registrar: domain not found")

// ErrDomainUnavailable is returned when the registrar reports the
// domain is taken. The handler maps it to 409 conflict.
var ErrDomainUnavailable = errors.New("registrar: domain unavailable")

// ErrInvalidDomain is returned when the domain name is not a valid
// canonical name (lowercase, no whitespace, no trailing dot).
var ErrInvalidDomain = errors.New("registrar: domain name must be a lowercase DNS name without whitespace or a trailing dot")

// ErrInvalidPeriod is returned when the period_years is outside the
// allowed range [1, 10].
var ErrInvalidPeriod = errors.New("registrar: period_years must be in [1, 10]")

// ErrInvalidContact is returned when the contact profile is missing a
// required field.
var ErrInvalidContact = errors.New("registrar: contact profile is incomplete")

// ErrAuthCodeRequired is returned when the caller did not supply the
// EPP auth code on a transfer.
var ErrAuthCodeRequired = errors.New("registrar: EPP auth code is required for transfer")

// ErrNoPricing is returned when the registrar did not return a pricing
// entry matching the requested period.
var ErrNoPricing = errors.New("registrar: no pricing available for the requested period")

// ErrBillingRequired is returned when the service was not wired with a
// billing hook. Paid endpoints (register / renew / transfer) cannot
// function without it.
var ErrBillingRequired = errors.New("registrar: billing hook is required for paid operations")
