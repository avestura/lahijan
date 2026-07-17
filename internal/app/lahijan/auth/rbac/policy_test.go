// Package rbac: policy_test.go covers the policy evaluator without touching
// Postgres. A fake membershipLookup drives the test cases so the assertions
// don't depend on the DB state.

package rbac

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLookup is a hand-written stub of the unexported membershipLookup
// interface. It returns whatever the test wired.
type fakeLookup struct {
	roleSlug       string
	roleMembership bool
	roleErr        error
	perms          []string
	permsErr       error
}

func (f *fakeLookup) RoleSlugForUser(_ context.Context, _, _ uuid.UUID) (string, bool, error) {
	return f.roleSlug, f.roleMembership, f.roleErr
}

func (f *fakeLookup) PermissionSlugsForUser(_ context.Context, _, _ uuid.UUID) ([]string, error) {
	return f.perms, f.permsErr
}

func TestEvaluator_HasPermission_PlatformAdminBypasses(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{
		roleSlug:       RolePlatformAdmin,
		roleMembership: true,
	})
	got, err := e.HasPermission(context.Background(), uuid.New(), uuid.New(), PermComputeInstanceDelete)
	require.NoError(t, err)
	assert.True(t, got, "platform.admin should bypass every check")
}

func TestEvaluator_HasPermission_PlatformAdminCannotForgeUnknownSlug(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{
		roleSlug:       RolePlatformAdmin,
		roleMembership: true,
	})
	got, err := e.HasPermission(context.Background(), uuid.New(), uuid.New(), "not.a.real.permission")
	require.NoError(t, err)
	assert.False(t, got, "platform.admin bypass must still require the slug to exist in the registry")
}

func TestEvaluator_HasPermission_NonMemberDenies(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{
		roleMembership: false,
	})
	got, err := e.HasPermission(context.Background(), uuid.New(), uuid.New(), PermAuditRead)
	require.NoError(t, err)
	assert.False(t, got, "non-member must be denied")
}

func TestEvaluator_HasPermission_GrantInCatalogAllows(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{
		roleSlug:       RoleTenantMember,
		roleMembership: true,
		perms:          []string{PermAuditRead, PermTenantRead},
	})
	got, err := e.HasPermission(context.Background(), uuid.New(), uuid.New(), PermAuditRead)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestEvaluator_HasPermission_MissingGrantDenies(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{
		roleSlug:       RoleTenantMember,
		roleMembership: true,
		perms:          []string{PermAuditRead},
	})
	got, err := e.HasPermission(context.Background(), uuid.New(), uuid.New(), PermAuditReadGlobal)
	require.NoError(t, err)
	assert.False(t, got, "permission not in user's catalog must be denied")
}

func TestEvaluator_HasPermission_LookupErrorFailsClosed(t *testing.T) {
	t.Parallel()
	dbErr := errors.New("connection refused")
	e := NewEvaluator(&fakeLookup{
		roleMembership: false,
		roleErr:        dbErr,
	})
	got, err := e.HasPermission(context.Background(), uuid.New(), uuid.New(), PermAuditRead)
	require.Error(t, err, "DB errors must surface, not silently deny")
	assert.False(t, got, "fail closed on DB error")
}

func TestEvaluator_PermissionsForUser_NonMemberReturnsEmpty(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{roleMembership: false})
	got, err := e.PermissionsForUser(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestEvaluator_PermissionsForUser_PlatformAdminReturnsFullCatalog(t *testing.T) {
	t.Parallel()
	e := NewEvaluator(&fakeLookup{
		roleSlug:       RolePlatformAdmin,
		roleMembership: true,
		// perms is intentionally empty; the platform.admin override must
		// ignore it and return the registry catalog instead.
	})
	got, err := e.PermissionsForUser(context.Background(), uuid.New(), uuid.New())
	require.NoError(t, err)
	assert.Len(t, got, len(allPermissions), "platform.admin should see the full registry")
}
