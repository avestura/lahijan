// Package compute: placement_test.go covers the WS-26 PlacementDriver
// implementations. The tests are pure Go (no DB, no real daemon); the
// cluster driver's Incus surface is stubbed via hand-rolled fakes so
// the scheduling decisions are deterministic.
package compute

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// fakeClusterProvider is the test stub for ClusterPlacementDriver's
// Incus surface. Tests seed the members slice + the migrate op to
// return so scheduling decisions are deterministic.
type fakeClusterProvider struct {
	members   []incus.ClusterMember
	listErr   error
	migrateOp *incus.Operation
	migrateFn func(params incus.MigrateInstanceParams) (*incus.Operation, error)
	migrate_calls []incus.MigrateInstanceParams
}

func (f *fakeClusterProvider) ListClusterMembers(_ context.Context) ([]incus.ClusterMember, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.members, nil
}

func (f *fakeClusterProvider) MigrateInstance(_ context.Context, params incus.MigrateInstanceParams) (*incus.Operation, error) {
	f.migrate_calls = append(f.migrate_calls, params)
	if f.migrateFn != nil {
		return f.migrateFn(params)
	}
	if f.migrateOp != nil {
		return f.migrateOp, nil
	}
	return &incus.Operation{ID: "fake-op", Status: "Success", StatusCode: 200}, nil
}

// recordingLocker is the test stub for advisoryLocker. Tests assert
// the cluster driver actually took the lock by reading the recorded
// tenant ids.
type recordingLocker struct {
	calls   []uuid.UUID
}

func (r *recordingLocker) LockTenant(_ context.Context, tenantID uuid.UUID) (func(), error) {
	r.calls = append(r.calls, tenantID)
	return func() {}, nil
}

// TestLocalPlacementDriver_SelectTarget asserts the local driver
// always returns the empty target (Incus treats it as "any member").
func TestLocalPlacementDriver_SelectTarget(t *testing.T) {
	t.Parallel()
	d := NewLocalPlacementDriver()
	got, err := d.SelectTarget(context.Background(), PlacementParams{
		TenantID: uuid.New(),
		Name:     "x",
	})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, "local", d.DriverName())
}

// TestLocalPlacementDriver_MigrateNotSupported asserts the local
// driver refuses to migrate. The API layer maps this to 409 conflict.
func TestLocalPlacementDriver_MigrateNotSupported(t *testing.T) {
	t.Parallel()
	d := NewLocalPlacementDriver()
	_, err := d.MigrateInstance(context.Background(), MigrateParams{
		Project:      "p",
		Instance:     "i",
		TargetMember: "node-b",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMigrationNotSupported)
}

// TestClusterPlacementDriver_SelectTarget_LeastLoaded asserts the
// cluster driver picks the member with the highest free-capacity hint.
func TestClusterPlacementDriver_SelectTarget_LeastLoaded(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		members: []incus.ClusterMember{
			{ServerName: "node-a", Status: "Online",
				Config: map[string]string{"lahijan.free_cpu_mhz": "1000"}},
			{ServerName: "node-b", Status: "Online",
				Config: map[string]string{"lahijan.free_cpu_mhz": "4000"}},
			{ServerName: "node-c", Status: "Online",
				Config: map[string]string{"lahijan.free_cpu_mhz": "2000"}},
		},
	}
	locker := &recordingLocker{}
	d := NewClusterPlacementDriver(fcp, locker)

	got, err := d.SelectTarget(context.Background(), PlacementParams{
		TenantID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "node-b", got, "should pick the highest-free member")
	assert.Len(t, locker.calls, 1, "should take the per-tenant advisory lock")
	assert.Equal(t, "cluster", d.DriverName())
}

// TestClusterPlacementDriver_SelectTarget_TieBreakByName asserts that
// when two members have the same free-capacity score the driver picks
// the alphabetically-first one so the decision is deterministic.
func TestClusterPlacementDriver_SelectTarget_TieBreakByName(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		members: []incus.ClusterMember{
			{ServerName: "zeta", Status: "Online", Config: map[string]string{}},
			{ServerName: "alpha", Status: "Online", Config: map[string]string{}},
			{ServerName: "mid", Status: "Online", Config: map[string]string{}},
		},
	}
	d := NewClusterPlacementDriver(fcp, nil)

	got, err := d.SelectTarget(context.Background(), PlacementParams{
		TenantID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "alpha", got, "ties break by ServerName (alphabetical)")
}

// TestClusterPlacementDriver_SelectTarget_SkipsOffline asserts the
// driver filters out Offline + Evacuated members before scoring.
func TestClusterPlacementDriver_SelectTarget_SkipsOffline(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		members: []incus.ClusterMember{
			{ServerName: "node-a", Status: "Online", Config: map[string]string{}},
			{ServerName: "node-b", Status: "Offline", Config: map[string]string{}},
			{ServerName: "node-c", Status: "Evacuated", Config: map[string]string{}},
		},
	}
	d := NewClusterPlacementDriver(fcp, nil)

	got, err := d.SelectTarget(context.Background(), PlacementParams{
		TenantID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "node-a", got, "should skip Offline + Evacuated members")
}

// TestClusterPlacementDriver_SelectTarget_NoEligible asserts the
// driver returns ErrNoEligibleMember when every member is Offline.
func TestClusterPlacementDriver_SelectTarget_NoEligible(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		members: []incus.ClusterMember{
			{ServerName: "node-a", Status: "Offline"},
		},
	}
	d := NewClusterPlacementDriver(fcp, nil)
	_, err := d.SelectTarget(context.Background(), PlacementParams{
		TenantID: uuid.New(),
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoEligibleMember)
}

// TestClusterPlacementDriver_SelectTarget_MaintenanceMode asserts the
// driver respects the per-member scheduler.instance=maintenance
// config knob (the member is skipped until the operator clears it).
func TestClusterPlacementDriver_SelectTarget_MaintenanceMode(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		members: []incus.ClusterMember{
			{ServerName: "node-a", Status: "Online",
				Config: map[string]string{"scheduler.instance": "maintenance"}},
			{ServerName: "node-b", Status: "Online", Config: map[string]string{}},
		},
	}
	d := NewClusterPlacementDriver(fcp, nil)

	got, err := d.SelectTarget(context.Background(), PlacementParams{
		TenantID: uuid.New(),
	})
	require.NoError(t, err)
	assert.Equal(t, "node-b", got, "should skip maintenance-mode members")
}

// TestClusterPlacementDriver_Migrate_HappyPath asserts the cluster
// driver forwards the migrate call to the provider with the right
// parameters.
func TestClusterPlacementDriver_Migrate_HappyPath(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		migrateOp: &incus.Operation{ID: "op-1", Status: "Success", StatusCode: 200},
	}
	d := NewClusterPlacementDriver(fcp, nil)

	op, err := d.MigrateInstance(context.Background(), MigrateParams{
		Project:      "p",
		Instance:     "i",
		TargetMember: "node-b",
		Live:         true,
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "op-1", op.ID)
	require.Len(t, fcp.migrate_calls, 1)
	assert.Equal(t, "node-b", fcp.migrate_calls[0].TargetMember)
	assert.True(t, fcp.migrate_calls[0].Live)
}

// TestClusterPlacementDriver_Migrate_NoTarget asserts the driver
// rejects an empty target up-front.
func TestClusterPlacementDriver_Migrate_NoTarget(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{}
	d := NewClusterPlacementDriver(fcp, nil)

	_, err := d.MigrateInstance(context.Background(), MigrateParams{
		Project:  "p",
		Instance: "i",
	})
	require.Error(t, err)
}

// TestClusterPlacementDriver_Migrate_PropagatesError asserts the
// driver wraps + surfaces an Incus error.
func TestClusterPlacementDriver_Migrate_PropagatesError(t *testing.T) {
	t.Parallel()
	fcp := &fakeClusterProvider{
		migrateFn: func(_ incus.MigrateInstanceParams) (*incus.Operation, error) {
			return nil, errors.New("incus said no")
		},
	}
	d := NewClusterPlacementDriver(fcp, nil)

	_, err := d.MigrateInstance(context.Background(), MigrateParams{
		Project:      "p",
		Instance:     "i",
		TargetMember: "node-b",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "incus said no")
}

// TestFilterEligible is a small unit test for the filter so future
// scheduling knobs can land with confidence.
func TestFilterEligible(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      []incus.ClusterMember
		want    []string
	}{
		{
			name: "all online",
			in: []incus.ClusterMember{
				{ServerName: "a", Status: "Online"},
				{ServerName: "b", Status: "Online"},
			},
			want: []string{"a", "b"},
		},
		{
			name: "strips offline",
			in: []incus.ClusterMember{
				{ServerName: "a", Status: "Online"},
				{ServerName: "b", Status: "Offline"},
			},
			want: []string{"a"},
		},
		{
			name: "strips evacuated",
			in: []incus.ClusterMember{
				{ServerName: "a", Status: "Evacuated"},
				{ServerName: "b", Status: "Online"},
			},
			want: []string{"b"},
		},
		{
			name: "strips maintenance via config",
			in: []incus.ClusterMember{
				{ServerName: "a", Status: "Online", Config: map[string]string{"scheduler.instance": "maintenance"}},
				{ServerName: "b", Status: "Online"},
			},
			want: []string{"b"},
		},
		{
			name: "case insensitive status",
			in: []incus.ClusterMember{
				{ServerName: "a", Status: "ONLINE"},
			},
			want: []string{"a"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := filterEligible(tc.in)
			names := make([]string, 0, len(got))
			for _, m := range got {
				names = append(names, m.ServerName)
			}
			assert.Equal(t, tc.want, names)
		})
	}
}
