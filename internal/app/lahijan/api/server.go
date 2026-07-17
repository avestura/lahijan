// Package api: server.go implements the OpenAPI-derived Fiber server interface.
//
// The handlers here are intentionally thin: they decode the request, call the
// relevant auth service, set/clear cookies as needed, and render the response.
// All errors go through the standard envelope (errors.go). Per ADR-0015 the
// request/response types come from apigen so they cannot drift from the spec.
package api

import (
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/version"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.opentelemetry.io/otel/trace"
)

// LocalsUserID is the Fiber-Locals key under which the auth middleware stores
// the resolved user id (mirrors middleware.LocalsUserID to keep the api package
// self-contained for handler reads).
const LocalsUserID = middleware.LocalsUserID

// Server implements apigen.ServerInterface. The auth service deps are injected
// so tests can drive the handlers with fakes, and the tracer is injected (it
// defaults to the package-level global one) so a test can use a dedicated,
// isolated tracer provider instead of mutating process-global OTel state.
type Server struct {
	tracer     trace.Tracer
	users      *database.UsersRepository
	sessions   *database.SessionsRepository
	sessionSvc *session.Service
	patSvc     *pat.Service
	emailSvc   *email.Service
	signer     *secrets.Signer
	cookies    CookieConfig

	// WS-08: audit query API deps.
	audit        *database.AuditLogRepository
	auditEmitter audit.Emitter
}

// ServerDeps carries the dependencies NewServer requires. Wire it once from
// program.Start once the DB pool and auth services are built.
type ServerDeps struct {
	Tracer     trace.Tracer
	Users      *database.UsersRepository
	Sessions   *database.SessionsRepository
	SessionSvc *session.Service
	PATSvc     *pat.Service
	EmailSvc   *email.Service
	Signer     *secrets.Signer
	Cookies    CookieConfig

	// WS-08: the audit query API reads from AuditLogRepository and records
	// export events via Emitter. Both are required for the audit endpoints
	// to function; pass nil only in tests that don't exercise those routes.
	Audit        *database.AuditLogRepository
	AuditEmitter audit.Emitter
}

// NewServer builds the API server with the given dependencies.
func NewServer(deps ServerDeps) *Server {
	s := &Server{
		tracer:       deps.Tracer,
		users:        deps.Users,
		sessions:     deps.Sessions,
		sessionSvc:   deps.SessionSvc,
		patSvc:       deps.PATSvc,
		emailSvc:     deps.EmailSvc,
		signer:       deps.Signer,
		cookies:      deps.Cookies,
		audit:        deps.Audit,
		auditEmitter: deps.AuditEmitter,
	}
	if s.tracer == nil {
		s.tracer = Tracer()
	}
	if s.auditEmitter == nil {
		// Default to Noop so audit_handlers.go can call Emit without nil
		// guards in every code path. Tests that assert on audit rows pass a
		// capturing emitter instead.
		s.auditEmitter = audit.NoopEmitter{}
	}
	return s
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

// currentUserID reads the authenticated user id from the request scope (set by
// the Auth middleware). Returns (uuid.Nil, false) when no user is resolved.
func currentUserID(c *fiber.Ctx) (uuid.UUID, bool) {
	v := c.Locals(LocalsUserID)
	if v == nil {
		return uuid.Nil, false
	}
	switch t := v.(type) {
	case uuid.UUID:
		return t, t != uuid.Nil
	case *uuid.UUID:
		if t == nil {
			return uuid.Nil, false
		}
		return *t, *t != uuid.Nil
	}
	return uuid.Nil, false
}

// requireUser returns the current user id or sends a 401 envelope. Every
// authenticated handler calls this first.
func (s *Server) requireUser(c *fiber.Ctx) (uuid.UUID, bool) {
	uid, ok := currentUserID(c)
	if !ok {
		_ = SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
		return uuid.Nil, false
	}
	return uid, true
}

// toUserDTO converts a database user row to the OpenAPI User schema.
func toUserDTO(u database.User) apigen.User {
	out := apigen.User{
		Id:          u.ID,
		Email:       openapi_types.Email(u.Email),
		DisplayName: u.DisplayName,
	}
	return out
}
