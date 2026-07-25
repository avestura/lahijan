// Package status maps the Lahijan host-function status codes to idiomatic
// Go errors. Every host function returns a single i32 status code (per
// ADR-0024); this package translates those codes into errors.Is-able
// sentinel values so plugin authors use standard Go error handling.
//
// The code values mirror internal/app/lahijan/wasm/hostfuncs/codes.go.
package status

import "errors"

// Code is the raw i32 status code returned by every host function.
type Code int32

const (
	// Success is the generic success return (also: zero bytes written).
	Success Code = 0

	// GenericFailure is the catch-all for unexpected errors.
	GenericFailure Code = -1

	// Denied is the permission-denied return.
	Denied Code = -2

	// Unavailable is the "feature disabled in this process" return.
	Unavailable Code = -3

	// InvalidMemory is the "(ptr, len) outside memory" return.
	InvalidMemory Code = -4

	// InvalidArgument is the "call shape is wrong" return.
	InvalidArgument Code = -5

	// NotFound is the "row does not exist" return (kv_get, config_get).
	NotFound Code = -6

	// BufferTooSmall is the "return value does not fit" return.
	BufferTooSmall Code = -7

	// UpstreamError is the "external service failed" return.
	UpstreamError Code = -8
)

// Sentinel errors. These are the values plugin authors compare against via
// errors.Is. Each maps 1:1 to a Code constant above.
var (
	ErrGenericFailure   = errors.New("lahijan: generic failure")
	ErrPermissionDenied = errors.New("lahijan: permission denied")
	ErrUnavailable      = errors.New("lahijan: feature unavailable")
	ErrInvalidMemory    = errors.New("lahijan: invalid memory access")
	ErrInvalidArgument  = errors.New("lahijan: invalid argument")
	ErrNotFound         = errors.New("lahijan: not found")
	ErrBufferTooSmall   = errors.New("lahijan: buffer too small")
	ErrUpstream         = errors.New("lahijan: upstream error")
)

// FromCode translates a raw i32 status code into a Go error. Returns nil
// for success (code >= 0, including success-with-length where code > 0
// indicates bytes written).
func FromCode(code int32) error {
	switch Code(code) {
	case Success:
		return nil
	case GenericFailure:
		return ErrGenericFailure
	case Denied:
		return ErrPermissionDenied
	case Unavailable:
		return ErrUnavailable
	case InvalidMemory:
		return ErrInvalidMemory
	case InvalidArgument:
		return ErrInvalidArgument
	case NotFound:
		return ErrNotFound
	case BufferTooSmall:
		return ErrBufferTooSmall
	case UpstreamError:
		return ErrUpstream
	default:
		if code > 0 {
			return nil // success-with-length
		}
		return ErrGenericFailure
	}
}
