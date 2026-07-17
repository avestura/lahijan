// Package program is Lahijan's bootstrap layer: it sets up config, builds the
// GoFiber app, wires middleware, builds the auth subsystem (WS-06), registers
// routes, and calls app.Listen. All other backend code is invoked from here;
// nothing should call into program/ from outside.
package program

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	_ "go.uber.org/automaxprocs"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/conf/computeddefault"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
	fiberlog "github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
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
	middleware.Apply(app, middleware.Options{
		CORS:          corsOptions(),
		RequestLogger: conf.GetHTTPServerLoggerEnabled(),
		Auth:          authMW,
	})

	// Register the OpenAPI-derived routes (/health, /api/v1/ping, /api/v1/auth/*, ...).
	api.RegisterRoutes(app, api.NewServer(api.ServerDeps{
		Users:      authDeps.repos.Users,
		Sessions:   authDeps.repos.Sessions,
		SessionSvc: authDeps.sessionSvc,
		PATSvc:     authDeps.patSvc,
		EmailSvc:   authDeps.emailSvc,
		Signer:     authDeps.signer,
		Cookies:    authDeps.cookies,
	}))

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
