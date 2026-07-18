package marketplace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
)

// Service is the marketplace entrypoint the admin API talks to. It owns
// the IndexLoader (cache + parse) and delegates install + upgrade to
// the existing installer.Service, so the audit emission, permission
// enforcement, and lifecycle hooks all stay in one place.
//
// Construct one at bootstrap and share across requests. Methods are
// safe for concurrent use (they delegate to thread-safe loaders + the
// installer's own concurrency-safe methods).
type Service struct {
	idx     IndexLoader
	assets  AssetLoader
	install *installer.Service
	plugins *database.PluginsRepository
	emitter audit.Emitter
	log     *slog.Logger
}

// New builds a Service. installer may be nil when the WASM subsystem
// is disabled; List will still work, Install + Upgrade will return
// ErrMarketplaceDisabled. emitter is required for audit emission but
// defaults to NoopEmitter when nil.
func New(
	idx IndexLoader,
	assets AssetLoader,
	installSvc *installer.Service,
	plugins *database.PluginsRepository,
	emitter audit.Emitter,
	log *slog.Logger,
) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		idx:     idx,
		assets:  assets,
		install: installSvc,
		plugins: plugins,
		emitter: emitter,
		log:     log,
	}
}

// ErrMarketplaceDisabled is returned when the WASM subsystem is off
// (conf.wasm.enabled=false). The api handler translates it to a 501.
var ErrMarketplaceDisabled = errors.New("marketplace: WASM subsystem is disabled")

// ErrNotFound is returned by Get + Install when the requested name does
// not appear in the marketplace index.
var ErrNotFound = errors.New("marketplace: plugin not in index")

// List returns the marketplace index. The marketplace API renders this
// verbatim as the response body of GET /api/v1/admin/marketplace.
func (s *Service) List(ctx context.Context) ([]Entry, error) {
	if s.idx == nil {
		return nil, ErrMarketplaceDisabled
	}
	idx, err := s.idx.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("marketplace: load index: %w", err)
	}
	return idx.Plugins, nil
}

// Get returns a single entry by name. Returns ErrNotFound when the name
// is not in the index.
func (s *Service) Get(ctx context.Context, name string) (Entry, error) {
	if s.idx == nil {
		return Entry{}, ErrMarketplaceDisabled
	}
	idx, err := s.idx.Load(ctx)
	if err != nil {
		return Entry{}, fmt.Errorf("marketplace: load index: %w", err)
	}
	for _, e := range idx.Plugins {
		if e.Name == name {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// InstallParams carries the caller context the installer needs to emit
// a proper audit row. TenantID is the install scope (nil for platform-
// wide plugins). ActorUserID is the admin performing the install.
type InstallParams struct {
	TenantID    *uuid.UUID
	ActorUserID uuid.UUID
	RequestID   *string
}

// Install fetches the plugin from the marketplace and runs it through
// the standard installer.Service.Upload path. The plugin lands in
// "pending" status; the admin must grant at least one permission (or
// call /enable directly) before the runtime will instantiate it.
//
// Returns ErrDuplicateUpload when a plugin with the same (name, version)
// already exists; the admin should call Upgrade instead.
func (s *Service) Install(ctx context.Context, name string, arg InstallParams) (database.Plugin, error) {
	if s.install == nil || s.plugins == nil {
		return database.Plugin{}, ErrMarketplaceDisabled
	}
	entry, err := s.Get(ctx, name)
	if err != nil {
		return database.Plugin{}, err
	}
	asset, err := s.assets.Fetch(ctx, entry)
	if err != nil {
		return database.Plugin{}, fmt.Errorf("marketplace: fetch %s: %w", name, err)
	}
	m, err := manifest.Parse(asset.ManifestYAML)
	if err != nil {
		return database.Plugin{}, fmt.Errorf("marketplace: parse manifest: %w", err)
	}
	row, err := s.install.Upload(ctx, installer.UploadParams{
		TenantID:    arg.TenantID,
		ActorUserID: arg.ActorUserID,
		WasmBytes:   asset.WasmBytes,
		Manifest:    m,
		RequestID:   arg.RequestID,
	})
	if err != nil {
		// A prior installation of the same name surfaces as
		// ErrDuplicateUpload. The marketplace caller is expected to
		// switch to the Upgrade flow when the admin POSTs to
		// /install/{name} for a plugin that's already present.
		return database.Plugin{}, err
	}
	_, _ = s.emitter.Emit(ctx, audit.Event{
		TenantID:     arg.TenantID,
		ActorUserID:  &arg.ActorUserID,
		ActorType:    audit.ActorUser,
		Action:       ActionInstall,
		ResourceType: audit.ResourcePlugin,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		RequestID:    arg.RequestID,
		Metadata: map[string]any{
			"name":    row.Name,
			"version": row.Version,
			"source":  entry.Source.Repo,
		},
	})
	return row, nil
}

// Upgrade fetches the marketplace version of an already-installed plugin
// and runs it through installer.Service.Upgrade. The admin is expected
// to call this only when a prior Install exists; otherwise the call
// returns installer.ErrNotInstalled (translated by the api layer to
// 404 + a hint to use Install).
//
// The returned UpgradeResult carries the NewPermissions list so the
// admin can grant each one (per WS-10c DoD "upgrade adds a permission
// → admin is prompted to grant it"). Preserved grants are carried over
// silently; dropped grants are surfaced in the response so the admin
// sees what was lost.
func (s *Service) Upgrade(ctx context.Context, name string, arg InstallParams) (installer.UpgradeResult, error) {
	if s.install == nil || s.plugins == nil {
		return installer.UpgradeResult{}, ErrMarketplaceDisabled
	}
	entry, err := s.Get(ctx, name)
	if err != nil {
		return installer.UpgradeResult{}, err
	}
	asset, err := s.assets.Fetch(ctx, entry)
	if err != nil {
		return installer.UpgradeResult{}, fmt.Errorf("marketplace: fetch %s: %w", name, err)
	}
	m, err := manifest.Parse(asset.ManifestYAML)
	if err != nil {
		return installer.UpgradeResult{}, fmt.Errorf("marketplace: parse manifest: %w", err)
	}
	res, err := s.install.Upgrade(ctx, installer.UploadParams{
		TenantID:    arg.TenantID,
		ActorUserID: arg.ActorUserID,
		WasmBytes:   asset.WasmBytes,
		Manifest:    m,
		RequestID:   arg.RequestID,
	})
	if err != nil {
		return installer.UpgradeResult{}, err
	}
	_, _ = s.emitter.Emit(ctx, audit.Event{
		TenantID:     arg.TenantID,
		ActorUserID:  &arg.ActorUserID,
		ActorType:    audit.ActorUser,
		Action:       ActionUpgrade,
		ResourceType: audit.ResourcePlugin,
		ResourceID:   &res.New.ID,
		Status:       audit.StatusSuccess,
		RequestID:    arg.RequestID,
		Metadata: map[string]any{
			"name":            res.New.Name,
			"old_id":          res.OldID.String(),
			"new_version":     res.New.Version,
			"new_permissions": res.NewPermissions,
		},
	})
	return res, nil
}

// ActionInstall is the audit action slug for marketplace installs.
// Distinct from plugins.upload (direct admin upload) so the admin UI
// can distinguish "installed from marketplace" vs "uploaded from disk".
const ActionInstall = "plugins.marketplace_install"

// ActionUpgrade is the audit action slug for marketplace upgrades.
const ActionUpgrade = "plugins.marketplace_upgrade"
