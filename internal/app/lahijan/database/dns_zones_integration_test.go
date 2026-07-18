// Package database: dns_zones_integration_test.go exercises the dns_zones
// repository against a real Postgres via testcontainers-go. Covers the
// tenant-scoping rule (the WS-12 DoD item "tenant isolation: tenant A
// cannot list/modify tenant B's zones") at the repository seam.
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

// withTenant returns a context carrying tenantID for the duration of fn.
// Wraps database.WithTenant so each test reads cleanly.
func withTenant(parent context.Context, tenantID uuid.UUID) context.Context {
	return database.WithTenant(parent, tenantID)
}

func TestDNSZones_CreateAndGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())

	zone, err := repos.DNSZones.Create(withTenant(ctx, tenant.ID), database.CreateDNSZoneParams{
		CanonicalID: "create-and-get.example.com.",
		Name:        "create-and-get.example.com.",
		Kind:        database.DNSZoneKindNative,
		Description: "test zone",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, zone.ID)
	assert.Equal(t, "create-and-get.example.com.", zone.CanonicalID)
	assert.Equal(t, tenant.ID, zone.TenantID)
	assert.False(t, zone.IsDnssecEnabled)
	assert.False(t, zone.IsAxfrEnabled)

	got, err := repos.DNSZones.Get(withTenant(ctx, tenant.ID), zone.ID)
	require.NoError(t, err)
	assert.Equal(t, zone.ID, got.ID)

	byCanonical, err := repos.DNSZones.GetByCanonical(withTenant(ctx, tenant.ID), zone.CanonicalID)
	require.NoError(t, err)
	assert.Equal(t, zone.ID, byCanonical.ID)
}

func TestDNSZones_TenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())

	zone, err := repos.DNSZones.Create(withTenant(ctx, tenantA.ID), database.CreateDNSZoneParams{
		CanonicalID: "iso.example.com.",
		Name:        "iso.example.com.",
	})
	require.NoError(t, err)

	// Tenant B cannot read tenant A's zone by id.
	_, err = repos.DNSZones.Get(withTenant(ctx, tenantB.ID), zone.ID)
	require.Error(t, err, "cross-tenant get by id must fail")

	// Tenant B cannot read tenant A's zone by canonical id.
	_, err = repos.DNSZones.GetByCanonical(withTenant(ctx, tenantB.ID), zone.CanonicalID)
	require.Error(t, err, "cross-tenant get by canonical must fail")

	// Tenant B cannot list tenant A's zone.
	zonesB, err := repos.DNSZones.List(withTenant(ctx, tenantB.ID), 100, 0)
	require.NoError(t, err)
	assert.Empty(t, zonesB, "tenant B must see zero zones")

	// But tenant A sees the zone.
	zonesA, err := repos.DNSZones.List(withTenant(ctx, tenantA.ID), 100, 0)
	require.NoError(t, err)
	require.Len(t, zonesA, 1)
	assert.Equal(t, zone.ID, zonesA[0].ID)

	// Tenant B cannot delete tenant A's zone.
	require.NoError(t, repos.DNSZones.Delete(withTenant(ctx, tenantB.ID), zone.ID),
		"cross-tenant delete returns no error (0 rows affected) but does not delete")
	zonesA, err = repos.DNSZones.List(withTenant(ctx, tenantA.ID), 100, 0)
	require.NoError(t, err)
	require.Len(t, zonesA, 1, "tenant A's zone must still exist after tenant B's delete")
}

func TestDNSZones_CanonicalIDGloballyUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())

	_, err := repos.DNSZones.Create(withTenant(ctx, tenantA.ID), database.CreateDNSZoneParams{
		CanonicalID: "unique.example.com.",
		Name:        "unique.example.com.",
	})
	require.NoError(t, err)

	// Tenant B cannot create a zone with the same canonical id — the
	// unique index blocks it. This is the repository-seam enforcement of
	// "two tenants cannot own the same zone".
	_, err = repos.DNSZones.Create(withTenant(ctx, tenantB.ID), database.CreateDNSZoneParams{
		CanonicalID: "unique.example.com.",
		Name:        "unique.example.com.",
	})
	require.Error(t, err, "duplicate canonical_id across tenants must fail")

	// The admin-only global lookup confirms it.
	row, err := repos.DNSZones.GetByCanonicalGlobal(ctx, "unique.example.com.")
	require.NoError(t, err)
	assert.Equal(t, tenantA.ID, row.TenantID, "global lookup must surface tenant A's ownership")
}

func TestDNSZones_SetCachedFlags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())

	zone, err := repos.DNSZones.Create(withTenant(ctx, tenant.ID), database.CreateDNSZoneParams{
		CanonicalID: "cached.example.com.",
		Name:        "cached.example.com.",
	})
	require.NoError(t, err)

	require.NoError(t, repos.DNSZones.SetCachedDNSSEC(withTenant(ctx, tenant.ID), zone.ID, true))
	require.NoError(t, repos.DNSZones.SetCachedAXFR(withTenant(ctx, tenant.ID), zone.ID, true))

	got, err := repos.DNSZones.Get(withTenant(ctx, tenant.ID), zone.ID)
	require.NoError(t, err)
	assert.True(t, got.IsDnssecEnabled)
	assert.True(t, got.IsAxfrEnabled)

	// Flip back to false; both flags are independent.
	require.NoError(t, repos.DNSZones.SetCachedDNSSEC(withTenant(ctx, tenant.ID), zone.ID, false))
	got, err = repos.DNSZones.Get(withTenant(ctx, tenant.ID), zone.ID)
	require.NoError(t, err)
	assert.False(t, got.IsDnssecEnabled)
	assert.True(t, got.IsAxfrEnabled, "AXFR flag must not change when DNSSEC flips")
}

func TestDNSZones_DefaultKindIsNative(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())

	zone, err := repos.DNSZones.Create(withTenant(ctx, tenant.ID), database.CreateDNSZoneParams{
		CanonicalID: "default-kind.example.com.",
		Name:        "default-kind.example.com.",
		// Kind left empty; repo wrapper should default to "Native".
	})
	require.NoError(t, err)
	assert.Equal(t, database.DNSZoneKindNative, zone.Kind)
}

func TestDNSZones_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())

	zone, err := repos.DNSZones.Create(withTenant(ctx, tenant.ID), database.CreateDNSZoneParams{
		CanonicalID: "delete-me.example.com.",
		Name:        "delete-me.example.com.",
	})
	require.NoError(t, err)

	require.NoError(t, repos.DNSZones.Delete(withTenant(ctx, tenant.ID), zone.ID))
	_, err = repos.DNSZones.Get(withTenant(ctx, tenant.ID), zone.ID)
	require.Error(t, err, "deleted zone must not be retrievable")
}
