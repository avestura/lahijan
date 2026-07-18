// Package powerdns: errors.go defines the sentinel errors every area file
// returns and the decoder for the PowerDNS REST error envelope.
//
// PowerDNS' HTTP API uses conventional HTTP status codes plus a small JSON
// envelope `{"error": "..."}` on errors. We translate each non-2xx response
// into one of the sentinels below so callers can errors.Is at the boundary.
package powerdns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Sentinel errors. Use errors.Is at boundaries to translate to HTTP.
var (
	// ErrDisabled is returned when the driver was not built
	// (providers.powerdns.enabled = false). The consuming module translates
	// this into a 501 "feature disabled" envelope.
	ErrDisabled = errors.New("powerdns: provider disabled")

	// ErrUnreachable is returned by Ping and the request layer when the
	// PDNS HTTP endpoint does not respond. Distinguished from a normal REST
	// error so the health-probe UI can render it as "backend down" rather
	// than "API error".
	ErrUnreachable = errors.New("powerdns: server unreachable")

	// ErrNotFound is returned when the PDNS REST API responds with 404.
	// Services translate to a 404 envelope.
	ErrNotFound = errors.New("powerdns: not found")

	// ErrAlreadyExists is returned when the PDNS rest API responds with
	// 409 and the error message matches "already exists".
	ErrAlreadyExists = errors.New("powerdns: already exists")

	// ErrConflict is returned when the PDNS REST API responds with 409 for
	// any reason other than "already exists" (e.g. zone is locked).
	ErrConflict = errors.New("powerdns: conflict")

	// ErrForbidden is returned when the PDNS REST API responds with 403.
	// Usually indicates a bad API key or insufficient daemon permissions.
	ErrForbidden = errors.New("powerdns: forbidden")

	// ErrBadRequest is returned when the PDNS REST API responds with 400.
	// Typically a malformed RRset, unsupported record type, or invalid
	// canonical zone name.
	ErrBadRequest = errors.New("powerdns: bad request")

	// ErrUnauthenticated is returned when the PDNS REST API responds with
	// 401. PDNS emits this when no API key was supplied.
	ErrUnauthenticated = errors.New("powerdns: unauthenticated")

	// ErrOperationFailed is the catch-all for a non-2xx response whose
	// status code is not in the sentinel set above. The wrapped message
	// carries the PDNS error string for diagnostics.
	ErrOperationFailed = errors.New("powerdns: operation failed")
)

// APIError carries a PDNS REST error response. The PDNS REST API returns
// errors as a small JSON object `{"error": "..."}` plus an HTTP status code;
// we decode the envelope here and wrap it as a Go error so callers can
// errors.As it.
type APIError struct {
	// StatusCode is the HTTP status code returned by PDNS.
	StatusCode int

	// Message is the human-readable error string from the JSON body.
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return "powerdns: <nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("powerdns: http %d", e.StatusCode)
	}
	return fmt.Sprintf("powerdns: http %d: %s", e.StatusCode, e.Message)
}

// Is allows errors.Is(err, ErrNotFound) to match an *APIError with status 404.
// We do not blanket-map every status to a sentinel here; classify() does the
// mapping explicitly so callers see the right sentinel for each status.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrAlreadyExists:
		return e.StatusCode == http.StatusConflict && strings.Contains(e.Message, "exists")
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrUnauthenticated:
		return e.StatusCode == http.StatusUnauthorized
	case ErrOperationFailed:
		return e.StatusCode >= 400 && e.StatusCode < 500 &&
			e.StatusCode != http.StatusNotFound &&
			e.StatusCode != http.StatusConflict &&
			e.StatusCode != http.StatusForbidden &&
			e.StatusCode != http.StatusBadRequest &&
			e.StatusCode != http.StatusUnauthorized
	default:
		return false
	}
}

// Unwrap returns the sentinel matching this APIError so errors.Is can match.
// We return ErrOperationFailed as the base; specific sentinels are matched
// via the Is method above.
func (e *APIError) Unwrap() error { return ErrOperationFailed }

// classify inspects resp for a PDNS error envelope. On a 2xx response it
// returns nil WITHOUT touching the body — the caller still needs to read it.
// On a non-2xx response it consumes + closes the body and returns a sentinel
// (or *APIError) wrapped error.
func classify(resp *http.Response) error {
	if resp == nil {
		return ErrOperationFailed
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// Error path: consume the body so the connection can be reused.
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	apiErr := decodeAPIError(resp.StatusCode, body)
	switch apiErr.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, apiErr.Message)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, apiErr.Message)
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthenticated, apiErr.Message)
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, apiErr.Message)
	case http.StatusConflict:
		if strings.Contains(strings.ToLower(apiErr.Message), "exists") {
			return fmt.Errorf("%w: %s", ErrAlreadyExists, apiErr.Message)
		}
		return fmt.Errorf("%w: %s", ErrConflict, apiErr.Message)
	default:
		return apiErr
	}
}

// decodeAPIError parses the PDNS error envelope {"error":"..."} into an
// *APIError. A malformed body still yields a useful APIError carrying the
// HTTP status code.
func decodeAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}
	if len(body) == 0 {
		return apiErr
	}
	// Cheap extraction — PDNS uses a tiny envelope so we avoid a full
	// json.Unmarshal + struct here. Handles simple ASCII payloads; complex
	// escaping flows through json.Unmarshal at the higher API layer.
	if msg := extractJSONString(body, "error"); msg != "" {
		apiErr.Message = msg
	}
	return apiErr
}

// extractJSONString returns the string value at the given top-level key in a
// small JSON object, or "" if not found. Used only for error envelopes.
// Handles simple ASCII payloads; complex escaping flows through json.Unmarshal
// at the higher API layer.
func extractJSONString(body []byte, key string) string {
	needle := []byte(`"` + key + `":"`)
	idx := indexOfBytes(body, needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := start
	for end < len(body) {
		switch body[end] {
		case '\\':
			end += 2 // skip escaped char
			continue
		case '"':
			return string(body[start:end])
		default:
			end++
		}
	}
	return ""
}

func indexOfBytes(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// wrapCtxErr converts a context error (cancelled / deadline) into a stable
// wrapped form so callers can errors.Is(err, context.Canceled).
func wrapCtxErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("powerdns: cancelled: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("powerdns: deadline: %w", err)
	default:
		return err
	}
}
