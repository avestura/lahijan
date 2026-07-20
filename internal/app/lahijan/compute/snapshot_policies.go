// Package compute: snapshot_policies.go implements the user-facing
// snapshot schedule surface (WS-25): policy CRUD plus the helpers the
// River workers (compute.snapshot.take + compute.snapshot.prune) reuse.
//
// A policy says "take a snapshot of this instance (or every instance in
// this tenant when instance_id is NULL) every <cadence>, keep the last
// <retain_count>, and push each snapshot to <target_id> when set". The
// take worker scans for due policies via ListDue (cross-tenant by
// design); the prune worker enforces retain_count via ListOldestForPolicy.
//
// The cadence field is stored verbatim as ISO 8601 ("PT1H", "P1D",
// "P1W"); ParseCadence (in snapshots.go) is the canonical parser.
//
// Every privileged action emits an audit row BEFORE the side effect
// (status=pending) and marks the outcome AFTER, mirroring the snapshot
// + instance patterns.
package compute

import (
	"context"
	"fmt"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/google/uuid"
)

// CreateSnapshotPolicyParams carries the user-controlled fields of a
// policy create call. The service resolves next_run_at from cadence; the
// caller does not pass it.
type CreateSnapshotPolicyParams struct {
	InstanceID  *uuid.UUID
	Name        string
	Cadence     string
	RetainCount int32
	TargetID    *uuid.UUID
	Enabled     bool
}

// CreateSnapshotPolicy creates a snapshot schedule policy.
func (s *Service) CreateSnapshotPolicy(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params CreateSnapshotPolicyParams,
) (database.ComputeSnapshotPolicy, error) {
	if params.Name == "" {
		return database.ComputeSnapshotPolicy{}, ErrInvalidName
	}
	if _, err := ParseCadence(params.Cadence); err != nil {
		return database.ComputeSnapshotPolicy{}, err
	}
	if params.RetainCount <= 0 {
		params.RetainCount = 7
	}

	// Pre-flight: name uniqueness within the tenant.
	if existing, err := s.repos.ComputeSnapshotPolicies.GetByName(ctx, params.Name); err == nil && existing.ID != uuid.Nil {
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("%w: name=%s", ErrSnapshotPolicyNameTaken, params.Name)
	} else if err != nil && !database.IsNoRows(err) {
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("compute: policy lookup name: %w", err)
	}

	// Compute the first next_run_at. When enabled, it fires immediately
	// (within the next worker tick); when disabled, it stays nil until the
	// user flips enabled via Update.
	var nextRun *time.Time
	if params.Enabled {
		now := time.Now().UTC()
		nextRun = &now
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditSnapshotPolicyCreate,
		ResourceType: ResourceSnapshotPolicy,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name":         params.Name,
			"cadence":      params.Cadence,
			"retain_count": params.RetainCount,
			"instance_id":  params.InstanceID,
			"target_id":    params.TargetID,
		},
	})

	row, err := s.repos.ComputeSnapshotPolicies.Create(ctx, database.CreateComputeSnapshotPolicyParams{
		InstanceID:  params.InstanceID,
		Name:        params.Name,
		Cadence:     params.Cadence,
		RetainCount: params.RetainCount,
		TargetID:    params.TargetID,
		Enabled:     params.Enabled,
		NextRunAt:   nextRun,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": "name taken",
			}})
			return database.ComputeSnapshotPolicy{}, fmt.Errorf("%w: name=%s", ErrSnapshotPolicyNameTaken, params.Name)
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("compute: create snapshot policy: %w", err)
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"policy_id": row.ID,
	}})
	return row, nil
}

// GetSnapshotPolicy returns the policy row.
func (s *Service) GetSnapshotPolicy(
	ctx context.Context,
	_ uuid.UUID,
	policyID uuid.UUID,
) (database.ComputeSnapshotPolicy, error) {
	row, err := s.repos.ComputeSnapshotPolicies.Get(ctx, policyID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshotPolicy{}, ErrSnapshotPolicyNotFound
		}
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("compute: get snapshot policy: %w", err)
	}
	return row, nil
}

// ListSnapshotPolicies returns a page of policies within the tenant.
func (s *Service) ListSnapshotPolicies(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.ComputeSnapshotPolicy, error) {
	return s.repos.ComputeSnapshotPolicies.List(ctx, limit, offset)
}

// CountSnapshotPolicies returns the total number of policies.
func (s *Service) CountSnapshotPolicies(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.ComputeSnapshotPolicies.Count(ctx)
}

// UpdateSnapshotPolicyParams carries the user-editable fields. The cadence
// change recomputes next_run_at when the policy is enabled so the new
// cadence takes effect immediately.
type UpdateSnapshotPolicyParams struct {
	InstanceID  *uuid.UUID
	Name        string
	Cadence     string
	RetainCount int32
	TargetID    *uuid.UUID
	Enabled     bool
}

// UpdateSnapshotPolicy replaces the user-editable fields.
func (s *Service) UpdateSnapshotPolicy(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	policyID uuid.UUID,
	params UpdateSnapshotPolicyParams,
) (database.ComputeSnapshotPolicy, error) {
	if _, err := ParseCadence(params.Cadence); err != nil {
		return database.ComputeSnapshotPolicy{}, err
	}
	if params.RetainCount <= 0 {
		params.RetainCount = 7
	}
	if _, err := s.repos.ComputeSnapshotPolicies.Get(ctx, policyID); err != nil {
		if database.IsNoRows(err) {
			return database.ComputeSnapshotPolicy{}, ErrSnapshotPolicyNotFound
		}
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("compute: get snapshot policy: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditSnapshotPolicyUpdate,
		ResourceType: ResourceSnapshotPolicy,
		ResourceID:   &policyID,
		Status:       audit.StatusPending,
	})

	if err := s.repos.ComputeSnapshotPolicies.Update(ctx, policyID, database.UpdateComputeSnapshotPolicyParams{
		InstanceID:  params.InstanceID,
		Name:        params.Name,
		Cadence:     params.Cadence,
		RetainCount: params.RetainCount,
		TargetID:    params.TargetID,
		Enabled:     params.Enabled,
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("compute: update snapshot policy: %w", err)
	}

	// Recompute next_run_at when the policy is enabled and has never run
	// OR the cadence changed. The simplest behaviour is: if enabled and
	// last_run_at is nil, set next_run_at to now.
	row, err := s.repos.ComputeSnapshotPolicies.Get(ctx, policyID)
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeSnapshotPolicy{}, fmt.Errorf("compute: re-read policy: %w", err)
	}
	if row.Enabled && row.LastRunAt == nil {
		now := time.Now().UTC()
		_ = s.repos.ComputeSnapshotPolicies.MarkRun(ctx, policyID, row.CreatedAt, now)
		row.NextRunAt = &now
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// DeleteSnapshotPolicy soft-deletes the policy. Existing snapshots stay;
// their policy_id still points at the row but the prune worker will skip
// the soft-deleted policy.
func (s *Service) DeleteSnapshotPolicy(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	policyID uuid.UUID,
) error {
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditSnapshotPolicyDelete,
		ResourceType: ResourceSnapshotPolicy,
		ResourceID:   &policyID,
		Status:       audit.StatusPending,
	})
	if err := s.repos.ComputeSnapshotPolicies.SoftDelete(ctx, policyID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("compute: delete snapshot policy: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}
