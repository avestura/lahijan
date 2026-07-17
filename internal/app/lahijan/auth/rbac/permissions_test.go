// Package rbac: permissions_test.go covers the registry's invariants without
// touching the database. These tests catch typos and drift before the seeder
// even runs.

package rbac

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllPermissions_AreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, len(allPermissions))
	for _, p := range allPermissions {
		_, dup := seen[p.Slug]
		require.Falsef(t, dup, "permission slug %q appears more than once in the registry", p.Slug)
		seen[p.Slug] = struct{}{}
	}
}

func TestAllPermissions_FollowScopeActionFormat(t *testing.T) {
	t.Parallel()
	for _, p := range allPermissions {
		assert.Truef(
			t,
			strings.Count(p.Slug, ".") >= 1,
			"permission %q must be at least 'scope.action', got no dot", p.Slug,
		)
		// Slugs must be lowercase ASCII; no spaces, no uppercase.
		assert.Truef(t, p.Slug == strings.ToLower(p.Slug),
			"permission %q must be lowercase", p.Slug)
		assert.NotContainsf(t, p.Slug, " ",
			"permission %q must not contain spaces", p.Slug)
	}
}

func TestAllPermissions_HaveNonEmptyDescription(t *testing.T) {
	t.Parallel()
	for _, p := range allPermissions {
		assert.NotEmptyf(t, p.Description, "permission %q is missing a description", p.Slug)
	}
}

func TestAllPermissions_IncludesCoreAuditAndRBACReads(t *testing.T) {
	t.Parallel()
	// These are load-bearing for WS-08 itself: the audit query API needs
	// audit.read on every default viewer/member/admin/owner, and the RBAC
	// UI needs rbac.role.list.
	mustHave := []string{
		PermAuditRead,
		PermAuditReadGlobal,
		PermAuditExport,
		PermRBACRoleList,
		PermTenantMemberList,
	}
	registry := make(map[string]struct{}, len(allPermissions))
	for _, p := range allPermissions {
		registry[p.Slug] = struct{}{}
	}
	for _, slug := range mustHave {
		_, ok := registry[slug]
		assert.Truef(t, ok, "permission %q must exist in the registry", slug)
	}
}

func TestPermissionExists(t *testing.T) {
	t.Parallel()
	assert.True(t, PermissionExists(PermComputeInstanceCreate))
	assert.False(t, PermissionExists("not.a.real.permission"))
	assert.False(t, PermissionExists(""))
}

func TestAllPermissions_ReturnsACopy(t *testing.T) {
	t.Parallel()
	a := AllPermissions()
	b := AllPermissions()
	// Mutating the returned slice must not affect the package var.
	a[0].Description = "MUTATED"
	require.NotEqual(t, a[0].Description, b[0].Description,
		"AllPermissions must return a defensive copy")
}

// TestAllPermissions_SortedWithinModule keeps the registry tidy so diffs in
// code review are obvious. We don't enforce global sort (modules are grouped),
// only within-module.
func TestAllPermissions_SortedWithinModule(t *testing.T) {
	t.Parallel()
	modules := make(map[string][]string)
	for _, p := range allPermissions {
		mod := p.Slug[:strings.IndexByte(p.Slug, '.')]
		modules[mod] = append(modules[mod], p.Slug)
	}
	for mod, slugs := range modules {
		sorted := append([]string(nil), slugs...)
		sort.Strings(sorted)
		assert.Equalf(t, sorted, slugs,
			"permissions in module %q must be alphabetized within the module", mod)
	}
}
