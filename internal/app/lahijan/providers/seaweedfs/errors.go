// Package seaweedfs: errors.go defines the sentinel errors every area file
// returns and the decoder for the SeaweedFS Filer HTTP error envelope.
//
// The Filer HTTP API uses conventional HTTP status codes plus a small JSON
// envelope `{"error": "..."}` on errors. We translate each non-2xx response
// into one of the sentinels below so callers can errors.Is at the boundary.
// AWS SDK S3 errors (from the S3 data plane) are translated via the
// `aws/errors.go` helpers in the smithy-go library; this file is concerned
// only with the Filer-side error envelope.
package seaweedfs

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
	// (providers.seaweedfs.enabled = false). The consuming module
	// translates this into a 501 "feature disabled" envelope.
	ErrDisabled = errors.New("seaweedfs: provider disabled")

	// ErrUnreachable is returned by Ping and the request layer when the
	// SeaweedFS S3 or Filer HTTP endpoint does not respond. Distinguished
	// from a normal REST error so the health-probe UI can render it as
	// "backend down" rather than "API error".
	ErrUnreachable = errors.New("seaweedfs: server unreachable")

	// ErrNotFound is returned when the Filer or S3 API responds with 404.
	// Services translate to a 404 envelope.
	ErrNotFound = errors.New("seaweedfs: not found")

	// ErrAlreadyExists is returned when the S3 API responds with
	// BucketAlreadyExists or the Filer responds with 409 "exists".
	ErrAlreadyExists = errors.New("seaweedfs: already exists")

	// ErrConflict is returned when the Filer responds with 409 for any
	// reason other than "already exists".
	ErrConflict = errors.New("seaweedfs: conflict")

	// ErrForbidden is returned when the S3 or Filer API responds with 403.
	// Usually indicates a bad access key or insufficient credentials.
	ErrForbidden = errors.New("seaweedfs: forbidden")

	// ErrBadRequest is returned when the Filer API responds with 400.
	// Typically a malformed bucket name, an invalid access key, or a
	// quota payload that fails schema validation.
	ErrBadRequest = errors.New("seaweedfs: bad request")

	// ErrUnauthenticated is returned when the S3 or Filer API responds
	// with 401. SeaweedFS emits this when no credentials were supplied.
	ErrUnauthenticated = errors.New("seaweedfs: unauthenticated")

	// ErrQuotaExceeded is returned when the S3 API responds with 507
	// Insufficient Storage (SeaweedFS surfaces quota-exhausted this way)
	// or when the driver's quota pre-check rejects a write preemptively.
	ErrQuotaExceeded = errors.New("seaweedfs: quota exceeded")

	// ErrOperationFailed is the catch-all for a non-2xx Filer response
	// whose status code is not in the sentinel set above. The wrapped
	// message carries the Filer error string for diagnostics.
	ErrOperationFailed = errors.New("seaweedfs: operation failed")

	// ErrInvalidBucketName is returned when the caller-supplied bucket
	// name violates the Lahijan naming convention (ADR-0011) or the
	// S3-compatibility rules (3-63 chars, lowercase alphanumeric + '-',
	// must start/end alphanumeric).
	ErrInvalidBucketName = errors.New("seaweedfs: invalid bucket name")

	// ErrInvalidSlug is returned when the caller-supplied slug part of
	// the bucket name fails validation. Slug rules: 1-50 chars,
	// lowercase alphanumeric + '-', must start/end alphanumeric.
	ErrInvalidSlug = errors.New("seaweedfs: invalid bucket slug")
)

// APIError carries a SeaweedFS REST error response. The Filer REST API
// returns errors as a small JSON object `{"error": "..."}` plus an HTTP
// status code; we decode the envelope here and wrap it as a Go error so
// callers can errors.As it.
type APIError struct {
	// StatusCode is the HTTP status code returned by SeaweedFS.
	StatusCode int

	// Message is the human-readable error string from the JSON body.
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return "seaweedfs: <nil>"
	}
	if e.Message == "" {
		return fmt.Sprintf("seaweedfs: http %d", e.StatusCode)
	}
	return fmt.Sprintf("seaweedfs: http %d: %s", e.StatusCode, e.Message)
}

// Is allows errors.Is(err, ErrNotFound) to match an *APIError whose
// status code maps to that sentinel.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrAlreadyExists:
		return e.StatusCode == http.StatusConflict && strings.Contains(strings.ToLower(e.Message), "exists")
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrUnauthenticated:
		return e.StatusCode == http.StatusUnauthorized
	case ErrQuotaExceeded:
		return e.StatusCode == http.StatusInsufficientStorage
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
// Specific sentinels are matched via the Is method above; the default
// fallback is ErrOperationFailed.
func (e *APIError) Unwrap() error { return ErrOperationFailed }

// classifyFiler inspects resp for a SeaweedFS Filer error envelope. On a
// 2xx response it returns nil WITHOUT touching the body — the caller still
// needs to read it. On a non-2xx response it consumes + closes the body and
// returns a sentinel (or *APIError) wrapped error.
func classifyFiler(resp *http.Response) error {
	if resp == nil {
		return ErrOperationFailed
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
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
	case http.StatusInsufficientStorage:
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, apiErr.Message)
	default:
		return apiErr
	}
}

// decodeAPIError parses the Filer error envelope {"error":"..."} into an
// *APIError. A malformed body still yields a useful APIError carrying the
// HTTP status code.
func decodeAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}
	if len(body) == 0 {
		return apiErr
	}
	if msg := extractJSONString(body, "error"); msg != "" {
		apiErr.Message = msg
	}
	return apiErr
}

// extractJSONString returns the string value at the given top-level key in a
// small JSON object, or "" if not found. Used only for error envelopes so it
// handles simple ASCII payloads; complex escaping flows through json.Unmarshal
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
		return fmt.Errorf("seaweedfs: cancelled: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("seaweedfs: deadline: %w", err)
	default:
		return err
	}
}
