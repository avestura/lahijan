// Package database: compute_storage_volumes_repo.go wraps the sqlc-generated
// compute_storage_volumes queries (WS-14). Every query is tenant-scoped via
// WithTenant at the repository seam.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// VolumeType values mirror the Incus storage volume "type" field. Lahijan
// only manages "custom" volumes today; container / image / virtual-machine
// volumes are derived from their owning instance / image.
const (
	VolumeTypeCustom         = "custom"
	VolumeTypeContainer      = "container"
	VolumeTypeImage          = "image"
	VolumeTypeVirtualMachine = "virtual-machine"
)

// ComputeStorageVolumesRepository is the persistence boundary for
// compute_storage_volumes.
type ComputeStorageVolumesRepository struct {
	q *gen.Queries
}

// NewComputeStorageVolumesRepository wraps the given sqlc queries.
func NewComputeStorageVolumesRepository(q *gen.Queries) *ComputeStorageVolumesRepository {
	return &ComputeStorageVolumesRepository{q: q}
}

// CreateComputeStorageVolumeParams carries the user-controlled fields.
type CreateComputeStorageVolumeParams struct {
	Name        string
	Description string
	Type        string
	PoolName    string
	Config      json.RawMessage
}

// Create inserts a new compute_storage_volumes row scoped to the tenant in ctx.
func (r *ComputeStorageVolumesRepository) Create(
	ctx context.Context,
	arg CreateComputeStorageVolumeParams,
) (gen.ComputeStorageVolume, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeStorageVolume{}, err
	}
	volType := arg.Type
	if volType == "" {
		volType = VolumeTypeCustom
	}
	pool := arg.PoolName
	if pool == "" {
		pool = "default"
	}
	cfg := arg.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	return r.q.CreateComputeStorageVolume(ctx, gen.CreateComputeStorageVolumeParams{
		TenantID: tenantID, Name: arg.Name, Description: arg.Description,
		Type: volType, PoolName: pool, ConfigJson: cfg,
	})
}

// Get returns the compute_storage_volumes row with id within the tenant in ctx.
func (r *ComputeStorageVolumesRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeStorageVolume, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeStorageVolume{}, err
	}
	return r.q.GetComputeStorageVolumeByID(ctx, gen.GetComputeStorageVolumeByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the compute_storage_volumes row matching (pool, name)
// within the tenant in ctx — the Incus composite key.
func (r *ComputeStorageVolumesRepository) GetByName(
	ctx context.Context,
	pool, name string,
) (gen.ComputeStorageVolume, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeStorageVolume{}, err
	}
	if pool == "" {
		pool = "default"
	}
	return r.q.GetComputeStorageVolumeByName(ctx, gen.GetComputeStorageVolumeByNameParams{
		TenantID: tenantID, PoolName: pool, Name: name,
	})
}

// List returns a page of compute_storage_volumes within the tenant in ctx.
func (r *ComputeStorageVolumesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeStorageVolume, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeStorageVolumes(ctx, gen.ListComputeStorageVolumesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of compute_storage_volumes within the tenant in ctx.
func (r *ComputeStorageVolumesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeStorageVolumes(ctx, tenantID)
}

// Update replaces the description + config snapshot for the volume.
func (r *ComputeStorageVolumesRepository) Update(
	ctx context.Context,
	id uuid.UUID,
	description string,
	config json.RawMessage,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	return r.q.UpdateComputeStorageVolume(ctx, gen.UpdateComputeStorageVolumeParams{
		TenantID: tenantID, ID: id,
		Description: description, ConfigJson: config,
	})
}

// SoftDelete marks the volume as deleted.
func (r *ComputeStorageVolumesRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeStorageVolume(ctx, gen.SoftDeleteComputeStorageVolumeParams{
		TenantID: tenantID, ID: id,
	})
}
