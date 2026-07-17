// Package database: rbac_integration_test.go verifies the RBAC pipeline end
// to end against a real Postgres. The seeder is idempotent; the policy
// evaluator joins memberships + role_permissions; the cross-tenant isolation
// invariant holds (a user with no membership in tenant X has zero permissions
// there even if they're a platform.admin elsewhere).

//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

func TestRBACSeeder_SeedsAllPermissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	perms, err := repos.RBAC.ListPermissions(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(perms), len(rbac.AllPermissions()),
		"seeder must insert every permission in the registry")

	// Idempotency: running again must not duplicate anything.
	require.NoError(t, rbac.SeedOnce(ctx, repos))
	perms2, err := repos.RBAC.ListPermissions(ctx)
	require.NoError(t, err)
	assert.Len(t, perms2, len(perms), "re-running the seeder must not duplicate permissions")
}

func TestRBACSeeder_SeedsDefaultRoles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	for _, slug := range []string{
		rbac.RoleTenantOwner, rbac.RoleTenantAdmin,
		rbac.RoleTenantMember, rbac.RoleTenantViewer,
		rbac.RolePlatformAdmin,
	} {
		role, err := repos.RBAC.GetRoleBySlug(ctx, slug)
		require.NoError(t, err, "default role %s must exist after seeding", slug)
		assert.True(t, role.IsSystem, "default role %s must be is_system=true", slug)
	}
}

func TestRBACSeeder_GrantsAllPermissionsToOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	owner, err := repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantOwner)
	require.NoError(t, err)
	perms, err := repos.RBAC.ListPermissionsForRole(ctx, owner.ID)
	require.NoError(t, err)
	// Every non-platform.* permission must be granted to owner.
	want := 0
	for _, p := range rbac.AllPermissions() {
		if len(p.Slug) >= 9 && p.Slug[:9] != "platform." {
			want++
		}
	}
	assert.Len(t, perms, want, "owner role must grant every non-platform permission")
}

func TestRBACPolicy_UserHoldsPermission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	role, err := repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantViewer)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)

	evaluator := rbac.NewEvaluator(repos.Memberships)

	// Viewer has audit.read.
	got, err := evaluator.HasPermission(ctx, user.ID, tenant.ID, rbac.PermAuditRead)
	require.NoError(t, err)
	assert.True(t, got, "viewer should have audit.read")

	// Viewer does NOT have audit.export.
	got, err = evaluator.HasPermission(ctx, user.ID, tenant.ID, rbac.PermAuditExport)
	require.NoError(t, err)
	assert.False(t, got, "viewer should NOT have audit.export")
}

func TestRBACPolicy_NonMemberDenied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	// No membership for user in tenant.

	evaluator := rbac.NewEvaluator(repos.Memberships)
	got, err := evaluator.HasPermission(ctx, user.ID, tenant.ID, rbac.PermAuditRead)
	require.NoError(t, err)
	assert.False(t, got, "non-member must be denied")
}

func TestRBACPolicy_TenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	tenantA := testutil.NewTenant(ctx, t, pool)
	tenantB := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	role, err := repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantAdmin)
	require.NoError(t, err)

	// Member of tenant A only.
	testutil.NewMembership(ctx, t, pool, tenantA.ID, user.ID, &role.ID)

	evaluator := rbac.NewEvaluator(repos.Memberships)

	// Has admin permissions in tenant A.
	got, err := evaluator.HasPermission(ctx, user.ID, tenantA.ID, rbac.PermAuditExport)
	require.NoError(t, err)
	assert.True(t, got, "admin in tenant A should have audit.export there")

	// Has NOTHING in tenant B.
	got, err = evaluator.HasPermission(ctx, user.ID, tenantB.ID, rbac.PermAuditRead)
	require.NoError(t, err)
	assert.False(t, got, "must not have permissions in tenant B (no membership)")
}

func TestRBACPolicy_PermissionsForUser_ReturnsFullCatalogForPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	require.NoError(t, rbac.SeedOnce(ctx, repos))

	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	role, err := repos.RBAC.GetRoleBySlug(ctx, rbac.RolePlatformAdmin)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)

	evaluator := rbac.NewEvaluator(repos.Memberships)

	// platform.admin bypass: should be able to do anything in the catalog.
	got, err := evaluator.HasPermission(ctx, user.ID, tenant.ID, rbac.PermAuditReadGlobal)
	require.NoError(t, err)
	assert.True(t, got, "platform.admin should bypass every check")

	// PermissionsForUser returns the full registry.
	perms, err := evaluator.PermissionsForUser(ctx, user.ID, tenant.ID)
	require.NoError(t, err)
	assert.Len(t, perms, len(rbac.AllPermissions()),
		"platform.admin should see the entire registry")
}

// Sanity check: database.MembershipsRepository.PermissionSlugsForUser satisfies
// the rbac Evaluator's lookup interface and returns the right slugs.
func TestMembershipsRepository_PermissionSlugsForUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	require.NoError(t, rbac.SeedOnce(ctx, repos))

	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)
	role, err := repos.RBAC.GetRoleBySlug(ctx, rbac.RoleTenantMember)
	require.NoError(t, err)
	testutil.NewMembership(ctx, t, pool, tenant.ID, user.ID, &role.ID)

	slugs, err := repos.Memberships.PermissionSlugsForUser(ctx, user.ID, tenant.ID)
	require.NoError(t, err)
	// Member role has audit.read but not audit.export.
	assert.Contains(t, slugs, rbac.PermAuditRead)
	for _, s := range slugs {
		assert.NotEqual(t, rbac.PermAuditExport, s, "member must not have audit.export")
	}
}
