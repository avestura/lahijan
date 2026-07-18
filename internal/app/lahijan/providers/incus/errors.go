// Package incus: errors.go defines the sentinel errors every area file returns
// and the decoder for the Incus REST error envelope.
package incus

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
	// ErrDisabled is returned when the driver was not built (config has
	// providers.incus.enabled = false). The consuming module translates this
	// into a 501 "feature disabled" envelope.
	ErrDisabled = errors.New("incus: provider disabled")

	// ErrUnreachable is returned by Ping and the request layer when the
	// daemon socket/HTTPS endpoint does not respond. Distinguished from a
	// normal REST error so the health-probe UI can render it as "backend
	// down" rather than "API error".
	ErrUnreachable = errors.New("incus: daemon unreachable")

	// ErrNotFound is returned when the Incus REST API responds with 404.
	// Services translate to a 404 envelope.
	ErrNotFound = errors.New("incus: not found")

	// ErrAlreadyExists is returned when the Incus REST API responds with
	// 409 and the error message matches "already exists".
	ErrAlreadyExists = errors.New("incus: already exists")

	// ErrConflict is returned when the Incus REST API responds with 409
	// for any reason other than "already exists" (e.g. instance is running
	// and the operation requires it stopped).
	ErrConflict = errors.New("incus: conflict")

	// ErrForbidden is returned when the Incus REST API responds with 403.
	// Usually indicates a project-restriction violation.
	ErrForbidden = errors.New("incus: forbidden")

	// ErrBadRequest is returned when the Incus REST API responds with 400.
	ErrBadRequest = errors.New("incus: bad request")

	// ErrOperationFailed is the catch-all for a non-2xx response whose
	// status code is not in the sentinel set above. The wrapped message
	// carries the Incus error string for diagnostics.
	ErrOperationFailed = errors.New("incus: operation failed")
)

// APIError carries an Incus REST error response. The Incus REST API returns
// errors as `{"type": "error", "error": "...", "error_code": 400}`; we decode
// the envelope here and wrap it as a Go error so callers can errors.As it.
type APIError struct {
	StatusCode int
	Code       int
	Message    string
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return "incus: <nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("incus: http %d", e.StatusCode)
	}
	return fmt.Sprintf("incus: http %d: %s", e.StatusCode, e.Message)
}

// Is allows errors.Is(err, ErrNotFound) to match an *APIError with status 404.
// We do not blanket-map every status to a sentinel here; classify() does the
// mapping explicitly so callers see the right sentinel for each status.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrAlreadyExists:
		return e.StatusCode == http.StatusConflict && strings.Contains(e.Message, "already exists")
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrOperationFailed:
		return e.StatusCode >= 400 && e.StatusCode < 500 &&
			e.StatusCode != http.StatusNotFound &&
			e.StatusCode != http.StatusConflict &&
			e.StatusCode != http.StatusForbidden &&
			e.StatusCode != http.StatusBadRequest
	default:
		return false
	}
}

// Unwrap returns the sentinel matching this APIError so errors.Is can match.
// We return ErrOperationFailed as the base; specific sentinels are matched via
// the Is method above.
func (e *APIError) Unwrap() error { return ErrOperationFailed }

// classify inspects resp for an Incus error envelope. On a 2xx response it
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
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, apiErr.Message)
	case http.StatusConflict:
		if strings.Contains(apiErr.Message, "already exists") {
			return fmt.Errorf("%w: %s", ErrAlreadyExists, apiErr.Message)
		}
		return fmt.Errorf("%w: %s", ErrConflict, apiErr.Message)
	default:
		return apiErr
	}
}

// decodeAPIError parses the Incus error envelope {"type":"error","error":"...",
// "error_code":NNN} into an *APIError. A malformed body still yields a useful
// APIError carrying the HTTP status code.
func decodeAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode, Code: statusCode}
	if len(body) == 0 {
		return apiErr
	}
	// Cheap extraction — Incus uses a stable, tiny envelope so we avoid a
	// json.Unmarshal + struct here.
	// Look for "error":"..." (string field).
	if msg := extractJSONString(body, "error"); msg != "" {
		apiErr.Message = msg
	}
	if code := extractJSONInt(body, "error_code"); code > 0 {
		apiErr.Code = code
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

// extractJSONInt returns the int value at the given top-level key, or 0 if not
// found / unparsable. Used only for error envelopes.
func extractJSONInt(body []byte, key string) int {
	needle := []byte(`"` + key + `":`)
	idx := indexOfBytes(body, needle)
	if idx < 0 {
		return 0
	}
	start := idx + len(needle)
	end := start
	for end < len(body) && body[end] >= '0' && body[end] <= '9' {
		end++
	}
	if end == start {
		return 0
	}
	n := 0
	for _, b := range body[start:end] {
		n = n*10 + int(b-'0')
	}
	return n
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
		return fmt.Errorf("incus: cancelled: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("incus: deadline: %w", err)
	default:
		return err
	}
}
