// Package incus: networks_test.go covers networks + ACLs + forwards.
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

func TestNetwork_CRUD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	require.NoError(t, p.CreateNetwork(ctx, project, incus.NetworksPost{
		Name: "lahijanbr", Type: "bridge",
		Description: "tenant bridge",
	}))

	got, err := p.GetNetwork(ctx, project, "lahijanbr")
	require.NoError(t, err)
	assert.Equal(t, "bridge", got.Type)
	assert.True(t, got.Managed)

	list, err := p.ListNetworks(ctx, project)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, p.UpdateNetwork(ctx, project, "lahijanbr", incus.NetworkPut{
		Description: "updated description",
	}))
	got, err = p.GetNetwork(ctx, project, "lahijanbr")
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Description)

	require.NoError(t, p.DeleteNetwork(ctx, project, "lahijanbr"))
	list, err = p.ListNetworks(ctx, project)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestNetwork_ACL_CRUD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	ingress := []map[string]any{{"action": "allow", "source": "10.0.0.0/24"}}
	require.NoError(t, p.CreateNetworkACL(ctx, project, "trusted", ingress, nil))

	got, err := p.GetNetworkACL(ctx, project, "trusted")
	require.NoError(t, err)
	assert.Equal(t, "trusted", got.Name)
	require.Len(t, got.Ingress, 1)

	list, err := p.ListNetworkACLs(ctx, project)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, p.DeleteNetworkACL(ctx, project, "trusted"))
	list, err = p.ListNetworkACLs(ctx, project)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestNetwork_Forward_CRUD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	require.NoError(t, p.CreateNetwork(ctx, project, incus.NetworksPost{
		Name: "pubbr", Type: "bridge",
	}))

	ports := []map[string]any{
		{"protocol": "tcp", "listen_port": "80", "target_address": "10.0.0.5", "target_port": "80"},
	}
	require.NoError(t, p.CreateNetworkForward(ctx, project, "pubbr", "203.0.113.5", ports))

	list, err := p.ListNetworkForwards(ctx, project, "pubbr")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "203.0.113.5", list[0].ListenAddress)

	require.NoError(t, p.DeleteNetworkForward(ctx, project, "pubbr", "203.0.113.5"))
	list, err = p.ListNetworkForwards(ctx, project, "pubbr")
	require.NoError(t, err)
	assert.Empty(t, list)
}
