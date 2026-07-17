// Package database: tenants_repo.go wraps the sqlc-generated tenant queries.
// `tenants` is a global table, so these methods do not perform tenant scoping;
// they are still the only sanctioned entry point for services to reach the
// table.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// TenantsRepository is the persistence boundary for the tenants table.
type TenantsRepository struct {
	q *gen.Queries
}

// NewTenantsRepository wraps the given sqlc queries.
func NewTenantsRepository(q *gen.Queries) *TenantsRepository {
	return &TenantsRepository{q: q}
}

// CreateTenantParams carries the user-controlled fields of a new tenant.
type CreateTenantParams struct {
	Slug     string
	Name     string
	IsActive *bool
}

// Create inserts a tenant row.
func (r *TenantsRepository) Create(ctx context.Context, arg CreateTenantParams) (gen.Tenant, error) {
	return r.q.CreateTenant(ctx, gen.CreateTenantParams{
		Slug:     arg.Slug,
		Name:     arg.Name,
		IsActive: boolOr(arg.IsActive, true),
	})
}

// GetByID returns the tenant with the given id.
func (r *TenantsRepository) GetByID(ctx context.Context, id uuid.UUID) (gen.Tenant, error) {
	return r.q.GetTenantByID(ctx, id)
}

// GetBySlug returns the non-deleted tenant with the given slug.
func (r *TenantsRepository) GetBySlug(ctx context.Context, slug string) (gen.Tenant, error) {
	return r.q.GetTenantBySlug(ctx, slug)
}

// List returns a page of non-deleted tenants, newest first.
func (r *TenantsRepository) List(ctx context.Context, limit, offset int32) ([]gen.Tenant, error) {
	return r.q.ListTenants(ctx, gen.ListTenantsParams{Limit: limit, Offset: offset})
}

// Count returns the number of non-deleted tenants.
func (r *TenantsRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountTenants(ctx)
}

// SetActive toggles a tenant's is_active flag.
func (r *TenantsRepository) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return r.q.SetTenantActive(ctx, gen.SetTenantActiveParams{ID: id, IsActive: active})
}

// SoftDelete marks a tenant deleted and inactive.
func (r *TenantsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	return r.q.SoftDeleteTenant(ctx, id)
}
