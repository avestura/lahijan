// Package stripe: errors.go defines the sentinel errors every area file
// returns and the classifier for the Stripe REST error envelope.
//
// Stripe's HTTP API uses conventional HTTP status codes plus a small
// JSON envelope `{"error": {"type": ..., "code": ..., "message": ...}}`
// on errors. We translate each non-2xx response into one of the
// sentinels below so callers can errors.Is at the boundary.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Sentinel errors. Use errors.Is at boundaries to translate to HTTP.
var (
	// ErrDisabled is returned when the driver was not built
	// (billing.stripe.enabled = false). The consuming module translates
	// this into a 501 "feature disabled" envelope.
	ErrDisabled = errors.New("stripe: provider disabled")

	// ErrUnreachable is returned by Ping and the request layer when the
	// Stripe API endpoint does not respond. Distinguished from a normal
	// REST error so the health-probe UI can render it as "backend down".
	ErrUnreachable = errors.New("stripe: server unreachable")

	// ErrNotFound is returned when the Stripe API responds with 404.
	ErrNotFound = errors.New("stripe: not found")

	// ErrAlreadyExists is returned when the Stripe API responds with 400
	// and a "resource_already_exists" code.
	ErrAlreadyExists = errors.New("stripe: already exists")

	// ErrConflict is returned when the Stripe API responds with 409.
	ErrConflict = errors.New("stripe: conflict")

	// ErrForbidden is returned when the Stripe API responds with 403.
	// Usually indicates insufficient permissions for the API key.
	ErrForbidden = errors.New("stripe: forbidden")

	// ErrBadRequest is returned when the Stripe API responds with 400.
	ErrBadRequest = errors.New("stripe: bad request")

	// ErrUnauthenticated is returned when the Stripe API responds with
	// 401. Stripe emits this when the API key is missing or invalid.
	ErrUnauthenticated = errors.New("stripe: unauthenticated")

	// ErrRateLimited is returned when the Stripe API responds with 429.
	// The caller SHOULD back off; Stripe honours Retry-After.
	ErrRateLimited = errors.New("stripe: rate limited")

	// ErrOperationFailed is the catch-all for a non-2xx response whose
	// status code is not in the sentinel set above. The wrapped message
	// carries the Stripe error string for diagnostics.
	ErrOperationFailed = errors.New("stripe: operation failed")

	// ErrInvalidSignature is returned by VerifyWebhook when the
	// Stripe-Signature header is missing, malformed, or fails the
	// HMAC-SHA256 check.
	ErrInvalidSignature = errors.New("stripe: webhook signature invalid")
)

// classify maps a non-2xx *http.Response into one of the sentinels
// above. The caller has already read the body via io.LimitReader; the
// body bytes are passed for decoding into an *APIError.
func classify(resp *http.Response, body []byte) error {
	// Decode the envelope: Stripe returns `{"error": {...}}`.
	type envelope struct {
		Error *APIError `json:"error"`
	}
	var env envelope
	if len(body) > 0 {
		// Best-effort decode; an unparseable body falls through to
		// the status-code-only path below.
		_ = json.Unmarshal(body, &env)
	}

	// Build the APIError. If the body had no envelope we synthesise a
	// minimal one from the status code so the sentinel Is() check still
	// works.
	apiErr := env.Error
	if apiErr == nil {
		apiErr = &APIError{StatusCode: resp.StatusCode}
	} else {
		apiErr.StatusCode = resp.StatusCode
	}

	switch resp.StatusCode {
	case http.StatusBadRequest:
		if apiErr.Code == "resource_already_exists" {
			return fmt.Errorf("%w: %s (%w)", ErrAlreadyExists, apiErr.Message, apiErr)
		}
		return fmt.Errorf("%w: %s (%w)", ErrBadRequest, apiErr.Message, apiErr)
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s (%w)", ErrUnauthenticated, apiErr.Message, apiErr)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s (%w)", ErrForbidden, apiErr.Message, apiErr)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s (%w)", ErrNotFound, apiErr.Message, apiErr)
	case http.StatusConflict:
		return fmt.Errorf("%w: %s (%w)", ErrConflict, apiErr.Message, apiErr)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s (%w)", ErrRateLimited, apiErr.Message, apiErr)
	}
	return fmt.Errorf("%w: %s (%w)", ErrOperationFailed, apiErr.Error(), apiErr)
}

// readResponse reads up to maxResponseBytes from r. Returns the raw
// bytes (caller decodes); on read error wraps with context so the
// caller can distinguish cancellation from network failure.
func readResponse(ctx context.Context, r io.ReadCloser) ([]byte, error) {
	defer func() { _ = r.Close() }()
	raw, err := io.ReadAll(io.LimitReader(r, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("stripe: read response: %w", err)
	}
	return raw, nil
}
