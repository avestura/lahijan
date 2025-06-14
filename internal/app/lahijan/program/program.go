package program

import (
	"log"
	"strings"

	_ "go.uber.org/automaxprocs"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	computeddefault "github.com/avestura/lahijan/internal/app/lahijan/conf/computedDefault"
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
		Concurrency:  conf.GetHttpServerConcurrency(),
		Prefork:      conf.GetHttpServerPreforkEnabled(),
	})

	if conf.GetHttpServerLoggerEnabled() {
		fiberlog.Debug("logging middleware is enabled.")
		app.Use(logger.New())
	}

	if conf.GetHttpServerCorsEnabled() {
		fiberlog.Debug("cors middleware is enabled.")
		corsConfig := cors.Config{
			AllowMethods: strings.Join(conf.GetHttpServerCorsAllowedMethods(), ","),
			AllowHeaders: strings.Join(conf.GetHttpServerCorsAllowedHeaders(), ","),
			AllowOrigins: strings.Join(conf.GetHttpServerCorsAllowedOrigins(), ","),
			MaxAge:       conf.GetHttpServerCorsMaxAge(),
		}
		app.Use(cors.New(corsConfig))
	}

	if conf.GetHttpServerHealthcheckEnabled() {
		fiberlog.Debug("healthcheck middleware is enabled.")
		healthcheckConfig := healthcheck.Config{
			LivenessProbe:     func(c *fiber.Ctx) bool { return true },
			ReadinessProbe:    func(c *fiber.Ctx) bool { return true },
			ReadinessEndpoint: conf.GetHttpServerHealthcheckReadinessEndpoint(),
			LivenessEndpoint:  conf.GetHttpServerHealthcheckLivenessEndpoint(),
		}
		app.Use(healthcheck.New(healthcheckConfig))
	}

	app.Use(recover.New())
	app.Use(requestid.New())

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	if err := app.Listen(conf.GetHttpServerAddress()); err != nil {
		fiberlog.Errorf("failed to start server: %w", err.Error())
		return err
	}
	return nil
}
