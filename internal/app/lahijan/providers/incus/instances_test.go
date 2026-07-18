// Package incus: instances_test.go covers the instance lifecycle (create,
// list, get, start/stop/restart, update, delete) against the fake daemon.
package incus_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstance_CreateAndList(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	op, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "web-1",
		Type:    "container",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status, "async op must complete in the fake")

	insts, err := p.ListInstances(ctx, project)
	require.NoError(t, err)
	require.Len(t, insts, 1)
	assert.Equal(t, "web-1", insts[0].Name)
	assert.Equal(t, project, insts[0].Project)
}

func TestInstance_Lifecycle(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "vm-1",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		action incus.InstanceAction
		status string
		code   int
	}{
		{incus.ActionStart, "Running", 103},
		{incus.ActionStop, "Stopped", 102},
		{incus.ActionRestart, "Running", 103},
		{incus.ActionFreeze, "Frozen", 110},
		{incus.ActionUnfreeze, "Running", 103},
	} {
		_, err := p.SetInstanceState(ctx, project, "vm-1", tc.action, false, 5)
		require.NoError(t, err)
		st, err := p.GetInstanceState(ctx, project, "vm-1")
		require.NoError(t, err)
		assert.Equal(t, tc.status, st.Status, "action %s", tc.action)
		assert.Equal(t, tc.code, st.StatusCode, "action %s code", tc.action)
	}
}

func TestInstance_UpdateAndDelete(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "del-1",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	_, err = p.UpdateInstance(ctx, project, "del-1", incus.InstancePut{
		Description: "updated description",
		Config:      map[string]string{"limits.cpu": "2"},
	})
	require.NoError(t, err)

	got, err := p.GetInstance(ctx, project, "del-1")
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Description)
	assert.Equal(t, "2", got.Config["limits.cpu"])

	_, err = p.DeleteInstance(ctx, project, "del-1")
	require.NoError(t, err)

	insts, err := p.ListInstances(ctx, project)
	require.NoError(t, err)
	assert.Empty(t, insts, "deleted instance must not appear in list")
}

func TestInstance_ProjectIsolation(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantA := uuid.New()
	tenantB := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantA))
	require.NoError(t, p.EnsureProject(ctx, tenantB))

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: p.ProjectName(tenantA),
		Name:    "secret-A",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	// Tenant B must not see tenant A's instance.
	got, err := p.GetInstance(ctx, p.ProjectName(tenantB), "secret-A")
	require.Error(t, err, "cross-tenant instance get must fail")
	assert.Nil(t, got)

	instsB, err := p.ListInstances(ctx, p.ProjectName(tenantB))
	require.NoError(t, err)
	assert.Empty(t, instsB, "tenant B must see zero instances in its own project")

	// And tenant A sees its own instance.
	instsA, err := p.ListInstances(ctx, p.ProjectName(tenantA))
	require.NoError(t, err)
	require.Len(t, instsA, 1)
	assert.Equal(t, "secret-A", instsA[0].Name)
}
