// Package program: seaweedfs_provider.go wires the SeaweedFS driver (WS-13)
// into process bootstrap. Mirrors the buildXxxDeps pattern used by the auth
// + jobs + wasm + incus + powerdns subsystems. The provider is built when
// conf.providers.seaweedfs.enabled is true; otherwise this file returns a
// zero-value deps bundle and the storage module (WS-16) degrades to 501.
package program

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	fiberlog "github.com/gofiber/fiber/v2/log"
)

// seaweedfsDeps bundles the WS-13 SeaweedFS driver dependencies built at
// bootstrap. Every field is nil-appropriate: when
// conf.providers.seaweedfs.enabled is false the bundle is zero-value,
// the storage module (WS-16) degrades to 501, and no driver is built.
type seaweedfsDeps struct {
	provider *seaweedfs.Provider
}

// seaweedfsBusAdapter adapts a *eventbus.Bus to the seaweedfs.EventBus
// interface so the driver does not import the eventbus package directly
// (mirrors the incus + powerdns busAdapters).
type seaweedfsBusAdapter struct {
	bus *eventbus.Bus
}

// Emit implements seaweedfs.EventBus.
func (b *seaweedfsBusAdapter) Emit(ctx context.Context, e seaweedfs.BusEvent) error {
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
		// ResourceID for SeaweedFS events is a bucket name or an access
		// key — both strings, not necessarily UUIDs. The eventbus stores
		// ResourceID as *uuid.UUID; we leave it nil when the value is
		// not a UUID so the bus tolerates the non-UUID shape.
		if id, err := parseUUID(*e.ResourceID); err == nil {
			ev.ResourceID = &id
		}
	}
	if e.TenantID != nil {
		if id, err := parseUUID(*e.TenantID); err == nil {
			ev.TenantID = &id
		}
	}
	return b.bus.Emit(ctx, ev)
}

// buildSeaweedfsDeps wires the SeaweedFS driver from config. Returns an
// empty seaweedfsDeps when providers.seaweedfs.enabled is false so the
// storage module degrades cleanly. The events synthesis path is
// enabled when both providers.seaweedfs.events.enabled AND the WASM
// event bus are non-nil.
//
// Failures here are loud (log.Fatalf) because a misconfigured admin
// credential the deployer turned on should not silently degrade to "no
// storage" at runtime.
func buildSeaweedfsDeps(_ context.Context, bus *eventbus.Bus) (seaweedfsDeps, error) {
	if !conf.GetProvidersSeaweedFSEnabled() {
		fiberlog.Debug("seaweedfs provider is disabled; skipping driver setup")
		return seaweedfsDeps{}, nil
	}

	accessKey := conf.GetProvidersSeaweedFSAdminAccessKey()
	if accessKey == "" {
		return seaweedfsDeps{}, errors.New("providers.seaweedfs.adminAccessKey must be set when providers.seaweedfs.enabled is true")
	}
	secretKey := conf.GetProvidersSeaweedFSAdminSecretKey()
	if secretKey == "" {
		return seaweedfsDeps{}, errors.New("providers.seaweedfs.adminSecretKey must be set when providers.seaweedfs.enabled is true")
	}

	var busAdapter seaweedfs.EventBus
	if conf.GetProvidersSeaweedFSEventsEnabled() && bus != nil {
		busAdapter = &seaweedfsBusAdapter{bus: bus}
	}

	provider, err := seaweedfs.NewClient(seaweedfs.Config{
		HTTPClient: &http.Client{
			Timeout: time.Duration(conf.GetProvidersSeaweedFSRequestTimeoutSeconds()) * time.Second,
		},
		S3Endpoint:        conf.GetProvidersSeaweedFSS3Endpoint(),
		FilerURL:          conf.GetProvidersSeaweedFSFilerURL(),
		Region:            conf.GetProvidersSeaweedFSRegion(),
		AdminAccessKey:    accessKey,
		AdminSecretKey:    secretKey,
		RequestTimeout:    time.Duration(conf.GetProvidersSeaweedFSRequestTimeoutSeconds()) * time.Second,
		DefaultPresignTTL: time.Duration(conf.GetProvidersSeaweedFSDefaultPresignTTLSeconds()) * time.Second,
		DefaultQuota: seaweedfs.QuotaSpec{
			SizeMiB:   conf.GetProvidersSeaweedFSDefaultQuotaMiB(),
			FileCount: 0,
		},
		Bus: busAdapter,
	})
	if err != nil {
		return seaweedfsDeps{}, errors.Join(errors.New("build seaweedfs client"), err)
	}

	// Ping the backend once at startup so a broken daemon fails fast.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pingCancel()
	if err := provider.Ping(pingCtx); err != nil {
		// A failed Ping is a warning, not a fatal — the operator may
		// bring the daemon up after Lahijan (e.g. sidecar ordering).
		// The storage module's healthz will surface the outage.
		fiberlog.Warn("seaweedfs provider ping failed at startup; will retry lazily",
			"error", err.Error())
	} else {
		caps := provider.Capabilities()
		fiberlog.Info("seaweedfs provider connected",
			"cluster_mode", caps.ClusterMode,
			"server_version", caps.ServerVersion,
			"quotas_enforced", caps.QuotasEnforced)
	}

	return seaweedfsDeps{provider: provider}, nil
}
