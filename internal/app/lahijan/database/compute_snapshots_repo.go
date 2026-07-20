// Package database: compute_snapshots_repo.go wraps the sqlc-generated
// compute_snapshots queries (WS-25). Every query is tenant-scoped via
// WithTenant at the repository seam — callers cannot pass a tenant id
// directly. The row mirrors Incus snapshot state but the daemon is the
// source of truth; the cached size_bytes + expires_at fields are
// reconciled after the CreateSnapshot call.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ComputeSnapshotsRepository is the persistence boundary for the
// compute_snapshots table.
type ComputeSnapshotsRepository struct {
	q *gen.Queries
}

// NewComputeSnapshotsRepository wraps the given sqlc queries.
func NewComputeSnapshotsRepository(q *gen.Queries) *ComputeSnapshotsRepository {
	return &ComputeSnapshotsRepository{q: q}
}

// CreateComputeSnapshotParams carries the user-controlled fields of a new
// compute_snapshots row. TenantID is taken from the request context, NOT
// from the caller. PolicyID is set when the snapshot is created by a
// schedule (NULL for manual snapshots — they are exempt from prune).
type CreateComputeSnapshotParams struct {
	InstanceID  uuid.UUID
	Name        string
	Stateful    bool
	SizeBytes   int64
	ExpiresAt   *time.Time
	Description string
	PolicyID    *uuid.UUID
}

// Create inserts a new compute_snapshots row scoped to the tenant in ctx.
func (r *ComputeSnapshotsRepository) Create(
	ctx context.Context,
	arg CreateComputeSnapshotParams,
) (gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeSnapshot{}, err
	}
	return r.q.CreateComputeSnapshot(ctx, gen.CreateComputeSnapshotParams{
		TenantID:    tenantID,
		InstanceID:  arg.InstanceID,
		Name:        arg.Name,
		Stateful:    arg.Stateful,
		SizeBytes:   arg.SizeBytes,
		ExpiresAt:   arg.ExpiresAt,
		Description: arg.Description,
		PolicyID:    arg.PolicyID,
	})
}

// Get returns the compute_snapshots row with id within the tenant in ctx.
func (r *ComputeSnapshotsRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeSnapshot{}, err
	}
	return r.q.GetComputeSnapshotByID(ctx, gen.GetComputeSnapshotByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the compute_snapshots row matching (instance, name)
// within the tenant in ctx.
func (r *ComputeSnapshotsRepository) GetByName(
	ctx context.Context,
	instanceID uuid.UUID,
	name string,
) (gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeSnapshot{}, err
	}
	return r.q.GetComputeSnapshotByName(ctx, gen.GetComputeSnapshotByNameParams{
		TenantID: tenantID, InstanceID: instanceID, Name: name,
	})
}

// List returns a page of compute_snapshots within the tenant in ctx.
func (r *ComputeSnapshotsRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeSnapshots(ctx, gen.ListComputeSnapshotsParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// ListByInstance returns a page of compute_snapshots for a given instance
// within the tenant in ctx.
func (r *ComputeSnapshotsRepository) ListByInstance(
	ctx context.Context,
	instanceID uuid.UUID,
	limit, offset int32,
) ([]gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeSnapshotsByInstance(ctx, gen.ListComputeSnapshotsByInstanceParams{
		TenantID: tenantID, InstanceID: instanceID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted compute_snapshots within the
// tenant in ctx.
func (r *ComputeSnapshotsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeSnapshots(ctx, tenantID)
}

// CountByInstance returns the number of non-deleted compute_snapshots for
// the given instance within the tenant in ctx. The prune worker uses it.
func (r *ComputeSnapshotsRepository) CountByInstance(
	ctx context.Context,
	instanceID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeSnapshotsByInstance(ctx, gen.CountComputeSnapshotsByInstanceParams{
		TenantID: tenantID, InstanceID: instanceID,
	})
}

// ListExpired returns up to limit snapshots whose expires_at has passed,
// oldest-first, within the tenant in ctx. The prune worker batches via
// this method.
func (r *ComputeSnapshotsRepository) ListExpired(
	ctx context.Context,
	now time.Time,
	limit int32,
) ([]gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListExpiredComputeSnapshots(ctx, gen.ListExpiredComputeSnapshotsParams{
		TenantID: tenantID, ExpiresAt: &now, Limit: limit,
	})
}

// ListOldestForPolicy returns up to limit oldest snapshots created by the
// given policy within the tenant in ctx. The prune worker passes
// (current_count - retain_count) to delete the excess.
func (r *ComputeSnapshotsRepository) ListOldestForPolicy(
	ctx context.Context,
	policyID uuid.UUID,
	limit int32,
) ([]gen.ComputeSnapshot, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListOldestComputeSnapshotsForPolicy(ctx, gen.ListOldestComputeSnapshotsForPolicyParams{
		TenantID: tenantID, PolicyID: &policyID, Limit: limit,
	})
}

// SetSize caches the daemon-reported size for the snapshot.
func (r *ComputeSnapshotsRepository) SetSize(
	ctx context.Context,
	id uuid.UUID,
	sizeBytes int64,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetComputeSnapshotSize(ctx, gen.SetComputeSnapshotSizeParams{
		TenantID: tenantID, ID: id, SizeBytes: sizeBytes,
	})
}

// SetExpiry sets or clears the expires_at column for the snapshot.
func (r *ComputeSnapshotsRepository) SetExpiry(
	ctx context.Context,
	id uuid.UUID,
	expiresAt *time.Time,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetComputeSnapshotExpiry(ctx, gen.SetComputeSnapshotExpiryParams{
		TenantID: tenantID, ID: id, ExpiresAt: expiresAt,
	})
}

// SoftDelete marks the snapshot as deleted. The row is retained for
// historical audit + billing joins. The Incus snapshot itself should be
// deleted via the provider driver before this runs.
func (r *ComputeSnapshotsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeSnapshot(ctx, gen.SoftDeleteComputeSnapshotParams{
		TenantID: tenantID, ID: id,
	})
}
