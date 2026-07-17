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
// audit, rbac) are always registered as pass-through seams.
type Options struct {
	// CORS, when non-nil, enables the cors middleware with the given config.
	CORS *CORSConfig
	// RequestLogger enables Fiber's request logger middleware when true.
	RequestLogger bool
}

// Apply registers the full Lahijan middleware stack on app, in the canonical
// order mandated by docs/architecture/conventions.md#http-api:
//
//	requestid -> recover -> cors -> logger -> tenant -> auth -> audit -> rbac
//
// Every later handler runs after all of these. Domain slots are pass-through
// skeletons in WS-05 and are filled in by WS-06 (auth) and WS-08 (rbac/audit).
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

	// 5-8. Domain slots (pass-through seams). See the individual files.
	app.Use(Tenant())
	app.Use(Auth())
	app.Use(Audit())
	app.Use(RBAC())
}
