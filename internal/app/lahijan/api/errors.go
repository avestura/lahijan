// Package api wires Lahijan's HTTP surface: it registers the OpenAPI-derived
// routes, owns the standard error envelope, and assembles the middleware
// stack in the canonical order.
//
// Handlers here are thin (parse -> call service -> render); business logic
// lives under internal/app/lahijan/domain/<area>. See ADR-0015 and
// docs/architecture/conventions.md#http-api.
package api

import "github.com/gofiber/fiber/v2"

// Machine-readable error codes used across the API. These are stable, snake_case
// strings that clients can switch on. Keep them in sync with the examples in
// api/openapi.yaml.
const (
	CodeBadRequest      = "bad_request"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodePayloadTooLarge = "payload_too_large"
	CodeInternal        = "internal"
	CodeNotImplemented  = "not_implemented"
)

// ErrorEnvelope is the standard error response body. Every error response in
// the API is shaped exactly like {error: {code, message, details?}}.
//
// `details` is optional and intentionally free-form (see api/openapi.yaml).
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody is the inner object of the standard error envelope.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// SendError writes a standard error envelope response.
//
// status  is the HTTP status code to set (e.g. fiber.StatusBadRequest).
// code    is the machine-readable error code (use one of the Code* constants).
// message is a short, human-readable message that is safe to show end users.
// details is an optional map of structured details (e.g. per-field validation
//
//	errors). Pass nil when there are no details.
//
// Every error path in a handler should go through this helper so that the
// envelope shape is guaranteed uniform across the API.
func SendError(c *fiber.Ctx, status int, code, message string, details map[string]any) error {
	return c.Status(status).JSON(ErrorEnvelope{
		Error: ErrorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

// SendBadRequest is a convenience wrapper for a 400 bad_request response.
func SendBadRequest(c *fiber.Ctx, message string, details map[string]any) error {
	return SendError(c, fiber.StatusBadRequest, CodeBadRequest, message, details)
}

// SendUnauthorized is a convenience wrapper for a 401 unauthorized response.
func SendUnauthorized(c *fiber.Ctx, message string) error {
	return SendError(c, fiber.StatusUnauthorized, CodeUnauthorized, message, nil)
}

// SendForbidden is a convenience wrapper for a 403 forbidden response.
func SendForbidden(c *fiber.Ctx, message string) error {
	return SendError(c, fiber.StatusForbidden, CodeForbidden, message, nil)
}

// SendNotFound is a convenience wrapper for a 404 not_found response.
func SendNotFound(c *fiber.Ctx, message string) error {
	return SendError(c, fiber.StatusNotFound, CodeNotFound, message, nil)
}

// SendInternal is a convenience wrapper for a 500 internal response.
// Internal errors must never leak the underlying cause to the client; pass a
// generic message and log the real error server-side.
func SendInternal(c *fiber.Ctx, message string) error {
	return SendError(c, fiber.StatusInternalServerError, CodeInternal, message, nil)
}

// SendNotImplemented is a convenience wrapper for a 501 not_implemented response.
func SendNotImplemented(c *fiber.Ctx, message string) error {
	return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, message, nil)
}
