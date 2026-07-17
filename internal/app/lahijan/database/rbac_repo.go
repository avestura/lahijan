// Package database: rbac_repo.go wraps the sqlc-generated RBAC queries. The
// roles, permissions, and role_permissions tables are global. Policy
// enforcement (RequirePerm) ships in WS-08; this repo only persists and reads
// the role/permission catalog.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// RBACRepository is the persistence boundary for roles, permissions, and
// role_permissions.
type RBACRepository struct {
	q *gen.Queries
}

// NewRBACRepository wraps the given sqlc queries.
func NewRBACRepository(q *gen.Queries) *RBACRepository {
	return &RBACRepository{q: q}
}

// CreateRoleParams carries the user-controlled fields of a new role.
type CreateRoleParams struct {
	Slug        string
	Name        string
	Description *string
	IsSystem    *bool
}

// CreateRole inserts a role row.
func (r *RBACRepository) CreateRole(ctx context.Context, arg CreateRoleParams) (gen.Role, error) {
	return r.q.CreateRole(ctx, gen.CreateRoleParams{
		Slug:        arg.Slug,
		Name:        arg.Name,
		Description: strOr(arg.Description, ""),
		IsSystem:    boolOr(arg.IsSystem, false),
	})
}

// GetRoleByID returns the role with the given id.
func (r *RBACRepository) GetRoleByID(ctx context.Context, id uuid.UUID) (gen.Role, error) {
	return r.q.GetRoleByID(ctx, id)
}

// GetRoleBySlug returns the non-deleted role with the given slug.
func (r *RBACRepository) GetRoleBySlug(ctx context.Context, slug string) (gen.Role, error) {
	return r.q.GetRoleBySlug(ctx, slug)
}

// ListRoles returns all non-deleted roles.
func (r *RBACRepository) ListRoles(ctx context.Context) ([]gen.Role, error) {
	return r.q.ListRoles(ctx)
}

// CreatePermission inserts a permission row.
func (r *RBACRepository) CreatePermission(
	ctx context.Context,
	slug string,
	description *string,
) (gen.Permission, error) {
	return r.q.CreatePermission(ctx, gen.CreatePermissionParams{Slug: slug, Description: strOr(description, "")})
}

// GetPermissionBySlug returns the permission with the given slug.
func (r *RBACRepository) GetPermissionBySlug(ctx context.Context, slug string) (gen.Permission, error) {
	return r.q.GetPermissionBySlug(ctx, slug)
}

// ListPermissions returns all permissions, ordered by slug.
func (r *RBACRepository) ListPermissions(ctx context.Context) ([]gen.Permission, error) {
	return r.q.ListPermissions(ctx)
}

// GrantPermissionToRole links a permission to a role (idempotent).
func (r *RBACRepository) GrantPermissionToRole(ctx context.Context, roleID, permissionID uuid.UUID) error {
	return r.q.GrantPermissionToRole(ctx, gen.GrantPermissionToRoleParams{
		RoleID: roleID, PermissionID: permissionID,
	})
}

// ListPermissionsForRole returns every permission granted to a role.
func (r *RBACRepository) ListPermissionsForRole(ctx context.Context, roleID uuid.UUID) ([]gen.Permission, error) {
	return r.q.ListPermissionsForRole(ctx, roleID)
}
