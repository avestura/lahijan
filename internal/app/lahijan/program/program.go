// Package program is Lahijan's bootstrap layer: it sets up config, builds the
// GoFiber app, wires middleware, registers routes, and calls app.Listen.
// All other backend code is invoked from here; nothing should call into
// program/ from outside.
package program

import (
	"errors"
	"log"
	"strings"

	_ "go.uber.org/automaxprocs"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/conf/computeddefault"
	"github.com/gofiber/fiber/v2"
	fiberlog "github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
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

	app := fiber.New(fiber.Config{
		ServerHeader: "Fiber",
		AppName:      "Lahijan",
		BodyLimit:    conf.GetServerBodyLimit(),
		Concurrency:  conf.GetHTTPServerConcurrency(),
		Prefork:      conf.GetHTTPServerPreforkEnabled(),
	})

	if conf.GetHTTPServerLoggerEnabled() {
		fiberlog.Debug("logging middleware is enabled.")
		app.Use(logger.New())
	}

	if conf.GetHTTPServerCORSEnabled() {
		fiberlog.Debug("cors middleware is enabled.")
		corsConfig := cors.Config{
			AllowMethods: strings.Join(conf.GetHTTPServerCORSAllowedMethods(), ","),
			AllowHeaders: strings.Join(conf.GetHTTPServerCORSAllowedHeaders(), ","),
			AllowOrigins: strings.Join(conf.GetHTTPServerCORSAllowedOrigins(), ","),
			MaxAge:       conf.GetHTTPServerCORSMaxAge(),
		}
		app.Use(cors.New(corsConfig))
	}

	if conf.GetHTTPServerHealthcheckEnabled() {
		fiberlog.Debug("healthcheck middleware is enabled.")
		healthcheckConfig := healthcheck.Config{
			LivenessProbe:     func(c *fiber.Ctx) bool { return true },
			ReadinessProbe:    func(c *fiber.Ctx) bool { return true },
			ReadinessEndpoint: conf.GetHTTPServerHealthcheckReadinessEndpoint(),
			LivenessEndpoint:  conf.GetHTTPServerHealthcheckLivenessEndpoint(),
		}
		app.Use(healthcheck.New(healthcheckConfig))
	}

	app.Use(recover.New())
	app.Use(requestid.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	if err := app.Listen(conf.GetHTTPServerAddress()); err != nil {
		return errors.Join(errors.New("fiber server stopped"), err)
	}
	return nil
}
