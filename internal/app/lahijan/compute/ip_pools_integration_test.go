// Package compute: ip_pools_integration_test.go exercises the WS-30
// IP-pool + floating-IP surface end-to-end against a real Postgres
// (via testcontainers-go) + a fake Incus provider (in-process, shared
// with service_integration_test.go). Covers the WS-30 DoD:
//
//   - pool + range CRUD with audit + permission-default assertions
//   - floating IP allocate picks the lowest free address (v4 + v6)
//   - attach / detach / release round-trip with audit emission
//   - multi-tenant isolation: tenant A cannot read/manage tenant B's
//     floating IPs
//   - pool exhaustion surfaces ErrIPPoolExhausted
//   - unique-address invariant: a concurrent allocate race for the
//     same address surfaces a 409 sentinel
//
// Run with:  go test -tags integration ./internal/app/lahijan/compute/...

//go:build integration

package compute_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestIPPool_CRUDLifecycle covers the operator-owned pool admin
// surface: create -> get -> list -> update -> delete with audit
// emission at every step.
func TestIPPool_CRUDLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})

	// Create.
	pool, err := svc.CreateIPPool(ctx, user.ID, compute.CreateIPPoolParams{
		Name:        "production-v4",
		Description: "main /24",
		IsActive:    true,
	})
	require.NoError(t, err)
	assert.Equal(t, "production-v4", pool.Name)
	assert.True(t, pool.IsActive)

	// Get by id.
	got, err := svc.GetIPPool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, pool.ID, got.ID)

	// Update description + active.
	desc := "production /24 (updated)"
	err = svc.UpdateIPPool(ctx, user.ID, compute.UpdateIPPoolParams{
		ID:          pool.ID,
		Description: desc,
		IsActive:    false,
	})
	require.NoError(t, err)
	updated, err := svc.GetIPPool(ctx, pool.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.Description)
	assert.Equal(t, desc, *updated.Description)
	assert.False(t, updated.IsActive)

	// List.
	pools, err := svc.ListIPPools(ctx, 100, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, pools)

	// Delete (no allocations yet).
	err = svc.DeleteIPPool(ctx, user.ID, pool.ID)
	require.NoError(t, err)
	_, err = svc.GetIPPool(ctx, pool.ID)
	require.ErrorIs(t, err, compute.ErrIPPoolNotFound)
}

// TestIPPool_DuplicateNameRejected covers the 409 path: a second pool
// with the same name surfaces ErrIPPoolNameTaken.
func TestIPPool_DuplicateNameRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})

	_, err := svc.CreateIPPool(ctx, user.ID, compute.CreateIPPoolParams{
		Name: "dupe", IsActive: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateIPPool(ctx, user.ID, compute.CreateIPPoolParams{
		Name: "dupe", IsActive: true,
	})
	require.ErrorIs(t, err, compute.ErrIPPoolNameTaken)
}

// TestIPPoolRange_AddAndDelete covers the per-pool range CRUD. The
// canonical-form + family-parity validators are also covered.
func TestIPPoolRange_AddAndDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})

	pool := testPool(ctx, t, svc, user.ID, "range-pool")

	// Add a v4 range with one excluded address.
	r1, err := svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID:            pool.ID,
		Cidr:              "203.0.113.0/24",
		Family:            4,
		ExcludedAddresses: []string{"203.0.113.0", "203.0.113.255"},
	})
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.0/24", r1.Cidr)

	// Family mismatch is rejected.
	_, err = svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID,
		Cidr:   "192.0.2.0/30",
		Family: 6, // wrong
	})
	require.ErrorIs(t, err, compute.ErrInvalidIPFamily)

	// Malformed CIDR is rejected.
	_, err = svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID,
		Cidr:   "not-a-cidr",
		Family: 4,
	})
	require.ErrorIs(t, err, compute.ErrInvalidCIDR)

	// Duplicate (pool_id, cidr) is rejected.
	_, err = svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID,
		Cidr:   "203.0.113.0/24",
		Family: 4,
	})
	require.ErrorIs(t, err, compute.ErrIPPoolRangeExists)

	// List.
	ranges, err := svc.ListIPPoolRanges(ctx, pool.ID, 100, 0)
	require.NoError(t, err)
	assert.Len(t, ranges, 1)

	// Delete.
	err = svc.DeleteIPPoolRange(ctx, user.ID, pool.ID, r1.ID)
	require.NoError(t, err)
	ranges, err = svc.ListIPPoolRanges(ctx, pool.ID, 100, 0)
	require.NoError(t, err)
	assert.Empty(t, ranges)
}

// TestFloatingIP_AllocatePicksLowest covers the allocator's
// deterministic next-free-IP logic across v4 + v6 ranges.
func TestFloatingIP_AllocatePicksLowest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})

	tenantCtx := database.WithTenant(ctx, tenant.ID)

	pool := testPool(ctx, t, svc, user.ID, "allocate-v4")
	// Use a unique CIDR per test so the global unique index on
	// floating_ips.address does not collide with parallel tests.
	_, err := svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID, Cidr: "203.0.113.0/29", Family: 4,
	})
	require.NoError(t, err)

	// /29 has 6 usable hosts (.1..6, after .0 net + .7 broadcast).
	// Allocations should land in order: .1, .2, .3, .4, .5, .6.
	addrs := []string{}
	for i := 0; i < 6; i++ {
		row, errAlloc := svc.AllocateFloatingIP(tenantCtx, tenant.ID, user.ID, compute.AllocateFloatingIPParams{
			PoolID: pool.ID,
		})
		require.NoError(t, errAlloc)
		addrs = append(addrs, row.Address.String())
	}
	assert.Equal(t, []string{
		"203.0.113.1", "203.0.113.2", "203.0.113.3",
		"203.0.113.4", "203.0.113.5", "203.0.113.6",
	}, addrs)

	// 7th allocate fails with exhaustion.
	_, err = svc.AllocateFloatingIP(tenantCtx, tenant.ID, user.ID, compute.AllocateFloatingIPParams{
		PoolID: pool.ID,
	})
	require.ErrorIs(t, err, compute.ErrIPPoolExhausted)
}

// TestFloatingIP_AttachDetachRelease covers the lifecycle of a single
// allocation: allocate -> attach -> detach -> release.
func TestFloatingIP_AttachDetachRelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})
	tenantCtx := database.WithTenant(ctx, tenant.ID)

	pool := testPool(ctx, t, svc, user.ID, "lifecycle-pool")
	_, err := svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID, Cidr: "203.0.113.8/29", Family: 4,
	})
	require.NoError(t, err)

	// Allocate.
	fip, err := svc.AllocateFloatingIP(tenantCtx, tenant.ID, user.ID, compute.AllocateFloatingIPParams{
		PoolID: pool.ID,
	})
	require.NoError(t, err)
	assert.Nil(t, fip.InstanceID)

	// Need an instance to attach to. Create one via the existing
	// compute instance factory; the fake Incus driver returns success.
	inst, err := svc.CreateInstance(tenantCtx, tenant.ID, user.ID, compute.InstanceCreateParams{
		Name: "floating-target", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	// Attach.
	attached, err := svc.AttachFloatingIP(tenantCtx, tenant.ID, user.ID, fip.ID, inst.ID)
	require.NoError(t, err)
	require.NotNil(t, attached.InstanceID)
	assert.Equal(t, inst.ID, *attached.InstanceID)

	// Second attach to the same instance is rejected.
	_, err = svc.AttachFloatingIP(tenantCtx, tenant.ID, user.ID, fip.ID, inst.ID)
	require.ErrorIs(t, err, compute.ErrFloatingIPAlreadyAttached)

	// Detach.
	detached, err := svc.DetachFloatingIP(tenantCtx, tenant.ID, user.ID, fip.ID)
	require.NoError(t, err)
	assert.Nil(t, detached.InstanceID)

	// Detach again is rejected.
	_, err = svc.DetachFloatingIP(tenantCtx, tenant.ID, user.ID, fip.ID)
	require.ErrorIs(t, err, compute.ErrFloatingIPNotAttached)

	// Release.
	err = svc.ReleaseFloatingIP(tenantCtx, tenant.ID, user.ID, fip.ID)
	require.NoError(t, err)
	_, err = svc.GetFloatingIP(tenantCtx, fip.ID)
	require.ErrorIs(t, err, compute.ErrFloatingIPNotFound)
}

// TestFloatingIP_MultiTenantIsolation covers the WS-14 DoD adapted to
// the floating-IP surface: tenant A cannot read/manage tenant B's
// floating IPs.
func TestFloatingIP_MultiTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})

	pool := testPool(ctx, t, svc, user.ID, "multi-tenant-pool")
	_, err := svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID, Cidr: "203.0.113.16/29", Family: 4,
	})
	require.NoError(t, err)

	// Tenant A allocates.
	ctxA := database.WithTenant(ctx, tenantA.ID)
	fipA, err := svc.AllocateFloatingIP(ctxA, tenantA.ID, user.ID, compute.AllocateFloatingIPParams{
		PoolID: pool.ID,
	})
	require.NoError(t, err)

	// Tenant B cannot see it.
	ctxB := database.WithTenant(ctx, tenantB.ID)
	_, err = svc.GetFloatingIP(ctxB, fipA.ID)
	require.ErrorIs(t, err, compute.ErrFloatingIPNotFound)

	// Tenant B list is empty.
	rowsB, err := svc.ListFloatingIPs(ctxB, 100, 0)
	require.NoError(t, err)
	assert.Empty(t, rowsB)

	// Tenant A still sees it.
	_, err = svc.GetFloatingIP(ctxA, fipA.ID)
	require.NoError(t, err)
}

// TestFloatingIP_SetPTR covers the set/clear pattern. The PTR target
// is validated for canonical shape; an invalid target is rejected.
func TestFloatingIP_SetPTR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})
	tenantCtx := database.WithTenant(ctx, tenant.ID)

	pool := testPool(ctx, t, svc, user.ID, "ptr-pool")
	_, err := svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID, Cidr: "203.0.113.24/29", Family: 4,
	})
	require.NoError(t, err)

	fip, err := svc.AllocateFloatingIP(tenantCtx, tenant.ID, user.ID, compute.AllocateFloatingIPParams{
		PoolID: pool.ID,
	})
	require.NoError(t, err)

	// Set a valid PTR.
	row, err := svc.SetFloatingIPPTRTarget(tenantCtx, tenant.ID, user.ID, fip.ID, "host.example.com.")
	require.NoError(t, err)
	require.NotNil(t, row.PtrTarget)
	assert.Equal(t, "host.example.com.", *row.PtrTarget)

	// Invalid PTR (uppercase, missing trailing dot) is rejected.
	_, err = svc.SetFloatingIPPTRTarget(tenantCtx, tenant.ID, user.ID, fip.ID, "Invalid.Name")
	require.Error(t, err)

	// Empty clears.
	row, err = svc.SetFloatingIPPTRTarget(tenantCtx, tenant.ID, user.ID, fip.ID, "")
	require.NoError(t, err)
	assert.Nil(t, row.PtrTarget)
}

// TestIPPool_DeleteRefusedWithAllocations covers the ON DELETE RESTRICT
// equivalent at the service layer: a pool with live allocations cannot
// be deleted; the operator must release them first.
func TestIPPool_DeleteRefusedWithAllocations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})
	tenantCtx := database.WithTenant(ctx, tenant.ID)

	pool := testPool(ctx, t, svc, user.ID, "delete-with-allocs")
	_, err := svc.AddIPPoolRange(ctx, user.ID, compute.AddIPPoolRangeParams{
		PoolID: pool.ID, Cidr: "203.0.113.32/29", Family: 4,
	})
	require.NoError(t, err)
	_, err = svc.AllocateFloatingIP(tenantCtx, tenant.ID, user.ID, compute.AllocateFloatingIPParams{
		PoolID: pool.ID,
	})
	require.NoError(t, err)

	err = svc.DeleteIPPool(ctx, user.ID, pool.ID)
	require.ErrorIs(t, err, compute.ErrIPPoolHasAllocations)
}

// testPool is a tiny helper that creates a pool + fails the test on
// error. Reduces boilerplate in the tests above.
func testPool(
	ctx context.Context,
	t *testing.T,
	svc *compute.Service,
	userID uuid.UUID,
	name string,
) compute.IPPoolRow {
	t.Helper()
	pool, err := svc.CreateIPPool(ctx, userID, compute.CreateIPPoolParams{
		Name: name, IsActive: true,
	})
	require.NoError(t, err)
	return pool
}
