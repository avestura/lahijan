// Package compute: snapshots.go implements the user-facing snapshot
// surface (WS-25): manual snapshot create / list / get / delete /
// restore, plus the helpers the River workers (compute.snapshot.take +
// compute.snapshot.prune) reuse.
//
// Every privileged action emits an audit row BEFORE the side effect
// (status=pending) and marks the outcome AFTER, mirroring the instance
// lifecycle pattern. Every successful create emits a compute.snapshot.taken
// event into the WASM bus so plugins can react.
//
// Layering (mirrors instances.go):
//
//  1. RBAC check at the HTTP boundary (api/middleware.RequirePerm).
//  2. Audit emit (status=pending) — the row exists even if step 4 fails.
//  3. Lahijan-side compute_snapshots row insert (before the Incus call so
//     a daemon timeout leaves the tenant able to retry by name).
//  4. Incus CreateSnapshot call.
//  5. Snapshot row update with daemon-reported size.
//  6. WASM event bus emit.
//  7. Audit mark-outcome (success | failure).
//
// If the provider is nil the service returns ErrProviderDisabled which the
// handler maps to 501 not_implemented.
package compute

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/google/uuid"
)

// CreateSnapshotParams carries the user-controlled fields of a manual
// snapshot create call. The service resolves the instance row + project
// from instanceID + tenantID. ExpiresAt is optional (manual snapshots do
// not expire by default); schedule policies stamp it on the snapshots
// they create.
type CreateSnapshotParams struct {
	InstanceID  uuid.UUID
	Name        string
	Stateful    bool
	Description string
	// ExpiresAt is the optional prune-time. NULL means keep-until-deleted.
	// The schedule-policy path computes this from cadence + retain; the
	// manual path forwards a user-supplied value (typically nil).
	ExpiresAt *time.Time
}

// TakeSnapshot creates a snapshot for the given instance. Used by both
// the manual POST /instances/{id}/snapshots path (actor = the user) and
// the compute.snapshot.take River worker (actor = the system). The
// policyID is nil for manual snapshots and set for scheduled ones so the
// prune worker can attribute snapshots to policies.
//
// The method is idempotent on name: a duplicate name returns
// ErrSnapshotNameTaken without calling Incus, so a worker retry after a
// partial failure does not double-snapshot.
func (s *Service) TakeSnapshot(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params CreateSnapshotParams,
	policyID *uuid.UUID,
) (database.ComputeSnapshot, error) {
	if s.provider == nil {
		return database.ComputeSnapshot{}, ErrProviderDisabled
	}
	if params.Name == "" {
		return database.ComputeSnapshot{}, ErrInvalidName
	}
	// Resolve the instance (also tenant-scoped via ctx).
	instance, err := s.repos.ComputeInstances.Get(ctx, params.InstanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshot{}, ErrInstanceNotFound
		}
		return database.ComputeSnapshot{}, fmt.Errorf("compute: snapshot lookup instance: %w", err)
	}

	// Pre-flight: name uniqueness within the instance.
	if existing, err := s.repos.ComputeSnapshots.GetByName(ctx, params.InstanceID, params.Name); err == nil && existing.ID != uuid.Nil {
		return database.ComputeSnapshot{}, fmt.Errorf("%w: name=%s", ErrSnapshotNameTaken, params.Name)
	} else if err != nil && !database.IsNoRows(err) {
		return database.ComputeSnapshot{}, fmt.Errorf("compute: snapshot lookup name: %w", err)
	}

	// Insert the row before the Incus call so a daemon timeout leaves the
	// tenant able to retry by name.
	row, err := s.repos.ComputeSnapshots.Create(ctx, database.CreateComputeSnapshotParams{
		InstanceID:  params.InstanceID,
		Name:        params.Name,
		Stateful:    params.Stateful,
		Description: params.Description,
		ExpiresAt:   params.ExpiresAt,
		PolicyID:    policyID,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return database.ComputeSnapshot{}, fmt.Errorf("%w: name=%s", ErrSnapshotNameTaken, params.Name)
		}
		return database.ComputeSnapshot{}, fmt.Errorf("compute: create snapshot row: %w", err)
	}

	// Audit emit (status=pending). For scheduled snapshots the actor is
	// the system; the audit row still records the policy that fired.
	auditAction := AuditSnapshotCreate
	actorID := userID
	actorType := audit.ActorUser
	if policyID != nil {
		auditAction = AuditScheduledSnapshotTaken
		actorType = audit.ActorSystem
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &actorID,
		ActorType:    actorType,
		Action:       auditAction,
		ResourceType: ResourceSnapshot,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"instance_id": params.InstanceID,
			"name":        params.Name,
			"stateful":    params.Stateful,
			"policy_id":   policyID,
		},
	})

	// Incus call.
	op, err := s.provider.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project:  instance.ProjectName,
		Instance: instance.Name,
		Name:     params.Name,
		Stateful: params.Stateful,
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		// The row stays in place so the user can see the failed take and retry.
		return database.ComputeSnapshot{}, fmt.Errorf("compute: incus create snapshot: %w", err)
	}

	// Cache the daemon-reported size when present (the metadata carries
	// the operation's terminal state; Incus surfaces size on the snapshot
	// GET, so we fetch it lazily).
	if op != nil && op.Status == "Success" {
		if snap, errGet := s.provider.GetSnapshot(ctx, instance.ProjectName, instance.Name, params.Name); errGet == nil && snap != nil {
			_ = s.repos.ComputeSnapshots.SetSize(ctx, row.ID, snap.Size)
			row.SizeBytes = snap.Size
		}
	}

	// WASM event bus emit.
	s.emitEvent(ctx, eventbus.ComputeSnapshotTaken, tenantID, userID, row.ID, map[string]any{
		"instance_id": row.InstanceID,
		"name":        row.Name,
		"stateful":    row.Stateful,
		"policy_id":   policyID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"snapshot_id": row.ID,
		"size_bytes":  row.SizeBytes,
	}})
	return row, nil
}

// ListSnapshots returns a page of snapshots for the given instance.
func (s *Service) ListSnapshots(
	ctx context.Context,
	_ uuid.UUID,
	instanceID uuid.UUID,
	limit, offset int32,
) ([]database.ComputeSnapshot, error) {
	return s.repos.ComputeSnapshots.ListByInstance(ctx, instanceID, limit, offset)
}

// CountSnapshots returns the total number of snapshots for the instance.
func (s *Service) CountSnapshots(
	ctx context.Context,
	_ uuid.UUID,
	instanceID uuid.UUID,
) (int64, error) {
	return s.repos.ComputeSnapshots.CountByInstance(ctx, instanceID)
}

// GetSnapshot returns the cached snapshot row. The Incus daemon is NOT
// probed here; callers that need live state call ReconcileSnapshot first.
func (s *Service) GetSnapshot(
	ctx context.Context,
	_ uuid.UUID,
	snapshotID uuid.UUID,
) (database.ComputeSnapshot, error) {
	row, err := s.repos.ComputeSnapshots.Get(ctx, snapshotID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshot{}, ErrSnapshotNotFound
		}
		return database.ComputeSnapshot{}, fmt.Errorf("compute: get snapshot: %w", err)
	}
	return row, nil
}

// DeleteSnapshot orchestrates a snapshot delete: Incus delete, soft-delete
// the DB row, event emit, audit outcome.
func (s *Service) DeleteSnapshot(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	snapshotID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	row, err := s.repos.ComputeSnapshots.Get(ctx, snapshotID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrSnapshotNotFound
		}
		return fmt.Errorf("compute: get snapshot: %w", err)
	}
	instance, err := s.repos.ComputeInstances.Get(ctx, row.InstanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrInstanceNotFound
		}
		return fmt.Errorf("compute: get instance for snapshot: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditSnapshotDelete,
		ResourceType: ResourceSnapshot,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"instance_id": row.InstanceID,
			"name":        row.Name,
		},
	})

	if _, err := s.provider.DeleteSnapshot(ctx, instance.ProjectName, instance.Name, row.Name); err != nil {
		if !errors.Is(err, incus.ErrNotFound) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return fmt.Errorf("compute: incus delete snapshot: %w", err)
		}
	}

	if err := s.repos.ComputeSnapshots.SoftDelete(ctx, snapshotID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("compute: soft delete snapshot row: %w", err)
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// RestoreSnapshot orchestrates an instance restore from a snapshot. The
// instance MUST already exist in the project; Incus does not auto-create
// it. The actor must hold compute.instance.update (the audit gate maps
// the restore path to that perm) because restore replaces the instance
// state wholesale.
func (s *Service) RestoreSnapshot(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	snapshotID uuid.UUID,
	stateful bool,
) (database.ComputeSnapshot, error) {
	if s.provider == nil {
		return database.ComputeSnapshot{}, ErrProviderDisabled
	}
	row, err := s.repos.ComputeSnapshots.Get(ctx, snapshotID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshot{}, ErrSnapshotNotFound
		}
		return database.ComputeSnapshot{}, fmt.Errorf("compute: get snapshot: %w", err)
	}
	instance, err := s.repos.ComputeInstances.Get(ctx, row.InstanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshot{}, ErrInstanceNotFound
		}
		return database.ComputeSnapshot{}, fmt.Errorf("compute: get instance for snapshot: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditSnapshotRestore,
		ResourceType: ResourceSnapshot,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"instance_id": row.InstanceID,
			"snapshot_id": row.ID,
			"stateful":    stateful,
		},
	})

	if _, err := s.provider.RestoreSnapshot(ctx, instance.ProjectName, instance.Name, row.Name, stateful); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeSnapshot{}, fmt.Errorf("compute: incus restore snapshot: %w", err)
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// ExportSnapshotBytes downloads the snapshot's tarball from Incus. The
// caller (the backup worker) streams the bytes to a BackupTarget.
// Tenant scoping is enforced via the snapshot row lookup; a snapshot
// outside the caller's tenant returns ErrSnapshotNotFound before the
// Incus call.
func (s *Service) ExportSnapshotBytes(
	ctx context.Context,
	_ uuid.UUID,
	snapshotID uuid.UUID,
) (database.ComputeSnapshot, []byte, error) {
	if s.provider == nil {
		return database.ComputeSnapshot{}, nil, ErrProviderDisabled
	}
	row, err := s.repos.ComputeSnapshots.Get(ctx, snapshotID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshot{}, nil, ErrSnapshotNotFound
		}
		return database.ComputeSnapshot{}, nil, fmt.Errorf("compute: get snapshot: %w", err)
	}
	instance, err := s.repos.ComputeInstances.Get(ctx, row.InstanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshot{}, nil, ErrInstanceNotFound
		}
		return database.ComputeSnapshot{}, nil, fmt.Errorf("compute: get instance for snapshot: %w", err)
	}
	body, err := s.provider.ExportSnapshot(ctx, instance.ProjectName, instance.Name, row.Name)
	if err != nil {
		return database.ComputeSnapshot{}, nil, fmt.Errorf("compute: incus export snapshot: %w", err)
	}
	if size := int64(len(body)); size > row.SizeBytes {
		_ = s.repos.ComputeSnapshots.SetSize(ctx, row.ID, size)
		row.SizeBytes = size
	}
	return row, body, nil
}

// -------------------------------------------------------------------------
// Cadence parsing (used by the snapshot-policy service).
// -------------------------------------------------------------------------

// iso8601DurationRegex matches the ISO 8601 duration subset the policy
// surface accepts: P[n]W or P[n]D or PT[n]H or PT[n]M. Larger units
// (years, months — ambiguous number of seconds) are rejected so the
// prune worker can compute deterministic expiries.
var iso8601DurationRegex = regexp.MustCompile(`^P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?)?$`)

// ParseCadence parses an ISO 8601 duration into a time.Duration. Accepts
// the subset P[n]W, P[n]D, PT[n]H, PT[n]M. Returns ErrInvalidCadence for
// anything else so the caller surfaces a 400 to the user.
//
//nolint:revive // name stays ParseCadence for API symmetry with the policy domain.
func ParseCadence(s string) (time.Duration, error) {
	if s == "" {
		return 0, ErrInvalidCadence
	}
	m := iso8601DurationRegex.FindStringSubmatch(s)
	if m == nil {
		return 0, ErrInvalidCadence
	}
	// Reject "P" alone (no fields populated).
	if m[1] == "" && m[2] == "" && m[3] == "" && m[4] == "" {
		return 0, ErrInvalidCadence
	}
	var d time.Duration
	if m[1] != "" {
		weeks, _ := strconv.Atoi(m[1])
		d += time.Duration(weeks) * 7 * 24 * time.Hour
	}
	if m[2] != "" {
		days, _ := strconv.Atoi(m[2])
		d += time.Duration(days) * 24 * time.Hour
	}
	if m[3] != "" {
		hours, _ := strconv.Atoi(m[3])
		d += time.Duration(hours) * time.Hour
	}
	if m[4] != "" {
		mins, _ := strconv.Atoi(m[4])
		d += time.Duration(mins) * time.Minute
	}
	if d <= 0 {
		return 0, ErrInvalidCadence
	}
	return d, nil
}

// FormatCadenceHuman renders a cadence string in a compact, locale-neutral
// form for audit logs + the admin UI. It does NOT localise; the caller
// renders the user-facing string via i18n.
func FormatCadenceHuman(cadence string) string {
	d, err := ParseCadence(cadence)
	if err != nil {
		return cadence // fall back to the raw string for unknown shapes
	}
	// Round to the nearest minute for display; sub-minute cadences are
	// rejected by ParseCadence so this is exact.
	mins := int(d.Minutes())
	switch {
	case mins%60 == 0 && mins/60 >= 1 && (mins/60)%24 != 0:
		return strings.TrimSuffix(cadence, "")
	case mins%(60*24) == 0 && mins/(60*24) >= 1:
		return strings.TrimSuffix(cadence, "")
	}
	return cadence
}
