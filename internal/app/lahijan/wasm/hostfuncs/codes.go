// Package hostfuncs: codes.go holds the status-code constants every host
// function returns. Per ADR-0024 the ABI is "host functions return a
// single i32 status code"; these constants are the canonical values so
// plugin authors can rely on the numbers being stable across versions.
//
// Range:
//
//	0           — success (generic)
//	positive    — host-specific success-with-length (e.g. bytes written)
//	-1          — generic failure (validation, IO, unknown)
//	-2          — permission denied (the enforcer rejected the call)
//	-3          — feature unavailable (the host module was wired with
//	              nil deps; the call shape is correct but the backend
//	              is disabled in this process)
//	-4          — invalid memory (ptr+len pair does not fit in the
//	              caller's linear memory)
//	-5          — invalid argument (string too long, missing NUL, ...)
//	-6          — not found (kv_get on a missing key)
//	-7          — buffer too small (return value does not fit; caller
//	              must allocate a larger buffer and retry)
//	-8          — upstream error (HTTP 5xx, provider down, ...)
//	<= -100     — reserved for per-function error codes (documented on
//	              the function)
package hostfuncs

const (
	// StatusSuccess is the generic success return.
	StatusSuccess int32 = 0

	// StatusGenericFailure is the catch-all for unexpected errors. The
	// host logs the underlying error; the plugin only sees the code.
	StatusGenericFailure int32 = -1

	// StatusDenied is the permission-denied return. Host functions
	// return this when the enforcer rejects the call (or when the
	// plugin_id cannot be resolved).
	StatusDenied int32 = -2

	// StatusUnavailable is the "feature disabled in this process"
	// return. Plugin authors should treat this as "the operator turned
	// this off; do not retry".
	StatusUnavailable int32 = -3

	// StatusInvalidMemory is the "(ptr, len) outside memory" return.
	// Almost always a plugin-side bug; the plugin should abort its
	// logic rather than retry.
	StatusInvalidMemory int32 = -4

	// StatusInvalidArgument is the "the call shape is wrong" return
	// (string too long, missing terminator, ...).
	StatusInvalidArgument int32 = -5

	// StatusNotFound is the "row does not exist" return. kv_get uses
	// this to distinguish "missing key" from "internal error".
	StatusNotFound int32 = -6

	// StatusBufferTooSmall is the "return value does not fit" return.
	// Host functions that write into a caller-supplied buffer return
	// this when the buffer is smaller than the value; the caller must
	// allocate a larger buffer and retry.
	StatusBufferTooSmall int32 = -7

	// StatusUpstreamError is the "external service failed" return
	// (HTTP 5xx, provider unreachable, ...).
	StatusUpstreamError int32 = -8
)

// Explain returns a human-readable label for a status code. Used in
// logs and the OTel span attributes so a dashboard reader can see at a
// glance which codes are most common.
func Explain(code int32) string {
	switch code {
	case StatusSuccess:
		return "success"
	case StatusGenericFailure:
		return "generic_failure"
	case StatusDenied:
		return "denied"
	case StatusUnavailable:
		return "unavailable"
	case StatusInvalidMemory:
		return "invalid_memory"
	case StatusInvalidArgument:
		return "invalid_argument"
	case StatusNotFound:
		return "not_found"
	case StatusBufferTooSmall:
		return "buffer_too_small"
	case StatusUpstreamError:
		return "upstream_error"
	default:
		if code > 0 {
			return "ok_with_length"
		}
		return "unknown"
	}
}
