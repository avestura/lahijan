// Package incus: storage_test.go covers storage pools (cluster-wide) and
// volumes (project-scoped).
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

func TestStoragePool_DefaultSeeded(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	list, err := p.ListStoragePools(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1, "fake seeds a default pool")
	assert.Equal(t, "default", list[0].Name)
}

func TestStoragePool_CRUD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, p.CreateStoragePool(ctx, incus.StoragePoolsPost{
		Name: "ssd", Driver: "zfs",
	}))

	got, err := p.GetStoragePool(ctx, "ssd")
	require.NoError(t, err)
	assert.Equal(t, "zfs", got.Driver)

	require.NoError(t, p.UpdateStoragePool(ctx, "ssd", incus.StoragePoolPut{
		Description: "updated",
	}))
	got, err = p.GetStoragePool(ctx, "ssd")
	require.NoError(t, err)
	assert.Equal(t, "updated", got.Description)

	require.NoError(t, p.DeleteStoragePool(ctx, "ssd"))
	list, err := p.ListStoragePools(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1, "only the seeded default pool remains")
}

func TestStorageVolume_CRUD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	require.NoError(t, p.CreateStorageVolume(ctx, "default", incus.StorageVolumesPost{
		Name:    "data-vol",
		Type:    "custom",
		Project: project,
	}))

	got, err := p.GetStorageVolume(ctx, "default", project, "custom", "data-vol")
	require.NoError(t, err)
	assert.Equal(t, "data-vol", got.Name)
	assert.Equal(t, "custom", got.Type)

	list, err := p.ListStorageVolumes(ctx, "default", project)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, p.DeleteStorageVolume(ctx, "default", project, "custom", "data-vol"))
	list, err = p.ListStorageVolumes(ctx, "default", project)
	require.NoError(t, err)
	assert.Empty(t, list)
}
