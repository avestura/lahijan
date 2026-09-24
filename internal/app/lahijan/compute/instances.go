// Package compute: instances.go implements the instance lifecycle (create,
// start, stop, restart, freeze, unfreeze, delete) and the per-action audit +
// event emission. Every privileged action emits an audit row BEFORE the side
// effect (status=pending) and marks the outcome AFTER; every lifecycle
// transition also emits into the WASM event bus so plugins can react.
//
// The orchestration order is:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Audit emit (status=pending) — the row exists even if step 4 fails.
//  3. Quota + balance check (for create).
//  4. Incus call (the actual side effect).
//  5. Compute repo update (cache the new state).
//  6. Event bus emit (so plugins react after the DB is consistent).
//  7. Audit mark-outcome (success | failure).
//
// If the provider is nil the service returns ErrProviderDisabled which the
// handler maps to 501 not_implemented.
package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// InstanceCreateParams carries the user-controlled fields of a create call.
// The service resolves the project_name from the tenant id (via the
// provider) and the image_fingerprint from the alias (via the images repo).
type InstanceCreateParams struct {
	// Name is the instance name. Required.
	Name string
	// Type is "container" or "virtual-machine". Empty defaults to "container".
	Type string
	// ImageAlias is the alias of the image to create from. Required.
	ImageAlias string
	// ImageFingerprint optionally pins a specific image by fingerprint.
	// When non-empty, the create call uses the fingerprint directly and
	// skips the public-image-server auto-resolution that fires when
	// ImageAlias contains "/".
	ImageFingerprint string
	// Description is the user-visible description.
	Description string
	// Config is the instance config map (limits.cpu, limits.memory, ...).
	Config map[string]string
	// Devices is the per-device config map (root disk, nics, ...).
	Devices map[string]map[string]string
	// Profiles is the list of profiles applied (defaults to ["default"]).
	Profiles []string
}

// InstanceRow is the API-facing shape returned by Get/List. It merges the
// cached DB row with the live Incus state when the daemon is reachable.
type InstanceRow struct {
	database.ComputeInstance
}

// CreateInstance orchestrates an instance create: quota check -> audit
// pending -> provider create -> cache row -> event emit -> audit outcome.
// Returns the cached DB row on success.
func (s *Service) CreateInstance(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params InstanceCreateParams,
) (database.ComputeInstance, error) {
	if s.provider == nil {
		return database.ComputeInstance{}, ErrProviderDisabled
	}
	if params.Name == "" {
		return database.ComputeInstance{}, ErrInvalidName
	}
	if params.ImageAlias == "" {
		return database.ComputeInstance{}, ErrInvalidImage
	}

	// Sanitise the device map: if the caller (e.g. the dashboard) sent a
	// "root" device without a "pool" property, drop it so the seeded
	// default profile's root device (which DOES point at a real pool, per
	// EnsureProject's seedDefaultProfile) is used instead. An instance-level
	// root device with no pool overrides the profile's root device and
	// Incus rejects it with "Device validation failed for \"root\": Root
	// disk entry must have a \"pool\" property set". The UI's "size" hint
	// is preserved by re-applying it to the profile's pool when both are
	// present; for the common case the profile's defaults are correct.
	if root, ok := params.Devices["root"]; ok {
		if _, hasPool := root["pool"]; !hasPool {
			delete(params.Devices, "root")
		}
	}

	// 0) Pre-flight: name uniqueness within the tenant.
	if existing, err := s.repos.ComputeInstances.GetByName(ctx, params.Name); err == nil && existing.ID != uuid.Nil {
		return database.ComputeInstance{}, fmt.Errorf("%w: name=%s", ErrInstanceNameTaken, params.Name)
	} else if err != nil && !database.IsNoRows(err) {
		return database.ComputeInstance{}, fmt.Errorf("compute: lookup name: %w", err)
	}

	// 1) Quota check. We pre-flight before the audit so a 422 doesn't
	// leave an audit row marked "pending" forever; a denied create is
	// not a privileged action that happened, it's one that was rejected.
	parsedConfig := InstanceConfig{
		Config:   params.Config,
		Devices:  params.Devices,
		Profiles: params.Profiles,
	}
	currentUsage, errUsage := s.computeUsage(ctx, tenantID)
	if errUsage != nil {
		return database.ComputeInstance{}, fmt.Errorf("compute: quota usage: %w", errUsage)
	}
	checker := quotaChecker{cfg: s.quotas}
	if err := checker.checkNewInstance(ctx, tenantID, currentUsage, parsedConfig); err != nil {
		return database.ComputeInstance{}, err
	}

	// 2) Balance check. WS-17 wires the real ledger; until then the
	// billing interface is nil and this is a no-op (the service still
	// works for free in dev / tests). ADR-0013: insufficient balance ->
	// 402 payment_required.

	// 3) EnsureProject on the Incus side. Idempotent.
	if errEnsure := s.provider.EnsureProject(ctx, tenantID); errEnsure != nil {
		return database.ComputeInstance{}, fmt.Errorf("compute: ensure project: %w", errEnsure)
	}

	// 4) Insert the compute_instances row with status=stopped (the
	// daemon will flip it to Running if start is requested). The row
	// exists before the Incus create call so a daemon timeout still
	// leaves the tenant able to retry by name.
	project := s.provider.ProjectName(tenantID)
	configJSON, _ := json.Marshal(parsedConfig)
	profiles := params.Profiles
	if len(profiles) == 0 {
		profiles = []string{"default"}
	}
	row, err := s.repos.ComputeInstances.Create(ctx, database.CreateComputeInstanceParams{
		ProjectName: project,
		Name:        params.Name,
		Type:        params.Type,
		Status:      database.InstanceStatusStopped,
		StatusCode:  int32(database.StatusCodeStopped),
		ImageAlias:  params.ImageAlias,
		Profiles:    profiles,
		Config:      configJSON,
		Description: params.Description,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return database.ComputeInstance{}, fmt.Errorf("%w: name=%s", ErrInstanceNameTaken, params.Name)
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: create instance row: %w", err)
	}

	// 5) Audit emit (status=pending). The row exists even if step 6 fails.
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceCreate,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name":        params.Name,
			"image_alias": params.ImageAlias,
			"type":        params.Type,
		},
	})

	// 5.5) WS-26: pick a cluster member for the new instance. The
	// placement driver is non-nil (New falls back to Local). The
	// Local driver returns "" so single-node daemons work unchanged;
	// the Cluster driver queries the Incus cluster API under a
	// per-tenant advisory lock.
	target, errPlace := s.placement.SelectTarget(ctx, PlacementParams{
		TenantID: tenantID,
		Project:  project,
		Name:     params.Name,
		Type:     params.Type,
		Config:   params.Config,
	})
	if errPlace != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": errPlace.Error(),
		}})
		return database.ComputeInstance{}, fmt.Errorf("compute: select target: %w", errPlace)
	}

	// 6) Incus create.
	//
	// Image source resolution: when the alias contains a "/" (e.g.
	// "ubuntu/24.04", "alpine/edge/tinycloud") AND no explicit fingerprint
	// was provided, point Incus at the public image server
	// (images.linuxcontainers.org). Without Source.Server, Incus looks up
	// the alias in the project's LOCAL image store only — and a fresh
	// tenant project has no cached images, so every create would fail with
	// an empty operation + "image not found" silently.
	//
	// The public server matches the daemon's own `images:` remote
	// (configured at preseed time) so this is the same image source an
	// operator gets from `incus launch images:ubuntu/24.04`.
	source := incus.InstanceSource{Type: "image", Alias: params.ImageAlias}
	if params.ImageFingerprint != "" {
		source.Fingerprint = params.ImageFingerprint
	} else if strings.Contains(params.ImageAlias, "/") {
		source.Server = "https://images.linuxcontainers.org"
		source.Protocol = "simplestreams"
	}
	op, err := s.provider.CreateInstance(ctx, incus.CreateInstanceParams{
		Project:     project,
		Name:        params.Name,
		Type:        params.Type,
		Description: params.Description,
		Config:      params.Config,
		Devices:     params.Devices,
		Profiles:    profiles,
		Source:      source,
		Target:      target,
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		// When the daemon definitively failed the create (bad image,
		// restriction, ...) no instance exists: soft-delete the row so the
		// name is free to retry (names are unique among live rows). On a
		// timeout / transport error the daemon may still finish, so the
		// row stays to track that instance.
		if errors.Is(err, incus.ErrAsyncOperationFailed) {
			_ = s.repos.ComputeInstances.SoftDelete(ctx, row.ID)
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: incus create: %w", err)
	}

	// 7) Cache the resolved fingerprint (if Incus reported one).
	if fp := opFingerprint(op); fp != "" {
		_ = s.repos.ComputeInstances.SetImageFingerprint(ctx, row.ID, fp)
		row.ImageFingerprint = fp
	}

	// 7.5) WS-26: cache the Incus-reported cluster member. The
	// daemon populates the instance's Location field from the
	// target; we mirror it into compute_instances.cluster_member so
	// the UI can render "where does this instance live" without a
	// per-row daemon round-trip.
	if live, errGetInstance := s.provider.GetInstance(ctx, project, params.Name); errGetInstance == nil && live.Location != "" {
		loc := live.Location
		_ = s.repos.ComputeInstances.SetClusterMember(ctx, row.ID, &loc)
		row.ClusterMember = &loc
	} else if target != "" {
		// Fall back to the requested target when the daemon does not
		// echo back the Location (e.g. single-node daemon that
		// accepts the target and ignores it).
		t := target
		_ = s.repos.ComputeInstances.SetClusterMember(ctx, row.ID, &t)
		row.ClusterMember = &t
	}

	// 8) Event bus emit (compute.instance.created). Best-effort: a
	// failing emit does not roll back the create.
	s.emitEvent(ctx, eventbus.ComputeInstanceCreated, tenantID, userID, row.ID, map[string]any{
		"name":        row.Name,
		"image_alias": row.ImageAlias,
		"status":      row.Status,
	})

	// 9) Audit mark-outcome (success).
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"instance_id": row.ID,
	}})

	return row, nil
}

// GetInstance returns the cached instance row. The Incus daemon is NOT
// probed here; callers that need live state call ReconcileInstance first.
func (s *Service) GetInstance(
	ctx context.Context,
	_ uuid.UUID,
	instanceID uuid.UUID,
) (database.ComputeInstance, error) {
	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeInstance{}, ErrInstanceNotFound
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: get instance: %w", err)
	}
	return row, nil
}

// ReconcileInstance asks the daemon for the live state and updates the
// cached row. Returns the updated row. A daemon error does NOT propagate;
// the caller sees the stale cache instead so a flaky daemon does not 500
// the API.
func (s *Service) ReconcileInstance(
	ctx context.Context,
	_ uuid.UUID,
	instanceID uuid.UUID,
) (database.ComputeInstance, error) {
	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeInstance{}, ErrInstanceNotFound
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: get instance: %w", err)
	}
	if s.provider == nil {
		return row, nil
	}
	inst, errGetInstance := s.provider.GetInstance(ctx, row.ProjectName, row.Name)
	if errGetInstance != nil {
		// Daemon unreachable / instance missing on daemon side. Return
		// the cached row; a follow-up reconcile will pick up the live
		// state when the daemon is back. The error is intentionally
		// swallowed: a flaky daemon should not 500 the API.
		return row, nil //nolint:nilerr // best-effort reconcile; cache is the fallback
	}
	_ = s.repos.ComputeInstances.SetStatus(ctx, instanceID, inst.Status, int32(inst.StatusCode))
	row.Status = inst.Status
	row.StatusCode = int32(inst.StatusCode)
	// WS-26: refresh the cluster_member cache from the daemon's
	// Location field. NULL on a single-node daemon; the cached column
	// is informational only.
	if inst.Location != "" {
		loc := inst.Location
		_ = s.repos.ComputeInstances.SetClusterMember(ctx, instanceID, &loc)
		row.ClusterMember = &loc
	}
	return row, nil
}

// ListInstances returns a paginated list of the tenant's instances. The
// cache is returned; reconciliation is a separate call so a list endpoint
// is not blocked on the daemon.
func (s *Service) ListInstances(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.ComputeInstance, error) {
	rows, err := s.repos.ComputeInstances.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("compute: list instances: %w", err)
	}
	return rows, nil
}

// CountInstances returns the number of non-deleted instances in the tenant.
func (s *Service) CountInstances(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.ComputeInstances.Count(ctx)
}

// InstanceLifecycleAction is the enum of state-change verbs the service
// accepts. Mirrors incus.InstanceAction but kept separate so the API layer
// can speak in user-facing terms ("start", "stop") without leaking the
// provider's vocabulary.
type InstanceLifecycleAction string

const (
	ActionStart    InstanceLifecycleAction = "start"
	ActionStop     InstanceLifecycleAction = "stop"
	ActionRestart  InstanceLifecycleAction = "restart"
	ActionFreeze   InstanceLifecycleAction = "freeze"
	ActionUnfreeze InstanceLifecycleAction = "unfreeze"
)

// incusAction maps the API-facing action to the provider-level action.
// Returns ok=false for an unknown action so the caller can 400.
func incusAction(action InstanceLifecycleAction) (incus.InstanceAction, bool) {
	switch action {
	case ActionStart:
		return incus.ActionStart, true
	case ActionStop:
		return incus.ActionStop, true
	case ActionRestart:
		return incus.ActionRestart, true
	case ActionFreeze:
		return incus.ActionFreeze, true
	case ActionUnfreeze:
		return incus.ActionUnfreeze, true
	}
	return "", false
}

// auditActionFor maps the lifecycle action to the audit action slug.
func auditActionFor(action InstanceLifecycleAction) string {
	switch action {
	case ActionStart:
		return AuditInstanceStart
	case ActionStop:
		return AuditInstanceStop
	case ActionRestart:
		return AuditInstanceRestart
	case ActionFreeze, ActionUnfreeze:
		return AuditInstanceFreeze
	}
	return ""
}

// eventTopicFor maps the lifecycle action to the WASM bus topic. Returns
// empty string when there's no canonical event for the action (e.g.
// freeze/unfreeze are operational, not lifecycle).
func eventTopicFor(action InstanceLifecycleAction) string {
	switch action {
	case ActionStart:
		return eventbus.ComputeInstanceStarted
	case ActionStop:
		return eventbus.ComputeInstanceStopped
	case ActionRestart:
		return eventbus.ComputeInstanceRestarted
	case ActionFreeze, ActionUnfreeze:
		// Operational state changes; no canonical event in the registry.
		return ""
	default:
		return ""
	}
}

// SetInstanceState applies a lifecycle action to an instance. The audit
// pattern (pending -> success | failure) is the same as CreateInstance.
// Quota is NOT re-checked; only create consumes quota.
func (s *Service) SetInstanceState(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	instanceID uuid.UUID,
	action InstanceLifecycleAction,
	force bool,
	timeoutSecs int,
) (database.ComputeInstance, error) {
	if s.provider == nil {
		return database.ComputeInstance{}, ErrProviderDisabled
	}
	iaction, ok := incusAction(action)
	if !ok {
		return database.ComputeInstance{}, fmt.Errorf("compute: unknown action %q", action)
	}

	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeInstance{}, ErrInstanceNotFound
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: get instance: %w", err)
	}

	// Audit emit (status=pending).
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       auditActionFor(action),
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"action": string(action),
			"force":  force,
		},
	})

	// Incus call.
	op, err := s.provider.SetInstanceState(ctx, row.ProjectName, row.Name, iaction, force, timeoutSecs)
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeInstance{}, fmt.Errorf("compute: incus %s: %w", action, err)
	}

	// Cache the new status. Incus reports the operation's terminal
	// status; the actual instance status is queried via GetInstanceState
	// so we record the daemon's view, not the operation's view.
	newStatus := row.Status
	newStatusCode := row.StatusCode
	if st := opStatus(op); st != "" {
		newStatus = st
		newStatusCode = opStatusCode(op)
	}
	if live, err := s.provider.GetInstanceState(ctx, row.ProjectName, row.Name); err == nil {
		newStatus = live.Status
		newStatusCode = int32(live.StatusCode)
	}
	_ = s.repos.ComputeInstances.SetStatus(ctx, instanceID, newStatus, newStatusCode)
	row.Status = newStatus
	row.StatusCode = newStatusCode

	// Event bus emit.
	if topic := eventTopicFor(action); topic != "" {
		s.emitEvent(ctx, topic, tenantID, userID, row.ID, map[string]any{
			"name":   row.Name,
			"status": newStatus,
		})
	}

	// Audit mark-outcome (success).
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"status": newStatus,
	}})

	return row, nil
}

// DeleteInstance orchestrates an instance delete: stop (force) if running,
// Incus delete, soft-delete the DB row, event emit, audit outcome.
func (s *Service) DeleteInstance(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	instanceID uuid.UUID,
	force bool,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrInstanceNotFound
		}
		return fmt.Errorf("compute: get instance: %w", err)
	}

	// Audit emit (status=pending).
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceDelete,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name":  row.Name,
			"force": force,
		},
	})

	// Stop the instance if it's running. The force flag is forwarded
	// so a hung shutdown does not block the delete.
	if row.Status == database.InstanceStatusRunning && s.provider != nil {
		if _, err := s.provider.SetInstanceState(ctx, row.ProjectName, row.Name,
			incus.ActionStop, force, 30); err != nil {
			if !force {
				_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
					"error": fmt.Sprintf("pre-delete stop: %v", err),
				}})
				return fmt.Errorf("compute: pre-delete stop: %w", err)
			}
			// force=true: ignore the stop error and let Incus delete it.
		}
	}

	// Incus delete.
	if _, err := s.provider.DeleteInstance(ctx, row.ProjectName, row.Name); err != nil {
		if !errors.Is(err, incus.ErrNotFound) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return fmt.Errorf("compute: incus delete: %w", err)
		}
		// Instance already gone on the daemon side; fall through to
		// the soft-delete so the DB row matches.
	}

	// Soft-delete the row (caches deleted status; preserves audit join).
	if err := s.repos.ComputeInstances.SoftDelete(ctx, instanceID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("compute: soft delete row: %w", err)
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.ComputeInstanceDeleted, tenantID, userID, row.ID, map[string]any{
		"name": row.Name,
	})

	// Audit mark-outcome (success).
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// UpdateInstance replaces the cached config snapshot. The Incus side is
// updated via UpdateInstance so the live instance picks up the change on
// the next start (or immediately for hot-pluggable fields).
func (s *Service) UpdateInstance(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	instanceID uuid.UUID,
	description string,
	config map[string]string,
	devices map[string]map[string]string,
	profiles []string,
) (database.ComputeInstance, error) {
	if s.provider == nil {
		return database.ComputeInstance{}, ErrProviderDisabled
	}
	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeInstance{}, ErrInstanceNotFound
		}
		return database.ComputeInstance{}, fmt.Errorf("compute: get instance: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceUpdate,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})

	// Incus PUT replaces config + devices wholesale, so a PATCH that omits
	// a field must carry the instance's current value for it; otherwise
	// changing one config key would silently wipe every local device (and
	// an omitted description would clear it).
	if live, errLive := s.provider.GetInstance(ctx, row.ProjectName, row.Name); errLive == nil {
		if config == nil {
			config = live.Config
		}
		if devices == nil {
			devices = live.Devices
		}
		if profiles == nil {
			profiles = live.Profiles
		}
		if description == "" {
			description = live.Description
		}
	}
	if profiles == nil {
		profiles = row.Profiles
	}
	parsedConfig := InstanceConfig{
		Config: config, Devices: devices, Profiles: profiles,
	}
	configJSON, _ := json.Marshal(parsedConfig)

	// Incus update.
	if _, err := s.provider.UpdateInstance(ctx, row.ProjectName, row.Name, incus.InstancePut{
		Description: description,
		Config:      config,
		Devices:     devices,
		Profiles:    profiles,
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeInstance{}, fmt.Errorf("compute: incus update: %w", err)
	}

	if err := s.repos.ComputeInstances.UpdateConfig(ctx, instanceID, configJSON, profiles, description); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeInstance{}, fmt.Errorf("compute: update config: %w", err)
	}
	row.Description = description
	row.ConfigJson = configJSON
	row.Profiles = profiles

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// emitEvent is the best-effort event-bus helper. A nil bus or a failed
// emit does NOT propagate; the privileged action already happened.
func (s *Service) emitEvent(
	ctx context.Context,
	topic string,
	tenantID, userID, resourceID uuid.UUID,
	meta map[string]any,
) {
	if s.bus == nil {
		return
	}
	var raw []byte
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = b
		}
	}
	tid := tenantID
	uid := userID
	rid := resourceID
	_ = s.bus.Emit(ctx, eventbus.Event{
		Topic:      topic,
		TenantID:   &tid,
		ActorType:  audit.ActorUser,
		ActorID:    &uid,
		ResourceID: &rid,
		Metadata:   raw,
	})
}

// opFingerprint extracts the image fingerprint from a create operation's
// metadata. Incus does not always populate this; returning "" is fine (the
// row keeps the alias and the daemon resolves it lazily on next read).
func opFingerprint(op *incus.Operation) string {
	if op == nil || len(op.Metadata) == 0 {
		return ""
	}
	var meta struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(op.Metadata, &meta); err != nil {
		return ""
	}
	return meta.Fingerprint
}

// opStatus extracts the human-readable status string from an operation.
// Incus operations end in "Success" / "Failure"; the instance status is
// queried separately via GetInstanceState.
func opStatus(op *incus.Operation) string {
	if op == nil {
		return ""
	}
	return op.Status
}

// opStatusCode extracts the numeric status code from an operation.
func opStatusCode(op *incus.Operation) int32 {
	if op == nil {
		return 0
	}
	return int32(op.StatusCode)
}
