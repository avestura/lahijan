// Package installer is the service-layer seam the admin plugin API talks to
// (WS-10a). It owns the upload → parse → validate → persist → compile flow
// and the grant/revoke/enable/disable actions. Every state-changing method
// calls the supplied audit.Emitter and the permission checks are explicit
// (handled in the API layer via RequirePerm).
//
// Layering:
//
//	api (admin_plugins_handlers.go)
//	  → installer.Service        ← this file
//	    → database.PluginsRepository
//	    → wasm.Runtime           (Compile, Instantiate for the verification step)
//	    → wasm.manifest.Parser
//	    → audit.Emitter          (one event per state-changing action)
//
// The installer NEVER calls rbac.RequirePerm itself; the API handler does
// that (per the conventions.md layering rule). The installer is the seam a
// future CLI / webhook would also call, so it must be policy-agnostic.
package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

// Audit action constants. These are emitted by the installer's methods and
// rendered via i18n keys "audit.action_<slug-with-dots-as-underscores>".
const (
	ActionUpload  = "plugins.upload"
	ActionInstall = "plugins.install"
	ActionGrant   = "plugins.grant"
	ActionRevoke  = "plugins.revoke"
	ActionEnable  = "plugins.enable"
	ActionDisable = "plugins.disable"
	ActionDelete  = "plugins.delete"
)

// ResourcePlugin is the resource_type recorded on audit rows for plugin events.
const ResourcePlugin = "plugin"

// ErrManifestInvalid is returned by Upload when the manifest fails Validate.
// Wraps the underlying error so the API layer can render the joined messages.
var ErrManifestInvalid = errors.New("installer: manifest invalid")

// ErrModuleRejected is returned by Upload when the wasm runtime rejects the
// module (too much memory, invalid bytes, etc).
var ErrModuleRejected = errors.New("installer: wasm module rejected")

// ErrNotFound is returned by every method that looks up a plugin by id.
var ErrNotFound = errors.New("installer: plugin not found")

// ErrDuplicateUpload is returned by Upload when (tenant, name, version)
// already exists. The caller renders a 409 Conflict envelope.
var ErrDuplicateUpload = errors.New("installer: plugin with this (name, version) already exists")

// Service is the install + lifecycle seam. Construct one at bootstrap and
// share it across requests. Methods are safe for concurrent use (they
// delegate to *database.PluginsRepository which is safe).
type Service struct {
	repo    *database.PluginsRepository
	rt      *runtime.Runtime
	emitter audit.Emitter
}

// New builds a Service. The runtime may be nil when WS-10a's runtime is
// disabled (admin API degrades to 501 per the api layer's nil check) — but
// when non-nil, Upload will additionally verify the module compiles before
// persisting. The emitter may be audit.NoopEmitter when tests don't care.
func New(repo *database.PluginsRepository, rt *runtime.Runtime, emitter audit.Emitter) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	return &Service{repo: repo, rt: rt, emitter: emitter}
}

// UploadParams carries the inputs to Upload. TenantID is the row's
// tenant_id (nil for platform-wide plugins). ActorUserID is the admin
// performing the upload; recorded on the audit row.
type UploadParams struct {
	TenantID    *uuid.UUID
	ActorUserID uuid.UUID
	Name        string // override from manifest when non-empty
	WasmBytes   []byte
	Manifest    *manifest.Manifest
	RequestID   *string
}

// Upload parses + validates the manifest, verifies the wasm compiles under
// the configured limits, persists a new plugin row in "pending" status, and
// emits an audit event. Returns the new plugin row.
//
// The plugin is NOT active after Upload. The admin must Grant at least one
// permission AND call Enable (or just call Enable directly when no
// permissions are needed) before the runtime will instantiate it.
func (s *Service) Upload(ctx context.Context, arg UploadParams) (database.Plugin, error) {
	if arg.Manifest == nil {
		return database.Plugin{}, fmt.Errorf("installer: manifest is required: %w", ErrManifestInvalid)
	}
	if len(arg.WasmBytes) == 0 {
		return database.Plugin{}, fmt.Errorf("installer: empty wasm bytes: %w", ErrModuleRejected)
	}
	// Manifest was already validated upstream (api handler calls
	// manifest.Parse), but re-running Validate is cheap and keeps this
	// method safe to call from non-API paths (CLI, webhook).
	if err := arg.Manifest.Validate(); err != nil {
		return database.Plugin{}, fmt.Errorf("installer: %w: %v", ErrManifestInvalid, err) //nolint:errorlint // joining two sentinels intentionally
	}

	// Compile (or at least verify the runtime accepts the module) BEFORE
	// persisting so we never store a plugin the runtime cannot load. When
	// the runtime is nil (WS-10a's conf.wasm.enabled=false path) we still
	// persist — the runtime will compile on first instantiation.
	if s.rt != nil {
		if _, err := s.rt.Compile(ctx, arg.WasmBytes, arg.Manifest); err != nil {
			return database.Plugin{}, fmt.Errorf("installer: %w: %v", ErrModuleRejected, err) //nolint:errorlint // joining two sentinels intentionally
		}
	}

	sum := sha256.Sum256(arg.WasmBytes)
	hash := hex.EncodeToString(sum[:])
	manifestJSON, err := arg.Manifest.MarshalJSON()
	if err != nil {
		return database.Plugin{}, fmt.Errorf("installer: marshal manifest: %w", err)
	}

	row, err := s.repo.Create(ctx, database.CreatePluginParams{
		TenantID:    arg.TenantID,
		Name:        arg.Manifest.Name,
		Version:     arg.Manifest.Version,
		Description: arg.Manifest.Description,
		WasmHash:    hash,
		WasmBytes:   arg.WasmBytes,
		WasmSize:    int64(len(arg.WasmBytes)),
		Manifest:    manifestJSON,
		Status:      database.PluginStatusPending,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return database.Plugin{}, ErrDuplicateUpload
		}
		return database.Plugin{}, fmt.Errorf("installer: persist plugin: %w", err)
	}

	s.emit(ctx, Event{
		Action: ActionUpload,
		Tenant: arg.TenantID, Actor: arg.ActorUserID, RequestID: arg.RequestID,
		PluginID: row.ID, Status: audit.StatusSuccess,
		Details: map[string]any{
			"name":    row.Name,
			"version": row.Version,
			"hash":    hash,
		},
	})
	return row, nil
}

// Grant adds a permission to a plugin. permission.Validate is run first so
// an admin cannot grant a slug the host functions do not understand.
func (s *Service) Grant(
	ctx context.Context,
	pluginID, actorUserID uuid.UUID,
	perm string,
	tenantID *uuid.UUID,
	requestID *string,
) error {
	if err := permission.Validate(perm); err != nil {
		return fmt.Errorf("installer: %w: %v", ErrManifestInvalid, err) //nolint:errorlint // joining two sentinels intentionally
	}
	if _, err := s.repo.Get(ctx, pluginID); err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("installer: lookup plugin: %w", err)
	}
	if err := s.repo.GrantPermission(ctx, pluginID, actorUserID, perm); err != nil {
		return fmt.Errorf("installer: grant %s: %w", perm, err)
	}
	s.emit(ctx, Event{
		Action: ActionGrant,
		Tenant: tenantID, Actor: actorUserID, RequestID: requestID,
		PluginID: pluginID, Status: audit.StatusSuccess,
		Details: map[string]any{"permission": perm},
	})
	return nil
}

// Revoke removes a permission. The plugin is responsible for noticing
// (the enforcer is called on every host call; a revoked permission fails
// closed on the next call).
func (s *Service) Revoke(
	ctx context.Context,
	pluginID, actorUserID uuid.UUID,
	perm string,
	tenantID *uuid.UUID,
	requestID *string,
) error {
	if _, err := s.repo.Get(ctx, pluginID); err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("installer: lookup plugin: %w", err)
	}
	if err := s.repo.RevokePermission(ctx, pluginID, perm); err != nil {
		return fmt.Errorf("installer: revoke %s: %w", perm, err)
	}
	s.emit(ctx, Event{
		Action: ActionRevoke,
		Tenant: tenantID, Actor: actorUserID, RequestID: requestID,
		PluginID: pluginID, Status: audit.StatusSuccess,
		Details: map[string]any{"permission": perm},
	})
	return nil
}

// Enable flips a plugin's status to "active". The plugin must already be
// persisted (Upload). Grants are NOT modified; an enabled plugin with zero
// grants loads but every host call is denied.
func (s *Service) Enable(
	ctx context.Context,
	pluginID, actorUserID uuid.UUID,
	tenantID *uuid.UUID,
	requestID *string,
) error {
	return s.setStatus(ctx, pluginID, actorUserID, database.PluginStatusActive, ActionEnable, tenantID, requestID)
}

// Disable flips a plugin's status to "disabled". Grants persist; Enable
// picks them back up.
func (s *Service) Disable(
	ctx context.Context,
	pluginID, actorUserID uuid.UUID,
	tenantID *uuid.UUID,
	requestID *string,
) error {
	return s.setStatus(ctx, pluginID, actorUserID, database.PluginStatusDisabled, ActionDisable, tenantID, requestID)
}

func (s *Service) setStatus(
	ctx context.Context,
	pluginID, actorUserID uuid.UUID,
	status, action string,
	tenantID *uuid.UUID,
	requestID *string,
) error {
	if _, err := s.repo.Get(ctx, pluginID); err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("installer: lookup plugin: %w", err)
	}
	if err := s.repo.SetStatus(ctx, pluginID, status); err != nil {
		return fmt.Errorf("installer: set status %s: %w", status, err)
	}
	s.emit(ctx, Event{
		Action: action,
		Tenant: tenantID, Actor: actorUserID, RequestID: requestID,
		PluginID: pluginID, Status: audit.StatusSuccess,
		Details: map[string]any{"new_status": status},
	})
	return nil
}

// Delete hard-deletes the plugin row. CASCADE removes its grants.
func (s *Service) Delete(
	ctx context.Context,
	pluginID, actorUserID uuid.UUID,
	tenantID *uuid.UUID,
	requestID *string,
) error {
	if _, err := s.repo.Get(ctx, pluginID); err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("installer: lookup plugin: %w", err)
	}
	if err := s.repo.Delete(ctx, pluginID); err != nil {
		return fmt.Errorf("installer: delete plugin: %w", err)
	}
	s.emit(ctx, Event{
		Action: ActionDelete,
		Tenant: tenantID, Actor: actorUserID, RequestID: requestID,
		PluginID: pluginID, Status: audit.StatusSuccess,
	})
	return nil
}

// Event is the installer's audit payload. The installer translates it to an
// audit.Event before emitting; this indirection exists so the installer
// doesn't import audit's full shape (and tests can capture Events easily).
type Event struct {
	Action    string
	Tenant    *uuid.UUID
	Actor     uuid.UUID
	RequestID *string
	PluginID  uuid.UUID
	Status    string
	Details   map[string]any
}

// emit is the local audit helper. Failures are logged via the emitter but
// never block the caller (per pillar 7); the action has already happened.
func (s *Service) emit(ctx context.Context, e Event) {
	_, _ = s.emitter.Emit(ctx, audit.Event{
		TenantID:     e.Tenant,
		ActorUserID:  actorPtr(e.Actor),
		ActorType:    audit.ActorUser,
		Action:       e.Action,
		ResourceType: ResourcePlugin,
		ResourceID:   &e.PluginID,
		Status:       e.Status,
		RequestID:    e.RequestID,
		Metadata:     e.Details,
	})
}

// actorPtr returns &id when id is non-nil. Used so audit rows have a
// proper pointer even when the actor is the system (uuid.Nil).
func actorPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
