// Package middleware: stack.go assembles the full middleware stack in the
// canonical order. This is the single place that owns middleware ordering.
package middleware

import (
	"github.com/gofiber/fiber/v2"
	fiberlog "github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// CORSConfig carries the CORS settings that the cors slot needs. It mirrors the
// shape of conf.GetHTTPServerCORS* so the api package does not depend on conf
// directly for a pure-function middleware setup (easier to test).
type CORSConfig struct {
	AllowMethods string
	AllowHeaders string
	AllowOrigins string
	MaxAge       int
}

// Options configures Apply. The infrastructure slots (cors, logger) are
// optional and conditionally registered; the domain slots (tenant, auth,
// audit, rbac) are always registered. Auth may be overridden with a real
// resolver-backed handler (WS-06); when nil, the pass-through seam is used.
type Options struct {
	// CORS, when non-nil, enables the cors middleware with the given config.
	CORS *CORSConfig
	// RequestLogger enables Fiber's request logger middleware when true.
	RequestLogger bool
	// Auth, when non-nil, replaces the pass-through Auth slot with a real
	// resolver-backed handler (WS-06). Nil keeps the WS-05 pass-through.
	Auth fiber.Handler
}

// Apply registers the full Lahijan middleware stack on app, in the canonical
// order mandated by docs/architecture/conventions.md#http-api:
//
//	requestid -> recover -> cors -> logger -> tenant -> auth -> audit -> rbac
//
// Every later handler runs after all of these. The auth slot defaults to a
// pass-through seam and is replaced by WS-06's resolver-backed handler when
// Options.Auth is set; audit/rbac remain pass-through seams filled by WS-08.
func Apply(app *fiber.App, opts Options) {
	// 1. requestid — must be first so every downstream log/audit row can carry
	//    the request id.
	app.Use(requestid.New())

	// 2. recover — catch panics, never let them crash the process.
	app.Use(recover.New())

	// 3. cors (optional) — preflight handling for browser clients.
	if opts.CORS != nil {
		fiberlog.Debug("cors middleware is enabled.")
		app.Use(cors.New(cors.Config{
			AllowMethods: opts.CORS.AllowMethods,
			AllowHeaders: opts.CORS.AllowHeaders,
			AllowOrigins: opts.CORS.AllowOrigins,
			MaxAge:       opts.CORS.MaxAge,
		}))
	}

	// 4. logger (optional) — per-request access log.
	if opts.RequestLogger {
		fiberlog.Debug("logging middleware is enabled.")
		app.Use(logger.New())
	}

	// 5. tenant (pass-through seam — WS-06/WS-08 fill in real resolution).
	app.Use(Tenant())

	// 6. auth — pass-through seam, or the WS-06 resolver-backed handler.
	if opts.Auth != nil {
		app.Use(opts.Auth)
	} else {
		app.Use(Auth())
	}

	// 7-8. audit + rbac (pass-through seams, filled by WS-08).
	app.Use(Audit())
	app.Use(RBAC())
}
