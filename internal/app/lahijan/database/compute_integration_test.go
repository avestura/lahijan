// Package database: compute_integration_test.go exercises the compute_*
// repositories against a real Postgres via testcontainers-go. Covers the
// tenant-scoping rule (WS-14 DoD: "tenant A cannot see/manage tenant B")
// at the repository seam, plus the soft-delete + unique-name invariants.
//
// Run with:  go test -tags integration ./internal/app/lahijan/database/...

//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestComputeInstances_CreateAndGet exercises the happy-path CRUD against a
// single tenant: create, read by id, read by name, soft-delete.
func TestComputeInstances_CreateAndGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantCtx := withTenant(ctx, tenant.ID)

	row, err := repos.ComputeInstances.Create(tenantCtx, database.CreateComputeInstanceParams{
		ProjectName: "lahijan-tenant-" + tenant.ID.String(),
		Name:        "web-01",
		Type:        database.InstanceTypeContainer,
		ImageAlias:  "ubuntu/24.04",
	})
	require.NoError(t, err, "create instance row")
	assert.NotEqual(t, uuid.Nil, row.ID)
	assert.Equal(t, tenant.ID, row.TenantID)
	assert.Equal(t, "web-01", row.Name)
	assert.Equal(t, database.InstanceStatusStopped, row.Status)
	assert.Equal(t, int32(database.StatusCodeStopped), row.StatusCode)
	assert.Equal(t, []string{}, row.Profiles)

	got, err := repos.ComputeInstances.Get(tenantCtx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	byName, err := repos.ComputeInstances.GetByName(tenantCtx, row.Name)
	require.NoError(t, err)
	assert.Equal(t, row.ID, byName.ID)

	// Soft-delete leaves the row in place (deleted_at set) so historical
	// joins still work. A subsequent Get returns ErrNoRows.
	require.NoError(t, repos.ComputeInstances.SoftDelete(tenantCtx, row.ID))
	_, err = repos.ComputeInstances.Get(tenantCtx, row.ID)
	require.Error(t, err, "soft-deleted row should not be visible via Get")
}

// TestComputeInstances_TenantIsolation asserts the WS-14 DoD: "tenant A
// cannot see/manage tenant B's instances". Every repository method that
// takes a tenant context MUST filter out other tenants' rows.
func TestComputeInstances_TenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())

	// Tenant A creates an instance.
	row, err := repos.ComputeInstances.Create(withTenant(ctx, tenantA.ID), database.CreateComputeInstanceParams{
		ProjectName: "lahijan-tenant-" + tenantA.ID.String(),
		Name:        "isolated-instance",
		ImageAlias:  "ubuntu/24.04",
	})
	require.NoError(t, err)

	// Tenant B cannot read it by id.
	_, err = repos.ComputeInstances.Get(withTenant(ctx, tenantB.ID), row.ID)
	require.Error(t, err, "cross-tenant get by id must fail")

	// Tenant B cannot read it by name (same name; the row belongs to A).
	_, err = repos.ComputeInstances.GetByName(withTenant(ctx, tenantB.ID), row.Name)
	require.Error(t, err, "cross-tenant get by name must fail")

	// Tenant B cannot update its status.
	err = repos.ComputeInstances.SetStatus(withTenant(ctx, tenantB.ID), row.ID,
		database.InstanceStatusRunning, int32(database.StatusCodeRunning))
	require.NoError(t, err, "status update is no-op on foreign rows (id+tenant filter)")
	// Verify tenant A still sees the original status.
	gotA, _ := repos.ComputeInstances.Get(withTenant(ctx, tenantA.ID), row.ID)
	assert.Equal(t, database.InstanceStatusStopped, gotA.Status)

	// Tenant B cannot list it.
	rowsB, err := repos.ComputeInstances.List(withTenant(ctx, tenantB.ID), 100, 0)
	require.NoError(t, err)
	assert.Empty(t, rowsB, "tenant B must not see tenant A's instances in list")

	// Tenant B cannot soft-delete it.
	require.NoError(t, repos.ComputeInstances.SoftDelete(withTenant(ctx, tenantB.ID), row.ID))
	// Tenant A still sees the row.
	_, err = repos.ComputeInstances.Get(withTenant(ctx, tenantA.ID), row.ID)
	require.NoError(t, err, "tenant A's row must survive tenant B's soft-delete attempt")
}

// TestComputeInstances_UniqueNameWithinTenant covers the partial unique
// index uq_compute_instances_tenant_name (WHERE deleted_at IS NULL). Two
// active instances in the same tenant with the same name is forbidden;
// after one is soft-deleted the name can be reused.
func TestComputeInstances_UniqueNameWithinTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantCtx := withTenant(ctx, tenant.ID)

	_, err := repos.ComputeInstances.Create(tenantCtx, database.CreateComputeInstanceParams{
		ProjectName: "p", Name: "dup-name", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = repos.ComputeInstances.Create(tenantCtx, database.CreateComputeInstanceParams{
		ProjectName: "p", Name: "dup-name", ImageAlias: "ubuntu/24.04",
	})
	require.Error(t, err, "duplicate name within tenant must fail")
	require.True(t, database.IsUniqueViolation(err), "expected unique_violation, got %v", err)

	// Soft-delete the first row; the name is now free.
	rows, err := repos.ComputeInstances.List(tenantCtx, 100, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, repos.ComputeInstances.SoftDelete(tenantCtx, rows[0].ID))

	// The name can now be reused.
	_, err = repos.ComputeInstances.Create(tenantCtx, database.CreateComputeInstanceParams{
		ProjectName: "p", Name: "dup-name", ImageAlias: "debian/12",
	})
	require.NoError(t, err, "name reuse after soft-delete must succeed")
}

// TestComputeImages_FeaturedSeedAndCustomUpload covers the image catalog:
// featured rows are seeded (source=featured), custom rows are uploaded
// (source=custom), and the unique (tenant_id, alias) constraint holds.
func TestComputeImages_FeaturedSeedAndCustomUpload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantCtx := withTenant(ctx, tenant.ID)

	// Featured seed.
	feat, err := repos.ComputeImages.Create(tenantCtx, database.CreateComputeImageParams{
		Alias: "ubuntu/24.04", Source: database.ImageSourceFeatured,
	})
	require.NoError(t, err)
	assert.Equal(t, database.ImageSourceFeatured, feat.Source)
	assert.Equal(t, "", feat.Fingerprint, "featured row has empty fingerprint until resolved")

	// Custom upload.
	cust, err := repos.ComputeImages.Create(tenantCtx, database.CreateComputeImageParams{
		Alias: "my-app-v1", Source: database.ImageSourceCustom, Fingerprint: "abc123",
	})
	require.NoError(t, err)
	assert.Equal(t, database.ImageSourceCustom, cust.Source)
	assert.Equal(t, "abc123", cust.Fingerprint)

	// Alias uniqueness within tenant.
	_, err = repos.ComputeImages.Create(tenantCtx, database.CreateComputeImageParams{
		Alias: "ubuntu/24.04", Source: database.ImageSourceFeatured,
	})
	require.Error(t, err)
	require.True(t, database.IsUniqueViolation(err))

	// Alias is free in a different tenant.
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	_, err = repos.ComputeImages.Create(withTenant(ctx, tenantB.ID), database.CreateComputeImageParams{
		Alias: "ubuntu/24.04", Source: database.ImageSourceFeatured,
	})
	require.NoError(t, err, "alias is tenant-scoped; cross-tenant dup is allowed")
}

// TestComputeProfiles_BasicCRUD covers create/get/list/soft-delete for
// the profile catalog. The shape mirrors the instance + image tests so
// the lint pass treats them as a single integration surface.
func TestComputeProfiles_BasicCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantCtx := withTenant(ctx, tenant.ID)

	row, err := repos.ComputeProfiles.Create(tenantCtx, database.CreateComputeProfileParams{
		Name:        "small",
		Description: "1 vCPU, 1GiB RAM",
	})
	require.NoError(t, err)
	assert.Equal(t, "small", row.Name)

	got, err := repos.ComputeProfiles.Get(tenantCtx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	byName, err := repos.ComputeProfiles.GetByName(tenantCtx, row.Name)
	require.NoError(t, err)
	assert.Equal(t, row.ID, byName.ID)

	count, err := repos.ComputeProfiles.Count(tenantCtx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	require.NoError(t, repos.ComputeProfiles.SoftDelete(tenantCtx, row.ID))
	countAfter, err := repos.ComputeProfiles.Count(tenantCtx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), countAfter)
}

// TestComputeNetworks_BasicCRUD mirrors the profile test for networks.
func TestComputeNetworks_BasicCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantCtx := withTenant(ctx, tenant.ID)

	row, err := repos.ComputeNetworks.Create(tenantCtx, database.CreateComputeNetworkParams{
		Name:        "lan",
		Description: "tenant LAN",
		Type:        database.NetworkTypeBridge,
	})
	require.NoError(t, err)
	assert.Equal(t, "lan", row.Name)
	assert.Equal(t, database.NetworkTypeBridge, row.Type)

	got, err := repos.ComputeNetworks.Get(tenantCtx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	require.NoError(t, repos.ComputeNetworks.SoftDelete(tenantCtx, row.ID))
	_, err = repos.ComputeNetworks.Get(tenantCtx, row.ID)
	require.Error(t, err)
}

// TestComputeStorageVolumes_BasicCRUD mirrors the profile test for volumes.
func TestComputeStorageVolumes_BasicCRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantCtx := withTenant(ctx, tenant.ID)

	row, err := repos.ComputeStorageVolumes.Create(tenantCtx, database.CreateComputeStorageVolumeParams{
		Name:        "data",
		Description: "shared data volume",
		PoolName:    "default",
	})
	require.NoError(t, err)
	assert.Equal(t, "data", row.Name)
	assert.Equal(t, "default", row.PoolName)
	assert.Equal(t, database.VolumeTypeCustom, row.Type)

	got, err := repos.ComputeStorageVolumes.Get(tenantCtx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	byName, err := repos.ComputeStorageVolumes.GetByName(tenantCtx, "default", "data")
	require.NoError(t, err)
	assert.Equal(t, row.ID, byName.ID)

	require.NoError(t, repos.ComputeStorageVolumes.SoftDelete(tenantCtx, row.ID))
}
