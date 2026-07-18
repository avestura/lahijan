// Package database: compute_profiles_repo.go wraps the sqlc-generated
// compute_profiles queries (WS-14). Every query is tenant-scoped via
// WithTenant at the repository seam.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ComputeProfilesRepository is the persistence boundary for compute_profiles.
type ComputeProfilesRepository struct {
	q *gen.Queries
}

// NewComputeProfilesRepository wraps the given sqlc queries.
func NewComputeProfilesRepository(q *gen.Queries) *ComputeProfilesRepository {
	return &ComputeProfilesRepository{q: q}
}

// CreateComputeProfileParams carries the user-controlled fields of a new row.
type CreateComputeProfileParams struct {
	Name        string
	Description string
	Config      json.RawMessage
}

// Create inserts a new compute_profiles row scoped to the tenant in ctx.
func (r *ComputeProfilesRepository) Create(
	ctx context.Context,
	arg CreateComputeProfileParams,
) (gen.ComputeProfile, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeProfile{}, err
	}
	cfg := arg.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	return r.q.CreateComputeProfile(ctx, gen.CreateComputeProfileParams{
		TenantID: tenantID, Name: arg.Name,
		Description: arg.Description, ConfigJson: cfg,
	})
}

// Get returns the compute_profiles row with id within the tenant in ctx.
func (r *ComputeProfilesRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeProfile, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeProfile{}, err
	}
	return r.q.GetComputeProfileByID(ctx, gen.GetComputeProfileByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the compute_profiles row matching name within the tenant.
func (r *ComputeProfilesRepository) GetByName(
	ctx context.Context,
	name string,
) (gen.ComputeProfile, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeProfile{}, err
	}
	return r.q.GetComputeProfileByName(ctx, gen.GetComputeProfileByNameParams{
		TenantID: tenantID, Name: name,
	})
}

// List returns a page of compute_profiles within the tenant in ctx.
func (r *ComputeProfilesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeProfile, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeProfiles(ctx, gen.ListComputeProfilesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of compute_profiles within the tenant in ctx.
func (r *ComputeProfilesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeProfiles(ctx, tenantID)
}

// Update replaces the description + config snapshot for the profile.
func (r *ComputeProfilesRepository) Update(
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
	return r.q.UpdateComputeProfile(ctx, gen.UpdateComputeProfileParams{
		TenantID: tenantID, ID: id,
		Description: description, ConfigJson: config,
	})
}

// SoftDelete marks the profile as deleted.
func (r *ComputeProfilesRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeProfile(ctx, gen.SoftDeleteComputeProfileParams{
		TenantID: tenantID, ID: id,
	})
}
