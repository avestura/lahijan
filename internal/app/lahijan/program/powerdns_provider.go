// Package program: powerdns_provider.go wires the PowerDNS driver (WS-12)
// into process bootstrap. Mirrors the buildXxxDeps pattern used by the auth
// + jobs + wasm + incus subsystems. The provider is built when
// conf.providers.powerdns.enabled is true; otherwise this file returns a
// zero-value deps bundle and the DNS module (WS-15) degrades to 501.
package program

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	fiberlog "github.com/gofiber/fiber/v2/log"
)

// powerdnsDeps bundles the WS-12 PowerDNS driver dependencies built at
// bootstrap. Every field is nil-appropriate: when
// conf.providers.powerdns.enabled is false the bundle is zero-value, the
// DNS module (WS-15) degrades to 501, and no driver is built.
type powerdnsDeps struct {
	provider *powerdns.Provider
}

// powerdnsBusAdapter adapts a *eventbus.Bus to the powerdns.EventBus
// interface so the driver does not import the eventbus package directly
// (mirrors the incus busAdapter from WS-11).
type powerdnsBusAdapter struct {
	bus *eventbus.Bus
}

// Emit implements powerdns.EventBus.
func (b *powerdnsBusAdapter) Emit(ctx context.Context, e powerdns.BusEvent) error {
	if b == nil || b.bus == nil {
		return nil
	}
	ev := eventbus.Event{
		Topic:     e.Topic,
		ActorType: e.ActorType,
		Metadata:  e.Metadata,
		EmittedAt: time.Now(),
	}
	if e.ResourceID != nil {
		// ResourceID for PDNS events is the canonical zone id (a DNS
		// name like "example.com."), NOT a UUID. The eventbus stores it
		// as *uuid.UUID; we leave ResourceID nil when the string is not
		// a UUID so the bus tolerates the non-UUID shape.
		if id, err := uuidParse(*e.ResourceID); err == nil {
			ev.ResourceID = &id
		}
	}
	if e.TenantID != nil {
		if id, err := uuidParse(*e.TenantID); err == nil {
			ev.TenantID = &id
		}
	}
	return b.bus.Emit(ctx, ev)
}

// uuidParse is a tiny alias for uuid.Parse so this file does not import the
// uuid package directly when only the adapter uses it (kept here so the
// import block stays tidy).
var uuidParse = parseUUID

// buildPowerdnsDeps wires the PowerDNS driver from config. Returns an
// empty powerdnsDeps when providers.powerdns.enabled is false so the DNS
// module degrades cleanly. The events synthesis path is enabled when both
// providers.powerdns.events.enabled AND the WASM event bus are non-nil.
//
// Failures here are loud (log.Fatalf) because a misconfigured API key the
// deployer turned on should not silently degrade to "no DNS" at runtime.
func buildPowerdnsDeps(_ context.Context, bus *eventbus.Bus) (powerdnsDeps, error) {
	if !conf.GetProvidersPowerDNSEnabled() {
		fiberlog.Debug("powerdns provider is disabled; skipping driver setup")
		return powerdnsDeps{}, nil
	}

	apiKey := conf.GetProvidersPowerDNSAPIKey()
	if apiKey == "" {
		return powerdnsDeps{}, errors.New("providers.powerdns.apiKey must be set when providers.powerdns.enabled is true")
	}

	var busAdapter powerdns.EventBus
	if conf.GetProvidersPowerDNSEventsEnabled() && bus != nil {
		busAdapter = &powerdnsBusAdapter{bus: bus}
	}

	provider, err := powerdns.NewClient(powerdns.Config{
		HTTPClient: &http.Client{
			Timeout: time.Duration(conf.GetProvidersPowerDNSRequestTimeoutSeconds()) * time.Second,
		},
		BaseURL: conf.GetProvidersPowerDNSBaseURL(),
		APIKey:  apiKey,
		Bus:     busAdapter,
	})
	if err != nil {
		return powerdnsDeps{}, errors.Join(errors.New("build powerdns client"), err)
	}

	// Ping the daemon once at startup so a broken daemon fails fast.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pingCancel()
	if err := provider.Ping(pingCtx); err != nil {
		// A failed Ping is a warning, not a fatal — the operator may
		// bring the daemon up after Lahijan (e.g. sidecar ordering).
		// The DNS module's healthz will surface the outage.
		fiberlog.Warn("powerdns provider ping failed at startup; will retry lazily",
			"error", err.Error())
	} else {
		caps := provider.Capabilities()
		fiberlog.Info("powerdns provider connected",
			"daemon_type", caps.DaemonType,
			"server_version", caps.ServerVersion)
	}

	return powerdnsDeps{provider: provider}, nil
}
