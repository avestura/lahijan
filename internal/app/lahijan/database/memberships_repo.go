// Package database: memberships_repo.go is the canonical tenant-scoped
// repository. Every method that touches a tenant's data extracts the tenant id
// from the request context via TenantFromContext and bakes it into the query;
// callers have no way to supply (or forget) the tenant id. A call without a
// tenant context fails fast with ErrNoTenantInContext. This is the discipline
// ADR-0002 mandates and the pattern every later tenant-scoped repo will copy.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// MembershipsRepository is the persistence boundary for the memberships table.
type MembershipsRepository struct {
	q *gen.Queries
}

// NewMembershipsRepository wraps the given sqlc queries.
func NewMembershipsRepository(q *gen.Queries) *MembershipsRepository {
	return &MembershipsRepository{q: q}
}

// Create inserts a membership linking a user to the tenant in ctx. The tenant
// id is taken from ctx, NOT from the caller, so a missing tenant context fails
// closed. roleID may be nil during RBAC bootstrap.
func (r *MembershipsRepository) Create(
	ctx context.Context,
	userID uuid.UUID,
	roleID *uuid.UUID,
) (gen.Membership, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Membership{}, err
	}
	return r.q.CreateMembership(ctx, gen.CreateMembershipParams{
		TenantID: tenantID,
		UserID:   userID,
		RoleID:   roleID,
	})
}

// Get returns the membership with id within the tenant in ctx.
func (r *MembershipsRepository) Get(ctx context.Context, id uuid.UUID) (gen.Membership, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Membership{}, err
	}
	return r.q.GetMembership(ctx, gen.GetMembershipParams{TenantID: tenantID, ID: id})
}

// GetByUser returns the membership for userID within the tenant in ctx.
func (r *MembershipsRepository) GetByUser(ctx context.Context, userID uuid.UUID) (gen.Membership, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Membership{}, err
	}
	return r.q.GetMembershipByUser(ctx, gen.GetMembershipByUserParams{TenantID: tenantID, UserID: userID})
}

// ListForTenant returns a page of memberships within the tenant in ctx.
func (r *MembershipsRepository) ListForTenant(
	ctx context.Context,
	limit, offset int32,
) ([]gen.Membership, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListMembershipsForTenant(ctx, gen.ListMembershipsForTenantParams{
		TenantID: tenantID,
		Limit:    limit,
		Offset:   offset,
	})
}

// CountForTenant returns the number of memberships within the tenant in ctx.
func (r *MembershipsRepository) CountForTenant(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountMembershipsForTenant(ctx, tenantID)
}

// SetRole updates the role of a user's membership within the tenant in ctx.
func (r *MembershipsRepository) SetRole(
	ctx context.Context,
	userID uuid.UUID,
	roleID *uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetMembershipRole(ctx, gen.SetMembershipRoleParams{
		TenantID: tenantID,
		UserID:   userID,
		RoleID:   roleID,
	})
}

// SoftDelete marks a membership deleted within the tenant in ctx.
func (r *MembershipsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteMembership(ctx, gen.SoftDeleteMembershipParams{TenantID: tenantID, ID: id})
}

// ListForUser is intentionally NOT tenant-scoped: it returns every tenant a
// user belongs to (used by the "switch tenant" flow). It takes userID from the
// caller because the tenant context is the *currently selected* tenant, which
// is not what this query is about. This deviation from the tenant-scoped rule
// is explicit and limited to cross-tenant discovery queries.
func (r *MembershipsRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]gen.Membership, error) {
	return r.q.ListMembershipsForUser(ctx, userID)
}
