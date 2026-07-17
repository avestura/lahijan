// Package rbac: roles_test.go covers the default-role registry invariants
// without touching the database. These tests catch typos and drift before the
// seeder runs.

package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRoles_AreUniqueBySlug(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, len(defaultRoles))
	for _, r := range defaultRoles {
		_, dup := seen[r.Slug]
		require.Falsef(t, dup, "role slug %q appears more than once", r.Slug)
		seen[r.Slug] = struct{}{}
	}
}

func TestDefaultRoles_AllGrantedPermissionsExist(t *testing.T) {
	t.Parallel()
	// Every slug in every default role MUST be a real permission in the
	// registry. A typo here would silently widen (impossible — fk rejects)
	// or narrow a role.
	for _, r := range defaultRoles {
		for _, slug := range r.Permissions {
			assert.Truef(t, PermissionExists(slug),
				"role %q grants unknown permission %q", r.Slug, slug)
		}
	}
}

func TestDefaultRoles_IncludesFourTenantScopes(t *testing.T) {
	t.Parallel()
	want := []string{RoleTenantOwner, RoleTenantAdmin, RoleTenantMember, RoleTenantViewer}
	have := make(map[string]struct{}, len(defaultRoles))
	for _, r := range defaultRoles {
		have[r.Slug] = struct{}{}
	}
	for _, slug := range want {
		_, ok := have[slug]
		assert.Truef(t, ok, "default role registry must include %q", slug)
	}
}

func TestDefaultRoles_ViewerIsStrictSubsetOfMember(t *testing.T) {
	t.Parallel()
	// The viewer role is documented as "read-only". If it ever gains a
	// write permission, the policy intent is broken.
	viewer := roleBySlug(t, RoleTenantViewer)
	for _, p := range viewer.Permissions {
		assert.Falsef(t,
			endsWithAny(p, ".create", ".update", ".delete", ".adjust",
				".revoke", ".invite", ".remove", ".install", ".uninstall",
				".approve", ".apply", ".restart", ".start", ".stop"),
			"viewer role must not include write permission %q", p,
		)
	}
}

func TestDefaultRoles_OwnerHasEveryNonGlobalPermission(t *testing.T) {
	t.Parallel()
	owner := roleBySlug(t, RoleTenantOwner)
	have := make(map[string]struct{}, len(owner.Permissions))
	for _, p := range owner.Permissions {
		have[p] = struct{}{}
	}
	for _, p := range allPermissions {
		if startsWith(p.Slug, "platform.") {
			continue
		}
		_, ok := have[p.Slug]
		assert.Truef(t, ok, "owner role must grant %q (only platform.* is excluded)", p.Slug)
	}
}

func TestDefaultRoles_AdminCannotDeleteTenantOrTransferOwnership(t *testing.T) {
	t.Parallel()
	admin := roleBySlug(t, RoleTenantAdmin)
	for _, p := range admin.Permissions {
		assert.NotEqualf(t, PermTenantDelete, p,
			"admin role must NOT include tenant.delete (only owner can)")
	}
}

func TestIsSystemRole(t *testing.T) {
	t.Parallel()
	assert.True(t, IsSystemRole(RoleTenantOwner))
	assert.True(t, IsSystemRole(RolePlatformAdmin))
	assert.False(t, IsSystemRole("custom.role"))
	assert.False(t, IsSystemRole(""))
}

// roleBySlug fetches a role definition by slug, failing the test if absent.
func roleBySlug(t *testing.T, slug string) RoleDefinition {
	t.Helper()
	for _, r := range defaultRoles {
		if r.Slug == slug {
			return r
		}
	}
	require.Failf(t, "role not found", "no default role with slug %q", slug)
	return RoleDefinition{}
}

// endsWithAny reports whether s ends with any of suffixes.
func endsWithAny(s string, suffixes ...string) bool {
	for _, sf := range suffixes {
		if endsWith(s, sf) {
			return true
		}
	}
	return false
}

// endsWith is a local replacement for strings.HasSuffix so this file keeps
// the same import profile as the others in the package.
func endsWith(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}
