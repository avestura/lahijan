// Package database: compute_backups_repo.go wraps the sqlc-generated
// compute_backups queries (WS-25). Every query is tenant-scoped via
// WithTenant at the repository seam. Backups are append-only for audit;
// the worker updates size_bytes + status via the dedicated setters.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// BackupStatus values are the worker progress states. The worker
// transitions an export through pending -> uploading -> completed | failed.
const (
	BackupStatusPending   = "pending"
	BackupStatusUploading = "uploading"
	BackupStatusCompleted = "completed"
	BackupStatusFailed    = "failed"
)

// ComputeBackupsRepository is the persistence boundary for the
// compute_backups table.
type ComputeBackupsRepository struct {
	q *gen.Queries
}

// NewComputeBackupsRepository wraps the given sqlc queries.
func NewComputeBackupsRepository(q *gen.Queries) *ComputeBackupsRepository {
	return &ComputeBackupsRepository{q: q}
}

// CreateComputeBackupParams carries the user-controlled fields of a new
// compute_backups row. TenantID is taken from the request context, NOT
// from the caller.
type CreateComputeBackupParams struct {
	SnapshotID     uuid.UUID
	InstanceID     uuid.UUID
	TargetID       uuid.UUID
	RemoteLocation string
	Status         string
}

// Create inserts a new compute_backups row scoped to the tenant in ctx.
func (r *ComputeBackupsRepository) Create(
	ctx context.Context,
	arg CreateComputeBackupParams,
) (gen.ComputeBackup, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeBackup{}, err
	}
	status := arg.Status
	if status == "" {
		status = BackupStatusPending
	}
	return r.q.CreateComputeBackup(ctx, gen.CreateComputeBackupParams{
		TenantID:       tenantID,
		SnapshotID:     arg.SnapshotID,
		InstanceID:     arg.InstanceID,
		TargetID:       arg.TargetID,
		RemoteLocation: arg.RemoteLocation,
		Status:         status,
	})
}

// Get returns the compute_backups row with id within the tenant in ctx.
func (r *ComputeBackupsRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeBackup, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeBackup{}, err
	}
	return r.q.GetComputeBackupByID(ctx, gen.GetComputeBackupByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// List returns a page of compute_backups within the tenant in ctx.
func (r *ComputeBackupsRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeBackup, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeBackups(ctx, gen.ListComputeBackupsParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// ListBySnapshot returns a page of compute_backups for a given snapshot
// within the tenant in ctx.
func (r *ComputeBackupsRepository) ListBySnapshot(
	ctx context.Context,
	snapshotID uuid.UUID,
	limit, offset int32,
) ([]gen.ComputeBackup, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeBackupsBySnapshot(ctx, gen.ListComputeBackupsBySnapshotParams{
		TenantID: tenantID, SnapshotID: snapshotID, Limit: limit, Offset: offset,
	})
}

// ListByInstance returns a page of compute_backups for a given instance
// within the tenant in ctx. Used by the instance-detail UI.
func (r *ComputeBackupsRepository) ListByInstance(
	ctx context.Context,
	instanceID uuid.UUID,
	limit, offset int32,
) ([]gen.ComputeBackup, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeBackupsByInstance(ctx, gen.ListComputeBackupsByInstanceParams{
		TenantID: tenantID, InstanceID: instanceID, Limit: limit, Offset: offset,
	})
}

// ListByTarget returns a page of compute_backups for a given target
// within the tenant in ctx.
func (r *ComputeBackupsRepository) ListByTarget(
	ctx context.Context,
	targetID uuid.UUID,
	limit, offset int32,
) ([]gen.ComputeBackup, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeBackupsByTarget(ctx, gen.ListComputeBackupsByTargetParams{
		TenantID: tenantID, TargetID: targetID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted compute_backups within the
// tenant in ctx.
func (r *ComputeBackupsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeBackups(ctx, tenantID)
}

// SetStatus updates the worker progress. The error_message column is set
// when status="failed".
func (r *ComputeBackupsRepository) SetStatus(
	ctx context.Context,
	id uuid.UUID,
	status, errorMessage string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetComputeBackupStatus(ctx, gen.SetComputeBackupStatusParams{
		TenantID: tenantID, ID: id, Status: status, ErrorMessage: errorMessage,
	})
}

// SetResult records the final size + checksum + remote_location after a
// successful upload. Only the worker calls this.
func (r *ComputeBackupsRepository) SetResult(
	ctx context.Context,
	id uuid.UUID,
	sizeBytes int64,
	checksum, remoteLocation string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetComputeBackupResult(ctx, gen.SetComputeBackupResultParams{
		TenantID:       tenantID,
		ID:             id,
		SizeBytes:      sizeBytes,
		ChecksumSha256: checksum,
		RemoteLocation: remoteLocation,
	})
}

// SoftDelete marks the backup as deleted. The row is retained for
// historical audit. The remote bytes should be removed by the worker
// before this runs.
func (r *ComputeBackupsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeBackup(ctx, gen.SoftDeleteComputeBackupParams{
		TenantID: tenantID, ID: id,
	})
}
