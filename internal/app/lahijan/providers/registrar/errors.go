// Package registrar: errors.go defines the sentinel errors every
// registrar driver returns + the decoder for typical registrar REST
// error envelopes.
//
// Registrars vary widely in their error envelopes (OpenSRS uses a JSON
// body with a "response" wrapper; ResellerClub uses a flat JSON object
// with "status" = "error"). The sentinels below are the common
// denominator every driver maps into; the per-driver decoder handles
// the envelope shape.
package registrar

import (
	"context"
	"errors"
	"fmt"
)

// Sentinel errors. Use errors.Is at boundaries to translate to HTTP.
var (
	// ErrDisabled is returned when no registrar driver was built
	// (providers.registrar.enabled = false). The consuming module
	// translates this into a 501 "feature disabled" envelope.
	ErrDisabled = errors.New("registrar: provider disabled")

	// ErrUnreachable is returned by Ping and the request layer when the
	// registrar HTTP endpoint does not respond.
	ErrUnreachable = errors.New("registrar: server unreachable")

	// ErrNotFound is returned when the registrar REST API responds with
	// 404 (typically "order not found").
	ErrNotFound = errors.New("registrar: not found")

	// ErrAlreadyExists is returned when a register / transfer call
	// targets a domain the caller already owns.
	ErrAlreadyExists = errors.New("registrar: already exists")

	// ErrConflict is returned for any other 409 (e.g. domain is locked
	// for transfer).
	ErrConflict = errors.New("registrar: conflict")

	// ErrForbidden is returned when the registrar responds 403.
	// Typically a bad API key.
	ErrForbidden = errors.New("registrar: forbidden")

	// ErrBadRequest is returned when the registrar responds 400.
	// Typically a malformed contact profile or unsupported TLD.
	ErrBadRequest = errors.New("registrar: bad request")

	// ErrUnauthenticated is returned when the registrar responds 401.
	ErrUnauthenticated = errors.New("registrar: unauthenticated")

	// ErrUnavailable is returned by CheckDomain when the domain is
	// taken by someone else. Distinguished from a hard error so the
	// service layer can return a search result.
	ErrUnavailable = errors.New("registrar: domain unavailable")

	// ErrInsufficientFunds is returned when the registrar account does
	// not have enough balance to cover the order. The service layer
	// translates this into a 402 payment_required envelope.
	ErrInsufficientFunds = errors.New("registrar: insufficient funds")

	// ErrOperationFailed is the catch-all for a non-2xx response whose
	// status code is not in the sentinel set above.
	ErrOperationFailed = errors.New("registrar: operation failed")
)

// APIError carries a registrar REST error response. Every driver maps
// its per-registrar error envelope into this shape so the consuming
// service does not need to know the per-registrar shape.
type APIError struct {
	// StatusCode is the HTTP status code returned by the registrar.
	StatusCode int

	// Code is the registrar-specific error code (OpenSRS' "response.code",
	// ResellerClub's "status" + "ecode", ...). Empty when the registrar
	// did not provide one.
	Code string

	// Message is the human-readable error string from the JSON body.
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return "registrar: <nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("registrar: http %d", e.StatusCode)
	}
	return fmt.Sprintf("registrar: http %d: %s", e.StatusCode, e.Message)
}

// Is allows errors.Is(err, ErrNotFound) to match an *APIError.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == 404
	case ErrAlreadyExists:
		return e.StatusCode == 409 && containsLower(e.Message, "exists")
	case ErrConflict:
		return e.StatusCode == 409
	case ErrForbidden:
		return e.StatusCode == 403
	case ErrBadRequest:
		return e.StatusCode == 400
	case ErrUnauthenticated:
		return e.StatusCode == 401
	case ErrInsufficientFunds:
		// OpenSRS reports insufficient funds as 400 + code "485".
		return e.Code == "485" || containsLower(e.Message, "insufficient")
	case ErrUnavailable:
		return e.Code == "UNAVAILABLE" || containsLower(e.Message, "unavailable")
	case ErrOperationFailed:
		return e.StatusCode >= 400 && e.StatusCode < 500
	default:
		return false
	}
}

// containsLower reports whether the lower-cased message contains substr.
// Tiny helper so we do not pull strings into every error path.
func containsLower(msg, substr string) bool {
	if len(msg) < len(substr) {
		return false
	}
	for i := 0; i+len(substr) <= len(msg); i++ {
		if equalFoldLower(msg[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

// equalFoldLower reports whether s1 equals s2 under ASCII case folding,
// assuming s2 is already lowercase. Tiny helper used by containsLower.
func equalFoldLower(s1, s2 string) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i := 0; i < len(s1); i++ {
		c1 := s1[i]
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c1 != s2[i] {
			return false
		}
	}
	return true
}

// wrapCtxErr converts a context error (cancelled / deadline) into a
// stable wrapped form so callers can errors.Is(err, context.Canceled).
func wrapCtxErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("registrar: cancelled: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("registrar: deadline: %w", err)
	default:
		return err
	}
}
