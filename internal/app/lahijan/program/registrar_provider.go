// Package program: registrar_provider.go wires the registrar driver
// (WS-28) into process bootstrap. Mirrors the buildXxxDeps pattern used
// by the auth + jobs + wasm + powerdns subsystems. The provider is
// built when conf.providers.registrar.enabled is true; otherwise this
// file returns a noop driver and the registrar module degrades to 501.
package program

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar"
	fiberlog "github.com/gofiber/fiber/v2/log"
)

// registrarDeps bundles the WS-28 registrar driver dependencies built
// at bootstrap. provider is nil-appropriate: when
// conf.providers.registrar.enabled is false the bundle is zero-value,
// the registrar service (and /api/v1/dns/domains/*) degrades to 501,
// and no driver is built.
type registrarDeps struct {
	provider registrar.Provider
}

// buildRegistrarDeps wires the registrar driver from config. Returns an
// empty registrarDeps when providers.registrar.enabled is false so the
// consuming module degrades cleanly.
//
// Failures here are loud (log.Fatalf) because a misconfigured API key
// the deployer turned on should not silently degrade to "no registrar"
// at runtime.
func buildRegistrarDeps(_ context.Context) (registrarDeps, error) {
	if !conf.GetProvidersRegistrarEnabled() {
		fiberlog.Debug("registrar provider is disabled; skipping driver setup")
		return registrarDeps{}, nil
	}

	switch conf.GetProvidersRegistrarProvider() {
	case "opensrs":
		apiKey := conf.GetProvidersRegistrarOpenSRSAPIKey()
		if apiKey == "" {
			return registrarDeps{}, errors.New(
				"providers.registrar.openSRS.apiKey must be set when " +
					"providers.registrar.enabled is true and provider=opensrs",
			)
		}
		username := conf.GetProvidersRegistrarOpenSRSUsername()
		if username == "" {
			return registrarDeps{}, errors.New(
				"providers.registrar.openSRS.username must be set when " +
					"providers.registrar.enabled is true and provider=opensrs",
			)
		}
		p, err := registrar.NewOpenSRSProvider(registrar.OpenSRSConfig{
			HTTPClient: &http.Client{
				Timeout: time.Duration(conf.GetProvidersRegistrarOpenSRSRequestTimeoutSeconds()) * time.Second,
			},
			BaseURL:  conf.GetProvidersRegistrarOpenSRSBaseURL(),
			APIKey:   apiKey,
			Username: username,
		})
		if err != nil {
			return registrarDeps{}, errors.Join(errors.New("build opensrs registrar client"), err)
		}
		// Ping the registrar once at startup so a broken endpoint fails
		// fast. A failed Ping is a warning (the operator may bring the
		// registrar up after Lahijan).
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer pingCancel()
		if err := p.Ping(pingCtx); err != nil {
			fiberlog.Warn("registrar provider ping failed at startup; will retry lazily",
				"error", err.Error())
		} else {
			fiberlog.Info("registrar provider connected",
				"provider", p.Name())
		}
		return registrarDeps{provider: p}, nil
	case "noop", "":
		// Explicit noop: enabled is true but the deployer picked "noop"
		// so the wiring exercises the registrar service end-to-end
		// without a real backend (useful for tests).
		fiberlog.Info("registrar provider wired in noop mode")
		return registrarDeps{provider: registrar.NoopProvider{}}, nil
	default:
		provider := conf.GetProvidersRegistrarProvider()
		return registrarDeps{}, errors.New(
			"providers.registrar.provider " + provider +
				" is not supported (in-tree: opensrs, noop)",
		)
	}
}
