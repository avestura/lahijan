// Package program: signup.go wires the self-service signup provisioner that
// gives every newly registered account a personal tenant it owns.
package program

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// personalTenantProvisioner implements session.SignupProvisioner. Without
// it a self-registered user has no membership, so every tenant-scoped
// action (compute, DNS, storage) is denied and the dashboard is read-only.
type personalTenantProvisioner struct {
	repos *database.Repos
	// hooks run after the tenant + owner membership exist (e.g. seeding
	// the compute featured-image catalog). Registered at bootstrap only.
	hooks []func(ctx context.Context, tenantID uuid.UUID) error
}

// onTenantCreated registers a hook run for every new personal tenant. Not
// safe for use after the server starts accepting signups.
func (p *personalTenantProvisioner) onTenantCreated(h func(ctx context.Context, tenantID uuid.UUID) error) {
	p.hooks = append(p.hooks, h)
}

// ProvisionSignup creates a tenant named after the user's email and makes
// the user its tenant.owner. The slug embeds the user id, so it is unique
// and stable.
func (p *personalTenantProvisioner) ProvisionSignup(ctx context.Context, userID uuid.UUID, email string) error {
	role, err := p.repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantOwner)
	if err != nil {
		return fmt.Errorf("resolve %s role: %w", rbac.RoleTenantOwner, err)
	}
	active := true
	tenant, err := p.repos.Tenants.Create(ctx, database.CreateTenantParams{
		Slug:     "personal-" + userID.String(),
		Name:     email,
		IsActive: &active,
	})
	if err != nil {
		return fmt.Errorf("create personal tenant: %w", err)
	}
	if _, err := p.repos.Memberships.Create(database.WithTenant(ctx, tenant.ID), userID, &role.ID); err != nil {
		return fmt.Errorf("create owner membership: %w", err)
	}
	var hookErrs []error
	for _, h := range p.hooks {
		if err := h(ctx, tenant.ID); err != nil {
			hookErrs = append(hookErrs, err)
		}
	}
	return errors.Join(hookErrs...)
}
