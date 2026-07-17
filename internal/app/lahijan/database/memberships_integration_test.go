// Package database: memberships_integration_test.go verifies the tenant-scoping
// discipline end to end. memberships is the canonical tenant-scoped table; its
// repository must (a) refuse to run without a tenant in the context, and (b)
// only ever return rows that belong to the tenant baked into the context.
// A later cross-tenant query (tenant A context, tenant B's row) must NOT find
// the row — the core guarantee of ADR-0002.

//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

func TestMemberships_RepoRejectsMissingTenantContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	// No tenant baked into the context -> repo must fail closed with the
	// sentinel, regardless of which method is called.
	_, err := repos.Memberships.Create(context.Background(), user.ID, nil)
	require.ErrorIs(t, err, database.ErrNoTenantInContext)

	_, err = repos.Memberships.ListForTenant(context.Background(), 10, 0)
	require.ErrorIs(t, err, database.ErrNoTenantInContext)

	_, err = repos.Memberships.CountForTenant(context.Background())
	require.ErrorIs(t, err, database.ErrNoTenantInContext)
}

func TestMemberships_EnforcesTenantScoping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	// Two tenants, one user, one membership in tenant A only.
	tenantA := testutil.NewTenant(ctx, t, pool)
	tenantB := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)

	ctxA := database.WithTenant(ctx, tenantA.ID)
	ctxB := database.WithTenant(ctx, tenantB.ID)

	membership, err := repos.Memberships.Create(ctxA, user.ID, nil)
	require.NoError(t, err, "create membership in tenant A")
	require.Equal(t, tenantA.ID, membership.TenantID, "membership must be stored in tenant A")

	// Tenant A context can see the membership.
	got, err := repos.Memberships.Get(ctxA, membership.ID)
	require.NoError(t, err)
	assert.Equal(t, membership.ID, got.ID)

	// Tenant B context must NOT see tenant A's membership (no leak).
	_, err = repos.Memberships.Get(ctxB, membership.ID)
	assert.Error(t, err, "tenant B must not read tenant A's membership")

	countA, err := repos.Memberships.CountForTenant(ctxA)
	require.NoError(t, err)
	assert.Equal(t, int64(1), countA, "tenant A sees its membership")

	countB, err := repos.Memberships.CountForTenant(ctxB)
	require.NoError(t, err)
	assert.Equal(t, int64(0), countB, "tenant B sees nothing from tenant A")
}

func TestMemberships_ListForUser_CrossTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	tenantA := testutil.NewTenant(ctx, t, pool)
	tenantB := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, false)

	ctxA := database.WithTenant(ctx, tenantA.ID)
	ctxB := database.WithTenant(ctx, tenantB.ID)

	_, err := repos.Memberships.Create(ctxA, user.ID, nil)
	require.NoError(t, err)
	_, err = repos.Memberships.Create(ctxB, user.ID, nil)
	require.NoError(t, err)

	// The cross-tenant discovery query is intentionally not tenant-scoped: it
	// returns every tenant the user belongs to.
	all, err := repos.Memberships.ListForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, all, 2, "user should have one membership per tenant")
}
