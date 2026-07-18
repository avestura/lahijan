// Package program: incus_provider.go wires the Incus driver (WS-11) into
// process bootstrap. Mirrors the buildXxxDeps pattern used by the auth + jobs
// + wasm subsystems. The provider is built when conf.providers.incus.enabled
// is true; otherwise this file returns a zero-value deps bundle and the
// compute module (WS-14) degrades to 501.
package program

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	fiberlog "github.com/gofiber/fiber/v2/log"
	"github.com/google/uuid"
)

// incusDeps bundles the WS-11 Incus driver dependencies built at bootstrap.
// Every field is nil-appropriate: when conf.providers.incus.enabled is false
// the bundle is zero-value, the compute module (WS-14) degrades to 501, and
// no event listener is started.
type incusDeps struct {
	provider *incus.Provider
	listener *incus.EventListener
}

// busAdapter adapts a *eventbus.Bus to the incus.EventBus interface so the
// driver does not import the eventbus package directly (avoiding a WS-10b
// dep for tests of WS-11). The adapter reshapes incus.BusEvent into the
// eventbus.Event shape.
type busAdapter struct {
	bus *eventbus.Bus
}

// Emit implements incus.EventBus.
func (b *busAdapter) Emit(ctx context.Context, e incus.BusEvent) error {
	if b == nil || b.bus == nil {
		return nil
	}
	ev := eventbus.Event{
		Topic:     e.Topic,
		ActorType: e.ActorType,
		Metadata:  e.Metadata,
		EmittedAt: time.Now(),
	}
	if e.TenantID != nil {
		// e.TenantID is the tenant UUID string; the eventbus stores it as
		// *uuid.UUID. Parse it here.
		id, err := parseUUID(*e.TenantID)
		if err == nil {
			ev.TenantID = &id
		}
	}
	if e.ResourceID != nil {
		ev.ResourceID = parseUUIDPtr(*e.ResourceID)
	}
	return b.bus.Emit(ctx, ev)
}

// buildIncusDeps wires the Incus driver + (optional) events listener from
// config. Returns an empty incusDeps when providers.incus.enabled is false so
// the compute module degrades cleanly. The events listener is only started
// when both providers.incus.enabled AND providers.incus.events.enabled are
// true AND the WASM event bus is non-nil.
func buildIncusDeps(_ context.Context, bus *eventbus.Bus) (incusDeps, error) {
	if !conf.GetProvidersIncusEnabled() {
		fiberlog.Debug("incus provider is disabled; skipping driver setup")
		return incusDeps{}, nil
	}

	features := conf.GetProvidersIncusProjectFeatures()
	cfg := incus.Config{
		RequestTimeout: time.Duration(conf.GetProvidersIncusRequestTimeoutSeconds()) * time.Second,
		ProjectPrefix:  conf.GetProvidersIncusProjectPrefix(),
		ProjectFeatures: incus.ProjectFeatures{
			Images:         features.Images,
			Profiles:       features.Profiles,
			Networks:       features.Networks,
			StorageVolumes: features.StorageVolumes,
			StorageBuckets: features.StorageBuckets,
		},
	}

	var provider *incus.Provider
	var err error
	if remoteURL := conf.GetProvidersIncusRemoteURL(); remoteURL != "" {
		tlsCfg := conf.GetProvidersIncusTLS()
		provider, err = incus.NewRemoteClient(remoteURL, incus.TLSConfig{
			ServerCert:         tlsCfg.ServerCert,
			ClientCert:         tlsCfg.ClientCert,
			ClientKey:          tlsCfg.ClientKey,
			InsecureSkipVerify: tlsCfg.InsecureSkipVerify,
		}, cfg)
		if err != nil {
			return incusDeps{}, errors.Join(errors.New("build incus remote client"), err)
		}
	} else {
		socketPath := conf.GetProvidersIncusSocketPath()
		provider, err = incus.NewUnixClient(socketPath, cfg)
		if err != nil {
			return incusDeps{}, errors.Join(errors.New("build incus unix client"), err)
		}
	}

	// Ping the daemon once at startup so a broken daemon fails fast.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pingCancel()
	if err := provider.Ping(pingCtx); err != nil {
		// A failed Ping is a warning, not a fatal — the operator may bring
		// the daemon up after Lahijan (e.g. sidecar ordering). The
		// compute module's healthz will surface the outage.
		fiberlog.Warn("incus provider ping failed at startup; will retry lazily",
			"error", err.Error())
	} else {
		caps := provider.Capabilities()
		fiberlog.Info("incus provider connected",
			"cluster_mode", caps.ClusterMode,
			"server_version", caps.ServerVersion,
			"server_name", caps.ServerName)
	}

	deps := incusDeps{provider: provider}

	// Optionally start the events listener.
	if conf.GetProvidersIncusEventsEnabled() {
		var adapter incus.EventBus
		if bus != nil {
			adapter = &busAdapter{bus: bus}
		}
		listener, lerr := provider.StartEventListener(incus.EventListenerConfig{
			Bus:             adapter,
			MaxReconnect:    time.Duration(conf.GetProvidersIncusEventsMaxReconnectSeconds()) * time.Second,
			MaxPayloadBytes: conf.GetProvidersIncusEventsMaxPayloadBytes(),
			Logger:          slog.Default(),
		})
		if lerr != nil {
			fiberlog.Warn("incus events listener failed to start",
				"error", lerr.Error())
		} else {
			deps.listener = listener
		}
	}

	return deps, nil
}

// parseUUID parses a tenant-id string into a uuid.UUID. Returns an error on
// malformed input; the caller treats the parse failure as "no tenant scope".
func parseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }

// parseUUIDPtr parses a resource-id string into a *uuid.UUID. Returns nil on
// malformed input — Incus source URLs sometimes carry non-UUID trailing
// segments (instance names), and the eventbus tolerates a nil ResourceID.
func parseUUIDPtr(s string) *uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}
