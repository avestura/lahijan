// Package rbac: seeder.go ensures the permissions and default roles exist in
// the database. It is idempotent: running it twice is a no-op the second time.
//
// Call SeedOnce at bootstrap (program.Start) after the DB pool is open and
// migrations are applied. It writes:
//
//   - Every permission in allPermissions -> permissions row.
//   - Every role in defaultRoles -> roles row (marked is_system = TRUE).
//   - Every (role, permission) pair -> role_permissions row.
//
// Each step is a lookup-then-create (for the parent row) plus an idempotent
// ON CONFLICT DO NOTHING insert (for the role_permissions link), so re-runs
// against an already-seeded DB do not fail and do not clobber any hand-edited
// description. The seeder never deletes anything; if the registry shrinks,
// the orphans stay in the DB until a migration prunes them explicitly.
package rbac

import (
	"context"
	"fmt"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// SeedOnce ensures every permission in the registry and every default role
// exists in the DB. Safe to call on every bootstrap.
//
// The function is intentionally a no-op when repos is nil so the bootstrap
// can call it unconditionally and short-circuit cleanly in tests that don't
// wire the DB.
func SeedOnce(ctx context.Context, repos *database.Repos) error {
	if repos == nil || repos.RBAC == nil {
		return nil
	}
	if err := seedPermissions(ctx, repos.RBAC); err != nil {
		return fmt.Errorf("rbac: seed permissions: %w", err)
	}
	if err := seedRoles(ctx, repos.RBAC); err != nil {
		return fmt.Errorf("rbac: seed roles: %w", err)
	}
	return nil
}

// seedPermissions inserts every permission in the registry. Idempotent: a
// permission that already exists is left alone (slug is unique).
func seedPermissions(ctx context.Context, rbac *database.RBACRepository) error {
	for _, p := range allPermissions {
		// Lookup-then-create so a re-run does not clobber a hand-edited row.
		if _, err := rbac.GetPermissionBySlug(ctx, p.Slug); err == nil {
			continue
		} else if !isNoRows(err) {
			return fmt.Errorf("lookup permission %s: %w", p.Slug, err)
		}
		if _, err := rbac.CreatePermission(ctx, p.Slug, strPtr(p.Description)); err != nil {
			return fmt.Errorf("create permission %s: %w", p.Slug, err)
		}
	}
	return nil
}

// seedRoles inserts every default role and its permission grants. Idempotent.
func seedRoles(ctx context.Context, rbac *database.RBACRepository) error {
	for _, r := range defaultRoles {
		// Find or create the role row.
		role, err := rbac.GetRoleBySlug(ctx, r.Slug)
		if err != nil {
			if !isNoRows(err) {
				return fmt.Errorf("lookup role %s: %w", r.Slug, err)
			}
			role, err = rbac.CreateRole(ctx, database.CreateRoleParams{
				Slug:        r.Slug,
				Name:        r.Name,
				Description: strPtr(r.Description),
				IsSystem:    &r.IsSystem,
			})
			if err != nil {
				return fmt.Errorf("create role %s: %w", r.Slug, err)
			}
		}
		// Grant every permission. GrantPermissionToRole is idempotent
		// (ON CONFLICT DO NOTHING), so this is safe on re-runs.
		for _, slug := range r.Permissions {
			perm, err := rbac.GetPermissionBySlug(ctx, slug)
			if err != nil {
				return fmt.Errorf("lookup permission %s for role %s: %w", slug, r.Slug, err)
			}
			if err := rbac.GrantPermissionToRole(ctx, role.ID, perm.ID); err != nil {
				return fmt.Errorf("grant %s to %s: %w", slug, r.Slug, err)
			}
		}
	}
	return nil
}

// strPtr returns &s. Exists to keep the call sites readable (the repo takes
// *string for the optional description field).
func strPtr(s string) *string { return &s }

// isNoRows reports whether err is the database "no rows" error. We delegate to
// database.IsNoRows so the rbac package picks up pgx.ErrNoRows via errors.Is
// (the same way every other package does).
func isNoRows(err error) bool { return database.IsNoRows(err) }
