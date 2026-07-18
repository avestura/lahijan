// Package program is Lahijan's bootstrap layer: it sets up config, builds the
// GoFiber app, wires middleware, builds the auth subsystem (WS-06) and the
// RBAC/audit subsystem (WS-08), seeds the permission catalog, registers
// routes, and calls app.Listen. All other backend code is invoked from here;
// nothing should call into program/ from outside.
package program

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"net/netip"
	"strings"
	"time"

	_ "go.uber.org/automaxprocs"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/conf/computeddefault"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
	fiberlog "github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/google/uuid"
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
		Users:        authDeps.repos.Users,
		Sessions:     authDeps.repos.Sessions,
		SessionSvc:   authDeps.sessionSvc,
		PATSvc:       authDeps.patSvc,
		EmailSvc:     authDeps.emailSvc,
		Signer:       authDeps.signer,
		Cookies:      authDeps.cookies,
		Audit:        authDeps.repos.AuditLog,
		AuditEmitter: authDeps.audit,
		IDPSvc:       idpDeps.svc,
		IDPOAuth:     idpDeps.oauthReg,
		IDPOIDC:      idpDeps.oidcReg,
		StateSigner:  idpDeps.stateSigner,
		IDPCookies:   api.DefaultExternalIDPCookies,
	}), policy)

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
