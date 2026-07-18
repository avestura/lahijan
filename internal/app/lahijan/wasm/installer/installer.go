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
	"strings"

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
	ActionUpgrade = "plugins.upgrade"
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

// ErrNotInstalled is returned by Upgrade when no prior version of the named
// plugin exists in the (tenant) scope. The caller renders a 404 envelope.
var ErrNotInstalled = errors.New("installer: plugin not installed (no prior version to upgrade)")

// ErrSameVersion is returned by Upgrade when the requested version equals
// the currently-installed version. The caller renders a 409 envelope.
var ErrSameVersion = errors.New("installer: plugin already at this version")

// ErrDowngrade is returned by Upgrade when the requested version is LOWER
// than the currently-installed one. Lahijan does not support transparent
// downgrades because the previous version's grants + state may have been
// migrated forward; the admin must Uninstall + Install instead.
var ErrDowngrade = errors.New("installer: downgrade not supported (uninstall + install instead)")

// Service is the install + lifecycle seam. Construct one at bootstrap and
// share it across requests. Methods are safe for concurrent use (they
// delegate to *database.PluginsRepository which is safe).
type Service struct {
	repo    *database.PluginsRepository
	rt      *runtime.Runtime
	emitter audit.Emitter
	// Optional side-channel repos for the Upgrade flow. Each is nil-
	// appropriate when the WS-10b tables are not wired; Upgrade degrades
	// to "best effort, no side cleanup" in that case.
	handlers     *database.PluginHTTPHandlersRepository
	subscriptions *database.PluginEventSubscriptionsRepository
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

// WithSideRepos attaches the WS-10b HTTP handler + event subscription
// repositories so Upgrade can clean up the prior version's mounts + subs
// atomically. Returns the receiver for chaining at bootstrap. Either
// argument may be nil; the corresponding cleanup step degrades to a no-op.
//
// The marketplace (WS-10c) wires this so the upgrade flow satisfies the
// DoD item "upgrade removes a permission → grant is dropped cleanly".
// Without these repos the upgrade still works but leaves orphan rows
// behind that CASCADE would catch at hard-delete time anyway.
func (s *Service) WithSideRepos(
	handlers *database.PluginHTTPHandlersRepository,
	subs *database.PluginEventSubscriptionsRepository,
) *Service {
	s.handlers = handlers
	s.subscriptions = subs
	return s
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

// UpgradeResult carries the outcome of an Upgrade call. The admin uses
// NewPermissions to decide whether to call Grant for each (the WS-10c
// DoD "upgrade adds a permission → admin is prompted to grant it").
type UpgradeResult struct {
	New             database.Plugin // the newly-installed row (status=pending)
	OldID           uuid.UUID       // the previously-installed row id (now hard-deleted)
	PreservedGrants []string        // grants carried forward from the prior version
	DroppedGrants   []string        // grants no longer requested by the new manifest
	NewPermissions  []string        // new manifest permissions the admin has not yet granted
}

// Upgrade replaces the existing plugin of the same name with a new
// version. The flow:
//
//  1. Locate the most recent prior version (FindByNameForTenant /
//     FindByNameGlobal depending on scope).
//  2. Verify the new version is strictly greater than the old
//     (semver compare). Reject same-version + downgrades.
//  3. Compile + persist the new version (status=pending) via the same
//     Upload path. Grants from the old version are copied across when
//     the new manifest still requests them (exact or wildcard match);
//     grants for permissions the new manifest no longer requests are
//     dropped. Brand-new permissions are returned in NewPermissions so
//     the admin can approve them.
//  4. Hard-delete the old row. CASCADE removes its remaining state
//     (plugin_kv, plugin_config, plugin_event_subscriptions,
//     plugin_http_handlers) atomically.
//
// The new plugin lands in "pending" status. The admin calls Enable
// (after granting any NewPermissions) to flip it to "active".
//
// The HTTP-handler + event-subscription repos attached via
// WithSideRepos are best-effort cleaned up BEFORE the CASCADE so an
// observer watching those tables sees the rows go away before the
// plugin row does. This is the WS-10c DoD item "removal cleans up event
// subscriptions, KV, HTTP routes".
func (s *Service) Upgrade(
	ctx context.Context,
	arg UploadParams,
) (UpgradeResult, error) {
	if arg.Manifest == nil {
		return UpgradeResult{}, fmt.Errorf("installer: manifest is required: %w", ErrManifestInvalid)
	}
	if len(arg.WasmBytes) == 0 {
		return UpgradeResult{}, fmt.Errorf("installer: empty wasm bytes: %w", ErrModuleRejected)
	}
	if err := arg.Manifest.Validate(); err != nil {
		return UpgradeResult{}, fmt.Errorf("installer: %w: %v", ErrManifestInvalid, err) //nolint:errorlint // joining two sentinels intentionally
	}

	// 1. Find the prior version. Admin (TenantID == nil) sees global;
	// tenant-scoped callers see own + platform-wide.
	var prior []database.Plugin
	var err error
	if arg.TenantID == nil {
		prior, err = s.repo.FindByNameGlobal(ctx, arg.Manifest.Name)
	} else {
		prior, err = s.repo.FindByNameForTenant(ctx, arg.Manifest.Name)
	}
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("installer: lookup prior version: %w", err)
	}
	if len(prior) == 0 {
		return UpgradeResult{}, ErrNotInstalled
	}

	// 2. Semver compare. Only strict upgrades are allowed.
	old := prior[0]
	cmp := compareSemver(arg.Manifest.Version, old.Version)
	if cmp == 0 {
		return UpgradeResult{}, fmt.Errorf("installer: %s already at %s: %w",
			arg.Manifest.Name, arg.Manifest.Version, ErrSameVersion)
	}
	if cmp < 0 {
		return UpgradeResult{}, fmt.Errorf("installer: %s new %s < old %s: %w",
			arg.Manifest.Name, arg.Manifest.Version, old.Version, ErrDowngrade)
	}

	// 3. Compile + persist the new version. The unique index on
	// (tenant_id, name, version) prevents collisions; an admin hitting
	// it gets ErrDuplicateUpload (409) which is the right outcome.
	oldGrants, err := s.repo.ListPermissions(ctx, old.ID)
	if err != nil {
		return UpgradeResult{}, fmt.Errorf("installer: list prior grants: %w", err)
	}
	newRow, err := s.Upload(ctx, arg)
	if err != nil {
		return UpgradeResult{}, err
	}

	// 4. Diff grants. The manifest's permission list is the new
	// baseline. For each prior grant:
	//   - if the new manifest requests it (exact or wildcard match),
	//     copy it to the new row.
	//   - otherwise drop it.
	// New manifest permissions the old version did not have surface in
	// NewPermissions so the admin can approve them.
	result := UpgradeResult{New: newRow, OldID: old.ID}
	requested := manifestPermissionSet(arg.Manifest)
	grantedSet := make(map[string]struct{}, len(oldGrants))
	for _, g := range oldGrants {
		grantedSet[g.Permission] = struct{}{}
	}
	for _, perm := range oldGrants {
		if _, stillRequested := requested[perm.Permission]; stillRequested {
			if err := s.repo.GrantPermission(ctx, newRow.ID, arg.ActorUserID, perm.Permission); err != nil {
				return result, fmt.Errorf("installer: copy grant %s: %w", perm.Permission, err)
			}
			result.PreservedGrants = append(result.PreservedGrants, perm.Permission)
		} else {
			result.DroppedGrants = append(result.DroppedGrants, perm.Permission)
		}
	}
	for perm := range requested {
		if _, alreadyGranted := grantedSet[perm]; alreadyGranted {
			continue
		}
		result.NewPermissions = append(result.NewPermissions, perm)
	}

	// 5. Best-effort cleanup of HTTP handlers + event subscriptions for
	// the OLD row before CASCADE. The DoD calls out "removal cleans up
	// event subscriptions, KV, HTTP routes" — CASCADE handles it
	// atomically, but the explicit cleanup makes the side effect
	// observable to anyone polling those tables.
	if s.handlers != nil {
		if _, err := s.handlers.DeleteAllForPlugin(ctx, old.ID); err != nil {
			return result, fmt.Errorf("installer: cleanup http handlers: %w", err)
		}
	}
	if s.subscriptions != nil {
		if _, err := s.subscriptions.DeleteAllForPlugin(ctx, old.ID); err != nil {
			return result, fmt.Errorf("installer: cleanup subscriptions: %w", err)
		}
	}

	// 6. Hard-delete the old row. CASCADE catches anything we missed.
	if err := s.repo.Delete(ctx, old.ID); err != nil {
		return result, fmt.Errorf("installer: delete prior version: %w", err)
	}

	s.emit(ctx, Event{
		Action: ActionUpgrade,
		Tenant: arg.TenantID, Actor: arg.ActorUserID, RequestID: arg.RequestID,
		PluginID: newRow.ID, Status: audit.StatusSuccess,
		Details: map[string]any{
			"name":             newRow.Name,
			"old_version":      old.Version,
			"new_version":      newRow.Version,
			"old_id":           old.ID.String(),
			"preserved_grants": result.PreservedGrants,
			"dropped_grants":   result.DroppedGrants,
			"new_permissions":  result.NewPermissions,
		},
	})
	return result, nil
}

// manifestPermissionSet returns the set of permissions the manifest
// declares. Used by Upgrade to diff against the prior version's grants.
func manifestPermissionSet(m *manifest.Manifest) map[string]struct{} {
	out := make(map[string]struct{}, len(m.Permissions))
	for _, p := range m.Permissions {
		out[p] = struct{}{}
	}
	return out
}

// compareSemver returns -1 / 0 / +1 by comparing MAJOR.MINOR.PATCH
// numerically. Pre-release / build suffixes are ignored (the manifest
// validator accepts a subset of semver; the comparison treats 1.0.0-rc1
// and 1.0.0 as equal — install/upgrade always picks the canonical
// MAJOR.MINOR.PATCH).
//
// Lahijan uses semver for plugin versions because:
//   - the (tenant, name, version) unique index requires stable ordering
//   - the upgrade flow rejects downgrades + same-version reinstalls
//   - the marketplace UI sorts entries by version
func compareSemver(a, b string) int {
	pa := splitSemver(a)
	pb := splitSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// splitSemver extracts the [major, minor, patch] triple from a version
// string. Non-numeric or missing segments are treated as 0 so the
// comparison never panics.
func splitSemver(v string) [3]int {
	var out [3]int
	parts := strings.SplitN(v, ".", 4)
	for i := 0; i < 3 && i < len(parts); i++ {
		// Strip an optional pre-release / build suffix on the PATCH.
		clean := strings.SplitN(parts[i], "+", 2)[0]
		clean = strings.SplitN(clean, "-", 2)[0]
		n := 0
		for _, r := range clean {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
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
