// Package api: server.go implements the OpenAPI-derived Fiber server interface.
//
// The handlers here are intentionally thin. They implement the
// apigen.ServerInterface generated from api/openapi.yaml by oapi-codegen, so
// the request/response types can never drift from the spec (ADR-0015). All
// errors go through the standard envelope (errors.go).
package api

import (
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/version"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/trace"
)

// Server implements apigen.ServerInterface. Add per-area handler fields here as
// later WSs land real services; for WS-05 the handlers are self-contained.
//
// The tracer is injected (defaulting to the package-level global-resolved one)
// so tests can drive the handlers with a dedicated, isolated tracer provider
// instead of mutating process-global OTel state.
type Server struct {
	tracer trace.Tracer
}

// NewServer builds the API server with the default (global) tracer.
func NewServer() *Server { return &Server{tracer: Tracer()} }

// NewServerWithTracer builds an API server whose handlers use the given tracer.
// Intended for tests; production code uses NewServer.
func NewServerWithTracer(t trace.Tracer) *Server {
	return &Server{tracer: t}
}

// Ping handles GET /api/v1/ping. It returns the current server timestamp and
// emits an OpenTelemetry trace span to prove the api pipeline is wired.
func (s *Server) Ping(c *fiber.Ctx) error {
	_, span := s.tracer.Start(c.UserContext(), "ping")
	defer span.End()

	pong := apigen.Pong{Pong: time.Now().UTC()}
	return c.JSON(pong)
}

// GetHealth handles GET /health. It reports coarse service health and the
// running build version. Intended for orchestrators; not a privileged route.
func (s *Server) GetHealth(c *fiber.Ctx) error {
	return c.JSON(apigen.Health{
		Status:  apigen.HealthStatusOk,
		Version: version.LahijanVersion,
	})
}

// GetCurrentUser handles GET /api/v1/me. It is a SEED for the future auth
// module (WS-06): until authentication exists it returns a deterministic
// placeholder so the client-generation pipeline is exercised end to end.
func (s *Server) GetCurrentUser(c *fiber.Ctx) error {
	return SendNotImplemented(c, "authentication is not implemented yet; see WS-06")
}
