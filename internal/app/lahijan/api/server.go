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
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/avestura/lahijan/internal/app/lahijan/version"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/marketplace"
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

	// WS-10a: admin plugin deps. pluginsRepo is the persistence boundary
	// (every read/write to the plugins + plugin_permissions tables);
	// pluginSvc is the install/lifecycle service the write paths go
	// through (so audit emission happens in one place). Both are
	// nil-appropriate when the WASM subsystem is disabled
	// (conf.wasm.enabled=false); the handlers degrade to 501.
	pluginsRepo *database.PluginsRepository
	pluginSvc   *installer.Service

	// WS-10c: marketplace deps. marketplaceSvc is the entrypoint the
	// admin marketplace API talks to; it wraps the installer + the
	// configured marketplace index + asset loaders. Nil-appropriate when
	// the WASM subsystem is disabled; the handlers degrade to 501.
	marketplaceSvc *marketplace.Service

	// WS-14: compute module deps. computeSvc is the entrypoint every
	// /api/v1/compute/* handler talks to; it wraps the Incus provider +
	// the compute_* repositories + the audit emitter + the WASM event
	// bus. Nil-appropriate when the Incus provider is disabled; the
	// handlers degrade to 501.
	computeSvc *compute.Service

	// WS-15: DNS module deps. dnsSvc is the entrypoint every
	// /api/v1/dns/* handler talks to; it wraps the PowerDNS provider +
	// the dns_zones + dns_records repositories + the audit emitter +
	// the WASM event bus. Nil-appropriate when the PowerDNS provider is
	// disabled; the handlers degrade to 501.
	dnsSvc *dns.Service

	// WS-16: object storage module deps. StorageSvc is the entrypoint
	// every /api/v1/storage/* handler talks to; it wraps the SeaweedFS
	// provider + the storage_buckets + storage_credentials repositories
	// + the audit emitter + the WASM event bus. Nil-appropriate when
	// the SeaweedFS provider is disabled; the handlers degrade to 501.
	storageSvc *storage.Service

	// WS-17: billing & metering module deps. BillingSvc is the
	// entrypoint every /api/v1/me/{balance,usage,ledger,receipts}*
	// + /api/v1/admin/billing/* + /api/v1/admin/users/{id}/{topup,
	// refund,ledger,balance}* handler talks to; it wraps the price
	// catalog + the append-only ledger + the per-user balance cache
	// + the usage stream + the receipt PDF generator + the audit
	// emitter + the WASM event bus. Nil-appropriate when the billing
	// subsystem is disabled; the handlers degrade to 501.
	billingSvc *billing.Service

	// WS-27: payment gateway deps. PaymentsSvc is the entrypoint every
	// /api/v1/billing/* + /api/v1/admin/billing/{plans,promo-codes,
	// webhook-events}* + /api/v1/webhooks/stripe handler talks to; it
	// wraps the Stripe provider + the payment_methods / subscriptions
	// / plans / promo_codes / webhook_events repos + the WS-17 billing
	// service (for ledger writes). Nil-appropriate when the Stripe
	// gateway is disabled; the handlers degrade to 501.
	paymentsSvc *billing.PaymentsService
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

	// WS-10a: admin plugin deps. PluginsRepo is required for every plugin
	// endpoint; PluginSvc is required for the state-changing endpoints.
	// Both nil-appropriate when the WASM subsystem is disabled; the
	// handlers degrade to a 501 envelope.
	PluginsRepo *database.PluginsRepository
	PluginSvc   *installer.Service

	// WS-10c: marketplace deps. MarketplaceSvc is the entrypoint the
	// admin marketplace API talks to. Nil-appropriate when the WASM
	// subsystem is disabled; the handlers degrade to 501 envelope.
	MarketplaceSvc *marketplace.Service

	// WS-14: compute module deps. ComputeSvc is the entrypoint every
	// /api/v1/compute/* handler talks to. Nil-appropriate when the Incus
	// provider is disabled; the handlers degrade to 501.
	ComputeSvc *compute.Service

	// WS-15: DNS module deps. DNSSvc is the entrypoint every
	// /api/v1/dns/* handler talks to. Nil-appropriate when the PowerDNS
	// provider is disabled; the handlers degrade to 501.
	DNSSvc *dns.Service

	// WS-16: object storage module deps. StorageSvc is the entrypoint
	// every /api/v1/storage/* handler talks to. Nil-appropriate when
	// the SeaweedFS provider is disabled; the handlers degrade to 501.
	StorageSvc *storage.Service

	// WS-17: billing & metering module deps. BillingSvc is the
	// entrypoint every billing handler talks to. Nil-appropriate when
	// the billing subsystem is disabled; the handlers degrade to 501.
	BillingSvc *billing.Service

	// WS-27: payment gateway deps. PaymentsSvc is the entrypoint every
	// payment handler talks to. Nil-appropriate when the Stripe gateway
	// is disabled; the handlers degrade to 501.
	PaymentsSvc *billing.PaymentsService
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
		pluginsRepo:     deps.PluginsRepo,
		pluginSvc:       deps.PluginSvc,
		marketplaceSvc:  deps.MarketplaceSvc,
		computeSvc:      deps.ComputeSvc,
		dnsSvc:          deps.DNSSvc,
		storageSvc:      deps.StorageSvc,
		billingSvc:      deps.BillingSvc,
		paymentsSvc:     deps.PaymentsSvc,
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

// SetDNSService swaps the DNS service after the server has been built.
// Used by integration tests that wire a real (httptest-backed) DNS
// service after the standard newTestApp path has run. The handler
// closures capture s.dnsSvc at request time, so the swap takes effect
// immediately for every subsequent request without re-registering
// routes. Production code passes DNSSvc via ServerDeps at construction.
func (s *Server) SetDNSService(svc *dns.Service) {
	s.dnsSvc = svc
}

// SetComputeService mirrors SetDNSService for the compute module.
func (s *Server) SetComputeService(svc *compute.Service) {
	s.computeSvc = svc
}

// SetStorageService mirrors SetDNSService for the storage module.
// Used by integration tests that wire a real (fake-server-backed) storage
// service after the standard newTestApp path has run. Production code
// passes StorageSvc via ServerDeps at construction.
func (s *Server) SetStorageService(svc *storage.Service) {
	s.storageSvc = svc
}

// SetBillingService mirrors SetStorageService for the billing module.
// Used by integration tests that wire a real (DB-backed) billing service
// after the standard newTestApp path has run. Production code passes
// BillingSvc via ServerDeps at construction.
func (s *Server) SetBillingService(svc *billing.Service) {
	s.billingSvc = svc
}

// SetPaymentsService mirrors SetBillingService for the WS-27 payment
// gateway. Used by integration tests that wire a real (httptest-backed)
// payments service after the standard newTestApp path has run.
// Production code passes PaymentsSvc via ServerDeps at construction.
func (s *Server) SetPaymentsService(svc *billing.PaymentsService) {
	s.paymentsSvc = svc
}

// PaymentsService returns the wired payments service (or nil when the
// Stripe gateway is disabled). Exported so integration tests can drive
// the service layer directly.
func (s *Server) PaymentsService() *billing.PaymentsService { return s.paymentsSvc }

// BillingService returns the wired billing service (or nil when the
// billing subsystem is disabled). Exported so integration tests can
// drive the service layer directly when the test setup is shared with
// the HTTP layer.
func (s *Server) BillingService() *billing.Service { return s.billingSvc }

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
