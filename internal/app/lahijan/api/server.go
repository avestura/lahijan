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
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/saml"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
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

	// WS-07a: external identity-provider deps. Any of these may be nil when
	// the corresponding feature is disabled in config; the handlers degrade
	// gracefully (returning a localised "feature disabled" envelope).
	idpSvc          *idp.Service
	idpOAuth        *oauth.Registry
	idpOIDC         *oidc.Registry
	stateSigner     *state.Signer
	idpCookies      ExternalIDPCookies
	idpRedirectHome string

	// WS-07b: SAML deps. idpSAML is the *saml.Registry (nil when SAML is
	// disabled). The idp service gains a SAML repo automatically when
	// repos.SamlIdentities is non-nil; the api handlers short-circuit via
	// idpSAML==nil when SAML is disabled.
	idpSAML *saml.Registry

	// WS-07c: MFA deps. mfaSvc is nil-appropriate when MFA is not wired
	// (dev without auth.mfa.webauthn config); the handlers degrade to a
	// 501 "feature disabled" envelope via mfaDisabled().
	mfaSvc *mfa.Service

	// WS-09: admin jobs deps. jobs is the River client the admin jobs API
	// talks to (list/get/retry/cancel). Nil-appropriate when the job
	// subsystem is disabled (dev/test without River wired); the handlers
	// degrade to a 501 "feature disabled" envelope.
	jobs JobsClient
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

	// WS-07a: external IdP deps. Nil-appropriate when the feature is disabled.
	IDPSvc          *idp.Service
	IDPOAuth        *oauth.Registry
	IDPOIDC         *oidc.Registry
	StateSigner     *state.Signer
	IDPCookies      ExternalIDPCookies
	IDPRedirectHome string

	// WS-07b: SAML deps. IDPSAML is the *saml.Registry (nil-appropriate when
	// SAML is disabled in config).
	IDPSAML *saml.Registry

	// WS-07c: MFA deps. MFASvc is nil-appropriate when MFA is not wired
	// (dev without auth.mfa.webauthn config); the handlers degrade to a
	// 501 "feature disabled" envelope.
	MFASvc *mfa.Service

	// WS-09: admin jobs deps. Jobs is the River client (or any JobsClient
	// fake) the admin jobs API talks to. Nil-appropriate when the job
	// subsystem is disabled; the handlers degrade to a 501 envelope.
	Jobs JobsClient
}

// NewServer builds the API server with the given dependencies.
func NewServer(deps ServerDeps) *Server {
	s := &Server{
		tracer:          deps.Tracer,
		users:           deps.Users,
		sessions:        deps.Sessions,
		sessionSvc:      deps.SessionSvc,
		patSvc:          deps.PATSvc,
		emailSvc:        deps.EmailSvc,
		signer:          deps.Signer,
		cookies:         deps.Cookies,
		audit:           deps.Audit,
		auditEmitter:    deps.AuditEmitter,
		idpSvc:          deps.IDPSvc,
		idpOAuth:        deps.IDPOAuth,
		idpOIDC:         deps.IDPOIDC,
		stateSigner:     deps.StateSigner,
		idpCookies:      deps.IDPCookies,
		idpRedirectHome: deps.IDPRedirectHome,
		idpSAML:         deps.IDPSAML,
		mfaSvc:          deps.MFASvc,
		jobs:            deps.Jobs,
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
	if s.idpCookies.State == "" && (deps.IDPSvc != nil || deps.IDPOAuth != nil || deps.IDPOIDC != nil) {
		// Default cookie names when the IdP feature is on but the caller did
		// not override the names.
		s.idpCookies = DefaultExternalIDPCookies
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
