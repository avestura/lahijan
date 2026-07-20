// Package database: compute_snapshot_policies_repo.go wraps the
// sqlc-generated compute_snapshot_policies queries (WS-25). Every
// per-tenant query is tenant-scoped via WithTenant at the repository seam.
// The ListDue method is intentionally cross-tenant so the take worker can
// scan the whole table in one round-trip; the worker re-scopes per row
// via WithTenant before any DB write.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ComputeSnapshotPoliciesRepository is the persistence boundary for the
// compute_snapshot_policies table.
type ComputeSnapshotPoliciesRepository struct {
	q *gen.Queries
}

// NewComputeSnapshotPoliciesRepository wraps the given sqlc queries.
func NewComputeSnapshotPoliciesRepository(q *gen.Queries) *ComputeSnapshotPoliciesRepository {
	return &ComputeSnapshotPoliciesRepository{q: q}
}

// CreateComputeSnapshotPolicyParams carries the user-controlled fields of
// a new compute_snapshot_policies row. TenantID is taken from the request
// context, NOT from the caller. InstanceID is nil for a tenant-default
// policy. NextRunAt is computed by the caller from cadence.
type CreateComputeSnapshotPolicyParams struct {
	InstanceID  *uuid.UUID
	Name        string
	Cadence     string
	RetainCount int32
	TargetID    *uuid.UUID
	Enabled     bool
	NextRunAt   *time.Time
}

// Create inserts a new compute_snapshot_policies row scoped to the tenant
// in ctx.
func (r *ComputeSnapshotPoliciesRepository) Create(
	ctx context.Context,
	arg CreateComputeSnapshotPolicyParams,
) (gen.ComputeSnapshotPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeSnapshotPolicy{}, err
	}
	retain := arg.RetainCount
	if retain <= 0 {
		retain = 7
	}
	return r.q.CreateComputeSnapshotPolicy(ctx, gen.CreateComputeSnapshotPolicyParams{
		TenantID:    tenantID,
		InstanceID:  arg.InstanceID,
		Name:        arg.Name,
		Cadence:     arg.Cadence,
		RetainCount: retain,
		TargetID:    arg.TargetID,
		Enabled:     arg.Enabled,
		NextRunAt:   arg.NextRunAt,
	})
}

// Get returns the compute_snapshot_policies row with id within the tenant
// in ctx.
func (r *ComputeSnapshotPoliciesRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeSnapshotPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeSnapshotPolicy{}, err
	}
	return r.q.GetComputeSnapshotPolicyByID(ctx, gen.GetComputeSnapshotPolicyByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the policy row matching name within the tenant in ctx.
func (r *ComputeSnapshotPoliciesRepository) GetByName(ctx context.Context, name string) (gen.ComputeSnapshotPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeSnapshotPolicy{}, err
	}
	return r.q.GetComputeSnapshotPolicyByName(ctx, gen.GetComputeSnapshotPolicyByNameParams{
		TenantID: tenantID, Name: name,
	})
}

// List returns a page of compute_snapshot_policies within the tenant in ctx.
func (r *ComputeSnapshotPoliciesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeSnapshotPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeSnapshotPolicies(ctx, gen.ListComputeSnapshotPoliciesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// ListByInstance returns the per-instance policy (if any) + the
// tenant-default within the tenant in ctx.
func (r *ComputeSnapshotPoliciesRepository) ListByInstance(
	ctx context.Context,
	instanceID uuid.UUID,
	limit, offset int32,
) ([]gen.ComputeSnapshotPolicy, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeSnapshotPoliciesByInstance(ctx, gen.ListComputeSnapshotPoliciesByInstanceParams{
		TenantID: tenantID, InstanceID: &instanceID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted compute_snapshot_policies within
// the tenant in ctx.
func (r *ComputeSnapshotPoliciesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeSnapshotPolicies(ctx, tenantID)
}

// ListDue returns up to limit policies whose next_run_at has passed. This
// is cross-tenant by design: the take worker scans the whole table for
// due rows, then re-scopes per row via WithTenant before any DB write.
func (r *ComputeSnapshotPoliciesRepository) ListDue(
	ctx context.Context,
	now time.Time,
	limit int32,
) ([]gen.ComputeSnapshotPolicy, error) {
	return r.q.ListDueComputeSnapshotPolicies(ctx, gen.ListDueComputeSnapshotPoliciesParams{
		NextRunAt: &now, Limit: limit,
	})
}

// UpdateComputeSnapshotPolicyParams carries the user-editable fields.
type UpdateComputeSnapshotPolicyParams struct {
	InstanceID  *uuid.UUID
	Name        string
	Cadence     string
	RetainCount int32
	TargetID    *uuid.UUID
	Enabled     bool
}

// Update replaces the user-editable fields.
func (r *ComputeSnapshotPoliciesRepository) Update(
	ctx context.Context,
	id uuid.UUID,
	arg UpdateComputeSnapshotPolicyParams,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	retain := arg.RetainCount
	if retain <= 0 {
		retain = 7
	}
	return r.q.UpdateComputeSnapshotPolicy(ctx, gen.UpdateComputeSnapshotPolicyParams{
		TenantID:    tenantID,
		ID:          id,
		InstanceID:  arg.InstanceID,
		Name:        arg.Name,
		Cadence:     arg.Cadence,
		RetainCount: retain,
		TargetID:    arg.TargetID,
		Enabled:     arg.Enabled,
	})
}

// MarkRun records that the policy just ran at ranAt and schedules the
// next run at nextAt. Used by the take worker after every run.
func (r *ComputeSnapshotPoliciesRepository) MarkRun(
	ctx context.Context,
	id uuid.UUID,
	ranAt, nextAt time.Time,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.MarkComputeSnapshotPolicyRun(ctx, gen.MarkComputeSnapshotPolicyRunParams{
		TenantID: tenantID, ID: id, LastRunAt: &ranAt, NextRunAt: &nextAt,
	})
}

// SoftDelete marks the policy as deleted + disabled so the worker stops
// picking it up.
func (r *ComputeSnapshotPoliciesRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeSnapshotPolicy(ctx, gen.SoftDeleteComputeSnapshotPolicyParams{
		TenantID: tenantID, ID: id,
	})
}
