// Package incus: profiles_test.go covers the profile CRUD + EnsureProfile
// (idempotent upsert).
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

func TestProfile_CRUD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	// Use a non-default profile name: EnsureProject now seeds a "default"
	// profile automatically (root disk + eth0), so creating "default"
	// here would collide. "shape" is what the compute module uses for
	// tenant-defined instance shapes.
	require.NoError(t, p.CreateProfile(ctx, incus.CreateProfileParams{
		Project:     project,
		Name:        "shape",
		Description: "tenant shape profile",
		Config:      map[string]string{"limits.cpu": "2"},
	}))

	got, err := p.GetProfile(ctx, project, "shape")
	require.NoError(t, err)
	assert.Equal(t, "2", got.Config["limits.cpu"])

	list, err := p.ListProfiles(ctx, project)
	require.NoError(t, err)
	// 2 profiles: the auto-seeded "default" + the "shape" we just created.
	require.Len(t, list, 2)

	require.NoError(t, p.UpdateProfile(ctx, project, "shape", incus.ProfilePut{
		Config: map[string]string{"limits.cpu": "4"},
	}))
	got, err = p.GetProfile(ctx, project, "shape")
	require.NoError(t, err)
	assert.Equal(t, "4", got.Config["limits.cpu"])

	require.NoError(t, p.DeleteProfile(ctx, project, "shape"))
	list, err = p.ListProfiles(ctx, project)
	require.NoError(t, err)
	// Only the auto-seeded "default" remains.
	require.Len(t, list, 1)
	assert.Equal(t, "default", list[0].Name)
}

func TestProfile_Ensure_Idempotent(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	params := incus.CreateProfileParams{
		Project: project, Name: "shape",
		Config: map[string]string{"limits.cpu": "2"},
	}
	require.NoError(t, p.EnsureProfile(ctx, params))
	// Second call must update, not fail.
	params.Config["limits.cpu"] = "4"
	require.NoError(t, p.EnsureProfile(ctx, params))

	got, err := p.GetProfile(ctx, project, "shape")
	require.NoError(t, err)
	assert.Equal(t, "4", got.Config["limits.cpu"], "EnsureProfile must apply the new config")
}

func TestProfile_DeviceAttach(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	require.NoError(t, p.CreateProfile(ctx, incus.CreateProfileParams{
		Project: project, Name: "shape",
	}))
	require.NoError(t, p.AttachProfileDevice(ctx, project, "shape", "root", "disk",
		map[string]string{"path": "/"}))

	got, err := p.GetProfile(ctx, project, "shape")
	require.NoError(t, err)
	require.Contains(t, got.Devices, "root")
	assert.Equal(t, "disk", got.Devices["root"]["type"])
}
