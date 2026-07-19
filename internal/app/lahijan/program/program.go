// Package program is Lahijan's bootstrap layer: it sets up config, builds the
// GoFiber app, wires middleware, builds the auth subsystem (WS-06) and the
// RBAC/audit subsystem (WS-08), seeds the permission catalog, registers
// routes, and calls app.Listen. All other backend code is invoked from here;
// nothing should call into program/ from outside.
package program

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math/big"
	"net/netip"
	"strings"
	"time"

	_ "go.uber.org/automaxprocs"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/recovery"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/totp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/webauthn"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/saml"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/conf/computeddefault"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/jobs"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventservice"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/hostfuncs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/marketplace"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	wasmruntime "github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
	wasmworker "github.com/avestura/lahijan/internal/app/lahijan/wasm/worker"
	"github.com/gofiber/fiber/v2"
	fiberlog "github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/google/uuid"
	riverui "riverqueue.com/riverui"
)

func init() {
	computeddefault.RegisterIntDefault("http.server.concurrency", fiber.DefaultConcurrency)
	computeddefault.RegisterIntDefault("http.server.bodylimit", fiber.DefaultBodyLimit)
}

// Start builds and runs the Lahijan HTTP server. It returns when the server
// stops (cleanly or with error).
func Start() error {
	info, err := conf.SetupConfig()
	if err != nil {
		log.Fatalf("failed to setup config: %s", err.Error())
	}
	conf.InitConfigBindFlags()

	if conf.IsDebugMode() {
		fiberlog.SetLevel(fiberlog.LevelTrace)
		fiberlog.Debug("debug mode is enabled.")
	} else {
		fiberlog.SetLevel(fiberlog.LevelInfo)
	}

	if info.FoundConfigFile {
		fiberlog.Debug("config file was found.")
	} else {
		fiberlog.Debug("config file was not found.")
	}

	// Wire the auth subsystem (WS-06). The DB pool + auth services are built
	// before the Fiber app so handlers can close over them. If the signing key
	// is empty in a non-dev environment we fail fast: an unguessable cookie
	// signing key is a hard security requirement.
	signingKey := conf.GetAuthSigningKey()
	if signingKey == "" && !isDev(conf.GetEnvironment()) {
		log.Fatal("auth.signing.key must be set in any non-dev environment")
	}

	authDeps, cleanup, err := buildAuthDeps(context.Background())
	if err != nil {
		log.Fatalf("failed to build auth deps: %s", err.Error())
	}
	defer cleanup()

	// WS-07a: build the external-IdP stack (OAuth + OIDC + account-linking
	// service). Returns a zero-value idpDeps (all nil) when no provider is
	// enabled; the handlers degrade to "feature disabled" envelopes.
	idpDeps, err := buildIdpDeps(context.Background(), authDeps)
	if err != nil {
		log.Fatalf("failed to build idp deps: %s", err.Error())
	}

	// WS-07b: build the SAML SP stack (per-provider ServiceProvider instances
	// sharing one process-wide signing key + cert). Returns a zero-value
	// samlDeps (registry nil) when no SAML provider is enabled; the handlers
	// degrade to "feature disabled" envelopes.
	samlDeps, err := buildSamlDeps(context.Background(), authDeps, idpDeps.stateSigner)
	if err != nil {
		log.Fatalf("failed to build saml deps: %s", err.Error())
	}

	// WS-07c: build the MFA service (TOTP + WebAuthn + recovery). The
	// WebAuthn relying-party is built only when conf.auth.mfa.webauthn.rpId
	// is set; otherwise the handlers degrade to a 501 "feature disabled"
	// envelope. TOTP + recovery codes do not need RP config and work out
	// of the box.
	mfaDeps, err := buildMFADeps(context.Background(), authDeps)
	if err != nil {
		log.Fatalf("failed to build mfa deps: %s", err.Error())
	}

	// WS-09: build the durable job queue (River). Returns a zero-value
	// jobDeps (all nil) when conf.jobs.enabled is false; the api handlers
	// degrade to a 501 "feature disabled" envelope in that case. The
	// supervisor is started in the background after this block and is
	// stopped on shutdown via defer.
	jobDeps, err := buildJobDeps(context.Background(), authDeps.repos)
	if err != nil {
		log.Fatalf("failed to build job deps: %s", err.Error())
	}
	if jobDeps.supervisor != nil {
		// Start workers in the background so bootstrap is not blocked on
		// River's leadership election (which can take a few seconds).
		// context.Background() so the queue's lifetime is the process's
		// lifetime — River ties the client's lifetime to the Start
		// context, so a timeout here would stop the workers after the
		// timeout. The defer below drains via Stop on shutdown.
		if startErr := jobDeps.supervisor.Start(context.Background()); startErr != nil {
			log.Fatalf("failed to start job supervisor: %s", startErr.Error())
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(),
				time.Duration(conf.GetJobsSoftStopTimeoutSeconds())*time.Second)
			defer stopCancel()
			if stopErr := jobDeps.supervisor.Stop(stopCtx); stopErr != nil {
				fiberlog.Error("job supervisor stop: %s", stopErr.Error())
			}
		}()
	}

	// WS-10a: build the WASM plugin subsystem (wazero runtime + installer
	// service). Returns a zero-value wasmDeps when conf.wasm.enabled is
	// false; the api handlers degrade to a 501 envelope. The runtime is
	// closed at shutdown via defer.
	//
	// WS-10b: the runtime is wired with the host-functions registrar
	// (network.outbound / kv.* / events.* / job.schedule /
	// api.handler.register / config.read). The bus + River client are
	// shared with the rest of the process so plugin-issued events and
	// jobs land on the same durable backbone as platform events.
	wasmDeps, err := buildWasmDeps(context.Background(), authDeps, &jobDeps)
	if err != nil {
		log.Fatalf("failed to build wasm deps: %s", err.Error())
	}
	if wasmDeps.runtime != nil {
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			if closeErr := wasmDeps.runtime.Close(stopCtx); closeErr != nil {
				fiberlog.Error("wasm runtime close: %s", closeErr.Error())
			}
		}()
	}

	// WS-10b: register the wasm.plugin.invoke River worker when both
	// wasm + jobs are enabled. The worker is the durable side of
	// jobs.schedule and async event delivery; without it, plugin-queued
	// jobs would sit in the queue forever. The EventService wires the
	// in-process bus to the subscription table; it enqueues a job per
	// matched subscription at emit time.
	if wasmDeps.runtime != nil && jobDeps.registry != nil && jobDeps.client != nil {
		registerPluginInvokeWorker(jobDeps.registry, wasmDeps.runtime, authDeps.repos.Plugins)
		eventSvc := eventservice.New(wasmDeps.bus, authDeps.repos.PluginSubscriptions, jobDeps.client, slog.Default())
		if startErr := eventSvc.Start(context.Background()); startErr != nil {
			log.Fatalf("failed to start event service: %s", startErr.Error())
		}
		defer eventSvc.Stop()
	}

	// Flip the idp service's JIT toggle from config. The toggle is
	// process-wide today (not per-provider); a future WS can move it onto
	// the per-provider struct if granular control is needed.
	idp.SetJITEnabled(conf.GetAuthSAMLJITEnabled())

	// WS-11: build the Incus driver (providers/incus/*). Returns a zero-value
	// incusDeps when providers.incus.enabled is false; the compute module
	// (WS-14) degrades to 501 in that case. Built AFTER wasmDeps so the
	// events listener can fan into the WASM event bus when both subsystems
	// are enabled.
	incusDeps, err := buildIncusDeps(context.Background(), wasmDeps.bus)
	if err != nil {
		log.Fatalf("failed to build incus deps: %s", err.Error())
	}
	if incusDeps.listener != nil {
		defer func() { _ = incusDeps.listener.Close() }()
	}

	// WS-14: build the compute module (internal/app/lahijan/compute/*).
	// Returns a nil service when the Incus provider is disabled; the api
	// handlers degrade to 501 in that case. Built AFTER wasmDeps + incusDeps
	// so it can wire both into the service. The policy evaluator is the
	// same one the audit gate uses; seeding has already run.
	var computeSvc *compute.Service
	if incusDeps.provider != nil {
		computeSvc = compute.New(
			incusDeps.provider,
			authDeps.repos,
			authDeps.audit,
			wasmDeps.bus,
			rbac.NewEvaluator(authDeps.repos.Memberships),
			compute.Config{Quotas: compute.DefaultQuotas()},
		)
		// Seed the featured-image catalog for every existing tenant. A
		// future WS will hook this into the tenant-create path so a new
		// tenant picks up the catalog automatically. Runs synchronously
		// at bootstrap so by the time the listener is up the catalog is
		// consistent.
		featuredAliases := incus.FeaturedImages(conf.GetProvidersIncusFeaturedImages())
		if len(featuredAliases) > 0 {
			seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
			seedComputeFeaturedImagesForTenants(seedCtx, computeSvc, authDeps.repos.Tenants, featuredAliases)
			seedCancel()
		}
	}

	// WS-12: build the PowerDNS driver (providers/powerdns/*). Returns a
	// zero-value powerdnsDeps when providers.powerdns.enabled is false; the
	// DNS module (WS-15) degrades to 501 in that case. Built AFTER wasmDeps
	// so the events synthesis path can fan into the WASM event bus when
	// both subsystems are enabled.
	powerdnsDeps, err := buildPowerdnsDeps(context.Background(), wasmDeps.bus)
	if err != nil {
		log.Fatalf("failed to build powerdns deps: %s", err.Error())
	}

	// WS-15: build the DNS module (internal/app/lahijan/dns/*). Returns a
	// nil service when the PowerDNS provider is disabled; the api handlers
	// degrade to 501 in that case. Built AFTER wasmDeps + powerdnsDeps so
	// it can wire both into the service. The policy evaluator mirrors the
	// compute module's wiring.
	var dnsSvc *dns.Service
	if powerdnsDeps.provider != nil {
		dnsSvc = dns.New(
			powerdnsDeps.provider,
			authDeps.repos,
			authDeps.audit,
			wasmDeps.bus,
			rbac.NewEvaluator(authDeps.repos.Memberships),
			dns.Config{
				DefaultNameservers:   conf.GetProvidersPowerDNSDefaultNameservers(),
				DefaultDNSSECEnabled: conf.GetProvidersPowerDNSDefaultDNSSECEnabled(),
			},
		)
	}

	// WS-13: build the SeaweedFS driver (providers/seaweedfs/*). Returns a
	// zero-value seaweedfsDeps when providers.seaweedfs.enabled is false;
	// the storage module (WS-16) degrades to 501 in that case. Built
	// AFTER wasmDeps so the events synthesis path can fan into the WASM
	// event bus when both subsystems are enabled.
	seaweedfsDeps, err := buildSeaweedfsDeps(context.Background(), wasmDeps.bus)
	if err != nil {
		log.Fatalf("failed to build seaweedfs deps: %s", err.Error())
	}

	// WS-16: build the storage module (internal/app/lahijan/storage/*).
	// Returns a nil service when the SeaweedFS provider is disabled; the
	// api handlers degrade to 501 in that case. Built AFTER wasmDeps +
	// seaweedfsDeps so it can wire both into the service. The policy
	// evaluator mirrors the compute / dns module's wiring.
	var storageSvc *storage.Service
	if seaweedfsDeps.provider != nil {
		storageSvc = storage.New(
			seaweedfsDeps.provider,
			authDeps.repos,
			authDeps.audit,
			wasmDeps.bus,
			rbac.NewEvaluator(authDeps.repos.Memberships),
			storage.Config{},
		)
	}

	// WS-17: build the billing & metering module
	// (internal/app/lahijan/billing/*). Always built — the module
	// has no provider dependency (it sits above the providers via
	// the Meter interface and above the compute module via the
	// Enforcer interface). When the Incus provider is disabled the
	// metering collectors run as no-ops; when compute is disabled
	// enforcement runs as a no-op. The service is wired with the
	// audit emitter + the WASM event bus + the shared policy
	// evaluator. The River workers are registered with the job
	// registry separately so they pick up the same service instance.
	billingSvc := billing.New(
		authDeps.repos,
		authDeps.audit,
		wasmDeps.bus,
		rbac.NewEvaluator(authDeps.repos.Memberships),
		billing.NoopEnforcer{},
		billing.NoopMeter{},
		billing.Config{},
	)
	// Register the billing River workers when jobs are enabled so
	// the metering/rollup/enforcement/receipts pipelines are live
	// the moment the supervisor starts. The workers share a
	// slog.Default() logger; a future WS can pass a scoped logger.
	if jobDeps.registry != nil {
		billing.RegisterJobs(jobDeps.registry, billingSvc, slog.Default())
	}

	// Seed the RBAC catalog (permissions + default roles + grants). Idempotent
	// so it is safe to run on every bootstrap. Fail-fast on error: without the
	// seed, every privileged route returns 403.
	seedCtx, seedCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer seedCancel()
	if err := rbac.SeedOnce(seedCtx, authDeps.repos); err != nil {
		log.Fatalf("failed to seed rbac catalog: %s", err.Error())
	}

	app := fiber.New(fiber.Config{
		ServerHeader: "Lahijan",
		AppName:      "Lahijan",
		BodyLimit:    conf.GetServerBodyLimit(),
		Concurrency:  conf.GetHTTPServerConcurrency(),
		Prefork:      conf.GetHTTPServerPreforkEnabled(),
		ErrorHandler: api.ErrorHandler(),
	})

	// Healthcheck middleware is infrastructure-only (probes for orchestrators)
	// and registers its own /healthcheck/* endpoints; it runs before the API
	// middleware stack so it stays out of the audit/rbac path.
	if conf.GetHTTPServerHealthcheckEnabled() {
		fiberlog.Debug("healthcheck middleware is enabled.")
		app.Use(healthcheck.New(healthcheck.Config{
			LivenessProbe:     func(c *fiber.Ctx) bool { return true },
			ReadinessProbe:    func(c *fiber.Ctx) bool { return true },
			ReadinessEndpoint: conf.GetHTTPServerHealthcheckReadinessEndpoint(),
			LivenessEndpoint:  conf.GetHTTPServerHealthcheckLivenessEndpoint(),
		}))
	}

	// Full middleware stack in the canonical order
	// (requestid -> recover -> cors -> logger -> tenant -> auth -> audit -> rbac).
	// See docs/architecture/conventions.md#http-api and api/middleware.
	authMW := buildAuthMiddleware(authDeps)
	tenantMW := buildTenantMiddleware(authDeps)
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(authDeps.repos.Memberships))
	middleware.Apply(app, middleware.Options{
		CORS:          corsOptions(),
		RequestLogger: conf.GetHTTPServerLoggerEnabled(),
		Tenant:        tenantMW,
		Auth:          authMW,
	})

	// Register the OpenAPI-derived routes (/health, /api/v1/ping, /api/v1/auth/*,
	// /api/v1/audit/*, ...). The audit gate runs RequirePerm for the audit
	// endpoints; everything else passes through to the handler.
	api.RegisterRoutes(app, api.NewServer(api.ServerDeps{
		Users:          authDeps.repos.Users,
		Sessions:       authDeps.repos.Sessions,
		SessionSvc:     authDeps.sessionSvc,
		PATSvc:         authDeps.patSvc,
		EmailSvc:       authDeps.emailSvc,
		Signer:         authDeps.signer,
		Cookies:        authDeps.cookies,
		Audit:          authDeps.repos.AuditLog,
		AuditEmitter:   authDeps.audit,
		IDPSvc:         idpDeps.svc,
		IDPOAuth:       idpDeps.oauthReg,
		IDPOIDC:        idpDeps.oidcReg,
		StateSigner:    idpDeps.stateSigner,
		IDPCookies:     api.DefaultExternalIDPCookies,
		IDPSAML:        samlDeps.registry,
		MFASvc:         mfaDeps.svc,
		Jobs:           jobDeps.client,
		PluginsRepo:    wasmDeps.pluginsRepo,
		PluginSvc:      wasmDeps.svc,
		MarketplaceSvc: wasmDeps.marketplace,
		ComputeSvc:     computeSvc,
		DNSSvc:         dnsSvc,
		StorageSvc:     storageSvc,
		BillingSvc:     billingSvc,
	}), policy)

	// WS-09: mount River's built-in web UI (admin-only). The UI ships its
	// own REST API under the same prefix; the same platform.jobs.read
	// RequirePerm wraps both. Skipped when jobs are disabled or when the
	// admin UI flag is off.
	if jobDeps.client != nil && conf.GetJobsAdminUIEnabled() {
		if err := mountJobsAdminUI(app, jobDeps.client, policy); err != nil {
			log.Fatalf("failed to mount jobs admin ui: %s", err.Error())
		}
	}

	if err := app.Listen(conf.GetHTTPServerAddress()); err != nil {
		return errors.Join(errors.New("fiber server stopped"), err)
	}
	return nil
}

// authDeps is the bundle of auth-subsystem dependencies wired at bootstrap. It
// is built once and shared by the API server and the auth middleware.
type authDeps struct {
	repos      *database.Repos
	signer     *secrets.Signer
	hasher     *password.Hasher
	sessionSvc *session.Service
	patSvc     *pat.Service
	emailSvc   *email.Service
	audit      audit.Emitter
	cookies    api.CookieConfig
}

// buildAuthDeps opens the DB pool, builds the repositories, and wires the
// session/pat/email services from config. Returns a cleanup func to close the pool.
func buildAuthDeps(ctx context.Context) (*authDeps, func(), error) {
	pool, err := database.NewPool(ctx)
	if err != nil {
		return nil, nil, errors.Join(errors.New("open db pool for auth deps"), err)
	}
	repos := database.NewRepos(pool)

	signingKey := conf.GetAuthSigningKey()
	signer := secrets.NewSigner(signingKey)
	a := conf.GetAuthPasswordArgon2()
	hasher := password.NewHasher(a.Memory, a.Iterations, a.Parallelism, a.SaltLength, a.KeyLength)
	emitter := audit.NewDBEmitter(repos.AuditLog)

	mailer := notifyemail.NewSMTPSender(notifyemail.SMTPConfig{
		Enabled:  conf.GetSMTPEnabled(),
		Host:     conf.GetSMTPHost(),
		Port:     conf.GetSMTPPort(),
		User:     conf.GetSMTPUser(),
		Password: conf.GetSMTPPassword(),
		From:     conf.GetSMTPFrom(),
		FromName: conf.GetSMTPFromName(),
	})

	emailSvc := email.New(
		repos.Users, repos.EmailTokens, hasher, signer, mailer, emitter,
		email.Config{
			VerifyTTL:      time.Duration(conf.GetAuthEmailVerificationTTLSeconds()) * time.Second,
			ResetTTL:       time.Duration(conf.GetAuthEmailPasswordResetTTLSeconds()) * time.Second,
			EmailChangeTTL: time.Duration(conf.GetAuthEmailEmailChangeTTLSeconds()) * time.Second,
			TokenByteLen:   conf.GetAuthEmailByteLength(),
			AppBaseURL:     conf.GetSMTPAppBaseURL(),
		},
	)

	sessionSvc := session.New(
		repos.Users, repos.Sessions, repos.Tokens, hasher, signer,
		emailSvc, emitter,
		session.Config{
			SessionLifetime: time.Duration(conf.GetAuthSessionLifetimeSeconds()) * time.Second,
			RefreshLifetime: time.Duration(conf.GetAuthRefreshLifetimeSeconds()) * time.Second,
			TokenByteLen:    conf.GetAuthEmailByteLength(),
			MinPasswordLen:  conf.GetAuthPasswordMinLength(),
			RequireVerified: false, // WS-06 leaves email-required-login configurable later
		},
	)

	patSvc := pat.New(
		repos.Tokens, signer, emitter,
		pat.Config{
			Prefix:  conf.GetAuthPATPrefix(),
			ByteLen: conf.GetAuthPATByteLength(),
		},
	)

	cookies := api.CookieConfig{
		SessionName:   conf.GetAuthSessionCookieName(),
		RefreshName:   conf.GetAuthRefreshCookieName(),
		Domain:        conf.GetAuthSessionCookieDomain(),
		Path:          conf.GetAuthSessionCookiePath(),
		Secure:        conf.GetAuthSessionCookieSecure(),
		SameSite:      conf.GetAuthSessionCookieSameSite(),
		SessionMaxAge: conf.GetAuthSessionLifetimeSeconds(),
		RefreshMaxAge: conf.GetAuthRefreshLifetimeSeconds(),
	}

	cleanup := func() { pool.Close() }
	return &authDeps{
		repos:      repos,
		signer:     signer,
		hasher:     hasher,
		sessionSvc: sessionSvc,
		patSvc:     patSvc,
		emailSvc:   emailSvc,
		audit:      emitter,
		cookies:    cookies,
	}, cleanup, nil
}

// buildAuthMiddleware builds the Auth middleware with the session-cookie +
// PAT resolver (WS-06). Returns nil to leave the pass-through seam in place
// when deps are unavailable (tests); program.Start always passes real deps.
func buildAuthMiddleware(deps *authDeps) fiber.Handler {
	if deps == nil {
		return nil
	}
	return middleware.AuthWithResolver(middleware.AuthResolver{
		Cookies: middleware.CookieConfig{
			SessionName: deps.cookies.SessionName,
			RefreshName: deps.cookies.RefreshName,
		},
		Signer:       deps.signer,
		SessionsRepo: deps.repos.Sessions,
		PATService:   deps.patSvc,
	})
}

// buildTenantMiddleware builds the Tenant middleware with the X-Tenant-Id /
// X-Tenant-Slug resolver (WS-08). Returns nil to leave the pass-through seam
// in place when deps are unavailable (tests); program.Start always passes
// real deps.
func buildTenantMiddleware(deps *authDeps) fiber.Handler {
	if deps == nil {
		return nil
	}
	return middleware.TenantWithResolver(middleware.TenantResolver{
		Tenants: deps.repos.Tenants,
	})
}

// corsOptions builds the middleware CORS config from config when CORS is
// enabled, returning nil (CORS disabled) otherwise.
func corsOptions() *middleware.CORSConfig {
	if !conf.GetHTTPServerCORSEnabled() {
		return nil
	}
	fiberlog.Debug("cors middleware is enabled.")
	return &middleware.CORSConfig{
		AllowMethods: strings.Join(conf.GetHTTPServerCORSAllowedMethods(), ","),
		AllowHeaders: strings.Join(conf.GetHTTPServerCORSAllowedHeaders(), ","),
		AllowOrigins: strings.Join(conf.GetHTTPServerCORSAllowedOrigins(), ","),
		MaxAge:       conf.GetHTTPServerCORSMaxAge(),
	}
}

// isDev reports whether the environment is a dev variant.
func isDev(env string) bool {
	e := strings.ToLower(strings.TrimSpace(env))
	return e == "" || e == "dev" || e == "development" || e == "local"
}

// idpDeps bundles the external-IdP (WS-07a) dependencies built at bootstrap.
// Any field may be nil when the corresponding feature is disabled in config;
// the api handlers degrade to "feature disabled" envelopes when so.
type idpDeps struct {
	svc         *idp.Service
	oauthReg    *oauth.Registry
	oidcReg     *oidc.Registry
	stateSigner *state.Signer
}

// buildIdpDeps wires the OAuth + OIDC providers + the account-linking service
// from config. Returns an empty idpDeps (all nil) when no provider is enabled
// in either registry, so the api handlers can short-circuit cleanly.
//
// Failures here are loud (log.Fatalf) because a misconfigured provider that
// the deployer turned on should not silently degrade to "no IdP" at runtime.
func buildIdpDeps(ctx context.Context, a *authDeps) (idpDeps, error) {
	stateSigner := state.NewSigner(a.signer)

	// Build the OAuth registry from conf.auth.oauth.providers.*.
	oauthNames := conf.ListAuthOAuthProviderNames()
	oauthProviders := make([]oauth.Provider, 0, len(oauthNames))
	redirectBase := conf.GetAuthOAuthRedirectBase()
	for _, name := range oauthNames {
		cfg := conf.GetAuthOAuthProvider(name)
		if !cfg.Enabled {
			continue
		}
		preset := oauth.PresetConfig{
			Key:          name,
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  buildRedirectURL(redirectBase, "/api/v1/auth/oauth/"+name+"/callback"),
			Scopes:       cfg.Scopes,
		}
		var p oauth.Provider
		switch name {
		case "google":
			p = oauth.NewGoogle(preset, stateSigner.Verify)
		case "github":
			p = oauth.NewGitHub(preset, stateSigner.Verify)
		default:
			// Generic OAuth2 (self-hosted IdP). Endpoints are sourced from
			// the per-provider endpoints subkey when shipped; for now we
			// fall back to google.Endpoint because the YAML has no generic
			// endpoint config in the WS-07a scope.
			p = oauth.NewGoogle(preset, stateSigner.Verify)
		}
		oauthProviders = append(oauthProviders, p)
	}
	oauthReg := oauth.NewRegistry(oauthProviders...)

	// Build the OIDC registry from conf.auth.oidc.providers.*. Discovery
	// happens here, once per provider, against the live IdP — a broken
	// issuer URL fails bootstrap.
	oidcNames := conf.ListAuthOIDCProviderNames()
	oidcProviders := make([]oidc.Provider, 0, len(oidcNames))
	oidcRedirectBase := conf.GetAuthOIDCRedirectBase()
	for _, name := range oidcNames {
		cfg := conf.GetAuthOIDCProvider(name)
		if !cfg.Enabled {
			continue
		}
		p, err := oidc.NewProvider(ctx, oidc.ProviderConfig{
			Key:          name,
			Issuer:       cfg.Issuer,
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  buildRedirectURL(oidcRedirectBase, "/api/v1/auth/oidc/"+name+"/callback"),
			Scopes:       cfg.Scopes,
		}, stateSigner.Verify)
		if err != nil {
			return idpDeps{}, errors.Join(errors.New("oidc discovery for "+name), err)
		}
		oidcProviders = append(oidcProviders, p)
	}
	oidcReg := oidc.NewRegistry(oidcProviders...)

	// If no provider is enabled, return an empty deps so handlers degrade.
	if len(oauthProviders) == 0 && len(oidcProviders) == 0 {
		return idpDeps{}, nil
	}

	// Build the AES-GCM envelope for token encryption. The key is sourced
	// from conf.auth.secrets.encryptionKey (base64 of 32 raw bytes). In dev
	// an empty key falls back to a deterministic warning-prefixed value.
	crypto, err := buildCrypto()
	if err != nil {
		return idpDeps{}, err
	}

	svc := idp.New(a.repos, crypto, a.audit, &sessionOpenerAdapter{svc: a.sessionSvc})
	return idpDeps{
		svc:         svc,
		oauthReg:    oauthReg,
		oidcReg:     oidcReg,
		stateSigner: stateSigner,
	}, nil
}

// buildCrypto loads the AES-256-GCM encryption key from conf and returns the
// Crypto envelope. In dev an empty key is derived from a fixed warning value
// so misconfiguration is loud but local dev "just works".
func buildCrypto() (*secrets.Crypto, error) {
	raw := conf.GetAuthSecretsEncryptionKey()
	if raw == "" {
		if isDev(conf.GetEnvironment()) {
			// Dev-only deterministic key. Logged once at warn so the dev
			// sees it. Production rejects this path via the empty check.
			fiberlog.Warn("auth.secrets.encryptionKey is empty in dev; using a derived warning value. Set it in any non-dev environment.")
			derived := sha256.Sum256([]byte("DEV-ONLY-INSECURE-CHANGE-ME-lahijan-encryption-key"))
			return secrets.NewCrypto(derived[:])
		}
		return nil, errors.New("auth.secrets.encryptionKey must be set in any non-dev environment")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.Join(errors.New("auth.secrets.encryptionKey is not valid base64"), err)
	}
	return secrets.NewCrypto(key)
}

// buildRedirectURL prepends the configured redirectBase to the given path.
// In dev, when redirectBase is empty, the path is returned as-is and the
// handler will derive the absolute URL from the request Host header.
func buildRedirectURL(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimRight(base, "/") + path
}

// sessionOpenerAdapter bridges session.Service (which owns openSession) to
// idp.SessionOpener. Lives here so the auth/idp package does not need to
// import auth/session (which would create a cycle in some test setups).
type sessionOpenerAdapter struct {
	svc *session.Service
}

// OpenForExistingUser implements idp.SessionOpener by delegating to
// session.Service.OpenForExistingUser and reshaping the result into
// idp.SessionOpen.
func (a *sessionOpenerAdapter) OpenForExistingUser(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (idp.SessionOpen, error) {
	sess, err := a.svc.OpenForExistingUser(ctx, userID, ua, ip)
	if err != nil {
		return idp.SessionOpen{}, err
	}
	return idp.SessionOpen{
		UserID:      sess.UserID,
		SessionID:   sess.SessionID,
		ExpiresAt:   sess.ExpiresAt,
		CookieValue: sess.CookieValue,
		Refresh: idp.RefreshIssue{
			Raw:       sess.Refresh.Raw,
			FamilyID:  sess.Refresh.FamilyID,
			ExpiresAt: sess.Refresh.ExpiresAt,
		},
	}, nil
}

// mfaSessionOpenerAdapter bridges session.Service to mfa.SessionOpener.
// Lives here for the same reason sessionOpenerAdapter does (avoids an
// import cycle between auth/mfa and auth/session).
type mfaSessionOpenerAdapter struct {
	svc *session.Service
}

// OpenForExistingUser implements mfa.SessionOpener by delegating to
// session.Service.OpenForExistingUser and reshaping the result into
// mfa.SessionOpen.
func (a *mfaSessionOpenerAdapter) OpenForExistingUser(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (mfa.SessionOpen, error) {
	sess, err := a.svc.OpenForExistingUser(ctx, userID, ua, ip)
	if err != nil {
		return mfa.SessionOpen{}, err
	}
	return mfa.SessionOpen{
		UserID:      sess.UserID,
		SessionID:   sess.SessionID,
		ExpiresAt:   sess.ExpiresAt,
		CookieValue: sess.CookieValue,
		Refresh: mfa.RefreshIssue{
			Raw:       sess.Refresh.Raw,
			FamilyID:  sess.Refresh.FamilyID,
			ExpiresAt: sess.Refresh.ExpiresAt,
		},
	}, nil
}

// samlDeps bundles the SAML SP (WS-07b) dependencies built at bootstrap. The
// registry is nil when no SAML provider is enabled, so the api handlers can
// short-circuit cleanly.
type samlDeps struct {
	registry *saml.Registry
}

// buildSamlDeps wires the SAML ServiceProvider registry from config. Returns
// an empty samlDeps when no provider is enabled, so the api handlers can
// short-circuit cleanly. The shared stateSigner from WS-07a is reused so the
// SAML state-token carries the same HMAC signing key as OAuth/OIDC.
//
// Failures here are loud (log.Fatalf) because a misconfigured SAML provider
// that the deployer turned on should not silently degrade to "no SAML" at
// runtime.
func buildSamlDeps(ctx context.Context, a *authDeps, stateSigner *state.Signer) (samlDeps, error) {
	if stateSigner == nil {
		stateSigner = state.NewSigner(a.signer)
	}
	names := conf.ListAuthSAMLProviderNames()
	if len(names) == 0 {
		return samlDeps{}, nil
	}

	creds, err := buildSAMLCredentials()
	if err != nil {
		return samlDeps{}, err
	}

	redirectBase := conf.GetAuthSAMLRedirectBase()
	providers := make([]saml.Provider, 0, len(names))
	for _, name := range names {
		cfg := conf.GetAuthSAMLProvider(name)
		if !cfg.Enabled {
			continue
		}
		metadataURL := buildRedirectURL(redirectBase, "/api/v1/auth/saml/metadata")
		acsURL := buildRedirectURL(redirectBase, "/api/v1/auth/saml/"+name+"/acs")
		entityID := cfg.EntityID
		if entityID == "" {
			entityID = metadataURL
		}
		p, err := saml.NewProvider(saml.ProviderConfig{
			Key:               name,
			EntityID:          entityID,
			ACSURL:            acsURL,
			MetadataURL:       metadataURL,
			IDPMetadataXML:    cfg.IDPMetadataXML,
			IDPMetadataURL:    cfg.IDPMetadataURL,
			AllowIDPInitiated: cfg.AllowIDPInitiated,
			AttributeMap: saml.AttributeMap{
				Email: cfg.EmailAttribute,
				Name:  cfg.NameAttribute,
			},
		}, creds, stateSigner.Verify)
		if err != nil {
			return samlDeps{}, errors.Join(errors.New("saml provider "+name), err)
		}
		providers = append(providers, p)
	}
	if len(providers) == 0 {
		return samlDeps{}, nil
	}
	return samlDeps{registry: saml.NewRegistry(providers...)}, nil
}

// buildSAMLCredentials loads the SP signing key + cert from conf. In dev an
// empty key falls back to a freshly-generated RSA keypair so local dev "just
// works" without requiring the deployer to mint a cert; production rejects
// an empty key. The cert is required in every environment because the SP
// metadata MUST publish a real x509 cert the IdP will pin.
//
// The key is HIGH-SENSITIVITY material. It is sourced from env
// LAHIJAN_AUTH_SAML_SP_SIGNING_KEY (or a file path in
// LAHIJAN_AUTH_SAML_SP_SIGNING_KEY_FILE, read by the bootstrap), loaded once
// into process memory, and NEVER persisted to the database. "Encrypted at
// rest" is satisfied by the deployment secret management that backs the env
// var / file (typically Docker secrets, Kubernetes secrets, or Vault).
func buildSAMLCredentials() (saml.SPCredentials, error) {
	keyPEM := conf.GetAuthSAMLSPSigningKey()
	certPEM := conf.GetAuthSAMLSPSigningCert()
	if keyPEM == "" || certPEM == "" {
		if !isDev(conf.GetEnvironment()) {
			if keyPEM == "" {
				return saml.SPCredentials{}, errors.New("auth.saml.spSigningKey must be set in any non-dev environment")
			}
			return saml.SPCredentials{}, errors.New("auth.saml.spSigningCert must be set in any non-dev environment")
		}
		fiberlog.Warn("auth.saml.spSigningKey/ spSigningCert are empty in dev; generating a fresh keypair. Set both in any non-dev environment.")
		kp, err := generateDevSAMLKeyPair()
		if err != nil {
			return saml.SPCredentials{}, fmt.Errorf("saml dev keypair: %w", err)
		}
		return kp, nil
	}
	return saml.SPCredentials{KeyPEM: []byte(keyPEM), CertPEM: []byte(certPEM)}, nil
}

// generateDevSAMLKeyPair mints a fresh RSA-2048 keypair + self-signed cert
// for dev-mode SAML. The keypair is regenerated on every process restart,
// so the SP metadata changes; that is acceptable for dev but not for prod.
func generateDevSAMLKeyPair() (saml.SPCredentials, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return saml.SPCredentials{}, fmt.Errorf("rsa keygen: %w", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lahijan-saml-dev-sp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return saml.SPCredentials{}, fmt.Errorf("x509 create: %w", err)
	}
	return saml.SPCredentials{
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}),
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}, nil
}

// mfaDeps bundles the MFA service (WS-07c) dependencies built at bootstrap.
// svc is nil-appropriate when MFA is disabled in config; the api handlers
// degrade to "feature disabled" envelopes when so.
type mfaDeps struct {
	svc *mfa.Service
}

// buildMFADeps wires the TOTP + WebAuthn + recovery orchestrator from
// config. Returns an empty mfaDeps (svc nil) when MFA is disabled entirely.
//
// Failures here are loud (log.Fatalf) because a misconfigured WebAuthn
// relying-party the deployer turned on should not silently degrade to
// "no WebAuthn" at runtime.
//
// The TOTP / recovery code paths do not need any external config and are
// always wired; only WebAuthn needs the RPID + RPOrigins.
func buildMFADeps(ctx context.Context, a *authDeps) (mfaDeps, error) {
	cfg := mfa.DefaultConfig()
	cfg.TOTP.Issuer = conf.GetAuthMFATOTPIssuer()
	cfg.Recovery.Count = conf.GetAuthMFARecoveryCount()
	cfg.PendingLifetime = time.Duration(conf.GetAuthMFAPendingTTLSeconds()) * time.Second
	cfg.MaxAttempts = conf.GetAuthMFAMaxAttempts()

	// Build the WebAuthn RP only when both rpId and at least one origin
	// are configured. Otherwise the orchestrator's RP stays nil and the
	// handlers degrade for WebAuthn only (TOTP / recovery still work).
	var rp *webauthn.RP
	rpID := conf.GetAuthMFAWebauthnRPID()
	origins := conf.GetAuthMFAWebauthnRPOrigins()
	if rpID != "" && len(origins) > 0 {
		cfg.WebAuthn = webauthn.Config{
			RPID:          rpID,
			RPDisplayName: conf.GetAuthMFAWebauthnRPDisplayName(),
			RPOrigins:     origins,
			RPTopOrigins:  conf.GetAuthMFAWebauthnRPTopOrigins(),
		}
		var err error
		rp, err = webauthn.New(cfg.WebAuthn)
		if err != nil {
			return mfaDeps{}, errors.Join(errors.New("build webauthn RP"), err)
		}
	}

	// Reuse the AES-GCM crypto envelope from authDeps so the TOTP secret
	// is encrypted with the same process-wide key as the IdP tokens.
	crypto, err := buildCrypto()
	if err != nil {
		return mfaDeps{}, err
	}

	svc := mfa.New(
		a.repos,
		crypto,
		a.signer,
		rp,
		&mfaSessionOpenerAdapter{svc: a.sessionSvc},
		a.audit,
		cfg,
	)
	// Silence the unused-package warnings for totp / recovery imports —
	// they are used via the DefaultConfig() above but go's import-unused
	// check still complains without the explicit reference.
	_ = totp.DefaultConfig
	_ = recovery.DefaultConfig
	return mfaDeps{svc: svc}, nil
}

// jobDeps bundles the WS-09 job-subsystem dependencies built at bootstrap.
// Every field is nil-appropriate: when conf.jobs.enabled is false the bundle
// is zero-value, the api handlers degrade to 501, and no supervisor is
// started. The cleanup func is invoked at process shutdown to drain the
// queue before the DB pool closes.
type jobDeps struct {
	registry   *jobs.Registry
	client     *jobs.Client
	supervisor *jobs.Supervisor
}

// buildJobDeps wires the River client + supervisor + example workers from
// config. Returns an empty jobDeps (all nil) when jobs.enabled is false so
// the api handlers degrade cleanly. The supervisor is returned WITHOUT
// having been started — program.Start starts it in the background so
// bootstrap is not blocked on leadership election.
//
// WS-10b: the wasm.plugin.invoke worker is registered when both jobs and
// wasm are enabled. The worker resolves plugin ids to compiled modules
// via the installer; that resolver is wired in buildWasmDeps AFTER the
// jobs deps return, so we leave the registration slot empty here and
// program.Start calls registerPluginInvokeWorker once both deps exist.
func buildJobDeps(_ context.Context, _ *database.Repos) (jobDeps, error) {
	if !conf.GetJobsEnabled() {
		fiberlog.Debug("jobs subsystem is disabled; skipping river client setup")
		return jobDeps{}, nil
	}

	// Reuse the authDeps pool — one pgxpool per process is the recommended
	// pattern (River is transactional with the data it touches).
	pool, err := database.NewPool(context.Background())
	if err != nil {
		return jobDeps{}, errors.Join(errors.New("open db pool for jobs deps"), err)
	}

	registry := jobs.NewRegistry()
	// Register the four WS-09 example workers so the queue has something
	// to execute end-to-end before any domain module ships. Domain modules
	// (WS-14, WS-17, ...) will add their own workers via jobs.Register in
	// their own Setup functions.
	jobs.RegisterExamples(registry, slog.Default())

	cfg := jobs.Config{
		Logger:                    slog.Default(),
		JobTimeout:                time.Duration(conf.GetJobsJobTimeoutSeconds()) * time.Second,
		MaxAttempts:               conf.GetJobsMaxAttempts(),
		PollOnly:                  conf.GetJobsPollOnly(),
		DefaultMaxWorkersPerQueue: conf.GetJobsDefaultMaxWorkersPerQueue(),
	}
	client, err := jobs.NewClient(pool, registry, cfg)
	if err != nil {
		pool.Close()
		return jobDeps{}, errors.Join(errors.New("build river client"), err)
	}

	supervisor, err := jobs.NewSupervisor(client,
		time.Duration(conf.GetJobsSoftStopTimeoutSeconds())*time.Second)
	if err != nil {
		pool.Close()
		return jobDeps{}, errors.Join(errors.New("build river supervisor"), err)
	}

	return jobDeps{
		registry:   registry,
		client:     client,
		supervisor: supervisor,
	}, nil
}

// mountJobsAdminUI mounts River's built-in web UI on the given Fiber app.
// The UI is wrapped in a RequirePerm(platform.jobs.read) gate so only
// platform admins can reach it; the same gate covers the UI's own REST API
// (River ships /api/* routes alongside the SPA assets).
func mountJobsAdminUI(app *fiber.App, client *jobs.Client, policy middleware.PolicyResolver) error {
	if client == nil {
		return nil
	}
	prefix := conf.GetJobsAdminUIPath()
	if prefix == "" {
		prefix = "/admin/jobs/ui"
	}

	endpoints := riverui.NewEndpoints(client.River(), nil)
	uiHandler, err := riverui.NewHandler(&riverui.HandlerOpts{
		Endpoints: endpoints,
		Logger:    slog.Default(),
		Prefix:    prefix,
		DevMode:   isDev(conf.GetEnvironment()),
	})
	if err != nil {
		return errors.Join(errors.New("build riverui handler"), err)
	}

	// Start the UI's background services (caching + status polling). The
	// services live for the rest of the process; we tie them to a
	// process-lifetime context that is never cancelled explicitly (the OS
	// reaps the goroutines on exit). The supervisor's Stop drains the
	// queue before the process exits; the UI's services do not need
	// graceful shutdown.
	if err := uiHandler.Start(context.Background()); err != nil {
		return errors.Join(errors.New("start riverui handler"), err)
	}

	// Wrap the http.Handler with adaptor.HTTPHandler so Fiber can serve it.
	// The RequirePerm gate runs first; only platform.admin (the only role
	// that holds platform.jobs.read) reaches the UI.
	httpHandler := adaptor.HTTPHandler(uiHandler)
	gate := middleware.RequirePerm(policy, rbac.PermPlatformJobsRead)
	app.Use(prefix, gate, httpHandler)
	fiberlog.Info("jobs admin ui mounted", "path", prefix)
	return nil
}

// registerPluginInvokeWorker registers the WS-10b wasm.plugin.invoke
// worker on the River registry. The worker resolves plugin_id ->
// compiled-module hash by reading the plugins table via the
// PluginsRepository; the hash is the sha256 of the wasm bytes the
// installer stored at upload time. Called from Start AFTER both
// jobDeps and wasmDeps are built, so the worker closes over both.
//
// The hash lookup uses the plugins table's wasm_hash column directly;
// the runtime's Compile step is idempotent on hash, so the worker's
// instantiation succeeds whether the module was compiled at upload
// time (warm) or at first invoke (cold).
func registerPluginInvokeWorker(
	registry *jobs.Registry,
	rt *wasmruntime.Runtime,
	plugins *database.PluginsRepository,
) {
	if registry == nil || rt == nil || plugins == nil {
		return
	}
	resolver := func(ctx context.Context, pluginID uuid.UUID) (string, error) {
		row, err := plugins.Get(ctx, pluginID)
		if err != nil {
			return "", err
		}
		return row.WasmHash, nil
	}
	jobs.Register(registry, hostfuncs.PluginInvokeArgs{},
		wasmworker.NewPluginInvokeWorker(rt, slog.Default(), resolver),
		jobs.KindSpec{
			Queue:       "plugins",
			Description: "Invoke a WASM plugin export on behalf of jobs.schedule or events delivery.",
			Tags:        []string{"wasm", "plugins"},
		})
}

// wasmDeps bundles the WS-10a/WS-10b/WS-10c WASM plugin subsystem
// dependencies built at bootstrap. Every field is nil-appropriate: when
// conf.wasm.enabled is false the bundle is zero-value, the api handlers
// degrade to 501, and no runtime is built.
type wasmDeps struct {
	runtime     *wasmruntime.Runtime
	pluginsRepo *database.PluginsRepository
	svc         *installer.Service
	bus         *eventbus.Bus
	marketplace *marketplace.Service
}

// buildWasmDeps wires the wazero runtime + permission enforcer + installer
// service from config. Returns an empty wasmDeps when wasm.enabled is false
// so the api handlers degrade cleanly. WS-10b registers the host-functions
// bundle (kv / events / jobs / api / network / config) via
// runtime.Config.HostFunctions so plugins that declare those imports can
// call them; every host function routes its permission check through the
// enforcer built here.
//
// WS-10c additionally builds the marketplace service when
// wasm.marketplace.{path,url} resolves to a parseable index. The
// marketplace service is nil-appropriate when the index is missing;
// the api handlers degrade to 501.
func buildWasmDeps(_ context.Context, a *authDeps, j *jobDeps) (wasmDeps, error) {
	if !conf.GetWasmEnabled() {
		fiberlog.Debug("wasm subsystem is disabled; skipping wazero runtime setup")
		// Even with the runtime disabled, expose the repo so future
		// operators can list + inspect existing plugin rows through the
		// admin API. The handlers degrade to 501 only on the write paths
		// that need pluginSvc (which stays nil).
		return wasmDeps{pluginsRepo: a.repos.Plugins}, nil
	}

	enforcer := permission.NewDBEnforcer(a.repos.Plugins)
	// Build the event bus. Plugin events + the audit log's event-listener
	// tap both go through here. The bus's async dispatcher is wired when
	// the EventService lands (WS-10b deliverable; for now Emit dispatches
	// synchronously to in-process listeners only).
	bus := eventbus.New(eventbus.Config{
		Logger: slog.Default(),
	})

	// Build the host-functions registrar. River client may be nil when
	// jobs are disabled — the jobs.schedule host function degrades to
	// StatusUnavailable in that case.
	var riverClient *jobs.Client
	if j != nil {
		riverClient = j.client
	}
	hostReg, err := hostfuncs.Registrar(hostfuncs.Deps{
		Enforcer: enforcer,
		Logger:   slog.Default(),
		Repos:    a.repos,
		Bus:      bus,
		Jobs:     riverClient,
	})
	if err != nil {
		return wasmDeps{}, errors.Join(errors.New("build hostfuncs registrar"), err)
	}

	rt, err := wasmruntime.New(context.Background(), enforcer, wasmruntime.Config{
		MaxMemoryBytes: conf.GetWasmMaxMemoryPerPlugin(),
		ExecTimeout:    time.Duration(conf.GetWasmExecTimeoutMs()) * time.Millisecond,
		Logger:         slog.Default(),
		// WS-10b: register every host module. Plugins that declare
		// imports from any lahijan_* module now resolve them at
		// instantiation; the permission gate runs on every call.
		HostFunctions: hostReg,
	})
	if err != nil {
		return wasmDeps{}, errors.Join(errors.New("build wazero runtime"), err)
	}

	// WS-10c: build the installer with the WS-10b side-channel repos so
	// the Upgrade flow can clean up HTTP handler mounts + event
	// subscriptions before CASCADE.
	svc := installer.New(a.repos.Plugins, rt, a.audit).
		WithSideRepos(a.repos.PluginHTTPHandlers, a.repos.PluginSubscriptions)

	// WS-10c: build the marketplace. The index loader picks local vs
	// HTTP based on conf.wasm.marketplace.url; the asset loader matches.
	// A missing or malformed index is a soft error: the marketplace
	// service stays nil and the api handlers degrade to 501.
	var mktSvc *marketplace.Service
	ttl := time.Duration(conf.GetWasmMarketplaceCacheTTL()) * time.Second
	if url := conf.GetWasmMarketplaceURL(); url != "" {
		mktSvc = marketplace.New(
			marketplace.NewHTTPIndexLoader(url, ttl),
			marketplace.NewHTTPAssetLoader(url),
			svc, a.repos.Plugins, a.audit, slog.Default(),
		)
		fiberlog.Info("wasm marketplace enabled", "url", url)
	} else if path := conf.GetWasmMarketplacePath(); path != "" {
		mktSvc = marketplace.New(
			marketplace.NewLocalIndexLoader(path, ttl),
			marketplace.NewLocalAssetLoader(path),
			svc, a.repos.Plugins, a.audit, slog.Default(),
		)
		fiberlog.Info("wasm marketplace enabled", "path", path)
	}

	fiberlog.Info("wasm subsystem enabled",
		"max_memory_per_plugin", conf.GetWasmMaxMemoryPerPlugin(),
		"exec_timeout_ms", conf.GetWasmExecTimeoutMs())
	return wasmDeps{
		runtime:     rt,
		pluginsRepo: a.repos.Plugins,
		svc:         svc,
		bus:         bus,
		marketplace: mktSvc,
	}, nil
}
