// Package incus_test: cluster_test.go covers the WS-26 cluster API surface
// against the in-memory fake daemon. Each test asserts the REST call
// round-trips the expected fields (target, migration body, evacuate
// action) and that the driver surfaces the response cleanly.
package incus_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus/fake"
)

// TestListClusterMembers_DefaultSeed asserts the fake seeds a single
// "fake-host" member so non-cluster-aware tests still see one entry.
func TestListClusterMembers_DefaultSeed(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)

	members, err := p.ListClusterMembers(context.Background())
	require.NoError(t, err)
	require.Len(t, members, 1, "default seed has one member")
	assert.Equal(t, "fake-host", members[0].ServerName)
	assert.Equal(t, "Online", members[0].Status)
	assert.True(t, members[0].Database, "default seed is the database leader")
}

// TestListClusterMembers_AddClusterMember asserts the fake's helper can
// grow the cluster so multi-node scheduling tests have a realistic view.
func TestListClusterMembers_AddClusterMember(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-2")
	srv.AddClusterMember("node-3")
	// Idempotent on name; a duplicate add is a no-op.
	srv.AddClusterMember("node-2")
	p := connectProvider(t, srv)

	members, err := p.ListClusterMembers(context.Background())
	require.NoError(t, err)
	assert.Len(t, members, 3, "fake-host + node-2 + node-3")
}

// TestGetClusterMember asserts the per-member GET surfaces the full
// metadata (roles, architecture, failure_domain).
func TestGetClusterMember(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-a")
	p := connectProvider(t, srv)

	m, err := p.GetClusterMember(context.Background(), "node-a")
	require.NoError(t, err)
	assert.Equal(t, "node-a", m.ServerName)
	assert.Equal(t, "Online", m.Status)
	assert.Equal(t, "x86_64", m.Architecture)
}

// TestGetClusterMember_NotFound asserts the driver surfaces the daemon's
// 404 as a wrapped error so the API layer can map it.
func TestGetClusterMember_NotFound(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)

	_, err := p.GetClusterMember(context.Background(), "ghost")
	require.Error(t, err)
}

// TestSetClusterMemberState_Evacuate asserts the evacuate action flips
// the member's status to "Evacuated" in the fake's view.
func TestSetClusterMemberState_Evacuate(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-a")
	p := connectProvider(t, srv)

	op, err := p.SetClusterMemberState(context.Background(), "node-a",
		incus.ClusterMemberActionEvacuate, "")
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status)

	members := srv.ClusterMembers()
	for _, m := range members {
		if m.ServerName == "node-a" {
			assert.Equal(t, "Evacuated", m.Status, "evacuate flips the member's status")
			return
		}
	}
	t.Fatal("node-a not found in cluster members list")
}

// TestSetClusterMemberState_Restore asserts the restore action brings
// the member back to "Online".
func TestSetClusterMemberState_Restore(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-a")
	srv.SetClusterMemberStatus("node-a", "Evacuated")
	p := connectProvider(t, srv)

	_, err := p.RestoreClusterMember(context.Background(), "node-a")
	require.NoError(t, err)

	for _, m := range srv.ClusterMembers() {
		if m.ServerName == "node-a" {
			assert.Equal(t, "Online", m.Status)
			return
		}
	}
	t.Fatal("node-a not found")
}

// TestCreateInstance_WithTarget asserts the WS-26 target plumbing lands
// on the create call. The fake records the target on the instance's
// Location field so a follow-up GetInstance round-trips it.
func TestCreateInstance_WithTarget(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-b")
	p := connectProvider(t, srv)
	ctx := context.Background()

	require.NoError(t, p.Ping(ctx))
	op, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: "default",
		Name:    "with-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
		Target:  "node-b",
	})
	require.NoError(t, err)
	require.NotNil(t, op)

	inst, err := p.GetInstance(ctx, "default", "with-target")
	require.NoError(t, err)
	assert.Equal(t, "node-b", inst.Location, "target should pin the instance's Location")
}

// TestCreateInstance_UnknownTargetRejected asserts an unknown target
// surfaces the daemon's 400 so the placement driver can route on the
// error.
func TestCreateInstance_UnknownTargetRejected(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)
	ctx := context.Background()
	require.NoError(t, p.Ping(ctx))

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: "default",
		Name:    "bad-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
		Target:  "ghost",
	})
	require.Error(t, err)
}

// TestMigrateInstance_HappyPath asserts the migrate call moves the
// instance's Location to the new target.
func TestMigrateInstance_HappyPath(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-b")
	srv.AddClusterMember("node-c")
	p := connectProvider(t, srv)
	ctx := context.Background()
	require.NoError(t, p.Ping(ctx))

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: "default",
		Name:    "will-migrate",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
		Target:  "node-b",
	})
	require.NoError(t, err)

	op, err := p.MigrateInstance(ctx, incus.MigrateInstanceParams{
		Project:      "default",
		Instance:     "will-migrate",
		TargetMember: "node-c",
		Live:         true,
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status)

	inst, err := p.GetInstance(ctx, "default", "will-migrate")
	require.NoError(t, err)
	assert.Equal(t, "node-c", inst.Location, "migrate should flip the Location")
}

// TestMigrateInstance_NoTarget asserts the driver rejects a migrate call
// without a target up-front (saves a round-trip + surfaces a clearer
// error than the daemon's terse "target required").
func TestMigrateInstance_NoTarget(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)

	_, err := p.MigrateInstance(context.Background(), incus.MigrateInstanceParams{
		Project:  "default",
		Instance: "x",
	})
	require.Error(t, err)
}

// TestJoinClusterMember asserts the join call adds a new member to the
// fake's cluster view. The fake does not implement the real handshake;
// the call just appends a row.
func TestJoinClusterMember(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := connectProvider(t, srv)

	op, err := p.JoinClusterMember(context.Background(), incus.ClusterMembersPost{
		ServerName:     "node-joined",
		ClusterAddress: "https://fake-host:8443",
		JoinToken:      "tok",
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status)

	members := srv.ClusterMembers()
	var found bool
	for _, m := range members {
		if m.ServerName == "node-joined" {
			found = true
		}
	}
	assert.True(t, found, "joined member must appear in the cluster list")
}

// TestJoinClusterMember_Duplicate asserts the daemon rejects a duplicate
// server_name with an error.
func TestJoinClusterMember_Duplicate(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	srv.AddClusterMember("node-d")
	p := connectProvider(t, srv)

	_, err := p.JoinClusterMember(context.Background(), incus.ClusterMembersPost{
		ServerName: "node-d",
	})
	require.Error(t, err)
}
