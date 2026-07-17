// Package api: validate.go provides request-body validation against the
// OpenAPI spec.
//
// The canonical oapi-codegen request validator middlewares target chi/echo/gin.
// Fiber sits on fasthttp, so converting a *fiber.Ctx into the net/http request
// shape that kin-openapi's openapi3filter expects is awkward and lossy. We
// therefore expose a focused, handler-driven validator: after a handler
// decodes a JSON body, it calls ValidateBody with the resolved schema to get a
// precise, envelope-friendly error. The router wires the spec so handlers can
// look up their operation's schema.
//
// For the WS-05 surface (only GET endpoints, no request bodies) this validator
// is exercised by tests with a synthetic body; it activates for real as soon
// as the first POST/PUT lands (WS-06 onwards).
package api

import (
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
)

// ValidateBody validates a decoded JSON body against an OpenAPI 3 schema. It
// returns a descriptive error in the form "body: <reason>" when validation
// fails; nil when the body conforms. The error is safe to wrap into a
// bad_request envelope detail.
//
// Pass the request-body media-type's schema (typically
// op.RequestBody.Value.Content["application/json"].Schema.Value).
func ValidateBody(schema *openapi3.Schema, body any) error {
	if schema == nil {
		return nil
	}
	if err := schema.VisitJSON(body); err != nil {
		return fmt.Errorf("body: %w", err)
	}
	return nil
}
