// Package incus: snapshots_test.go covers the snapshot surface
// (create / list / get / rename / delete / restore / export) against the
// fake daemon.
package incus_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bootstrapSnapshotInstance creates a project + instance the rest of the
// suite can take snapshots of. Returns the project + instance name.
func bootstrapSnapshotInstance(t *testing.T, p *incus.Provider) (string, string) {
	t.Helper()
	ctx := context.Background()
	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)
	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "snap-source",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)
	return project, "snap-source"
}

func TestSnapshot_CreateListGet(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	project, instance := bootstrapSnapshotInstance(t, p)

	op, err := p.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project:  project,
		Instance: instance,
		Name:     "hourly-1",
	})
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status)

	snaps, err := p.ListInstanceSnapshots(ctx, project, instance)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "hourly-1", snaps[0].Name)

	got, err := p.GetSnapshot(ctx, project, instance, "hourly-1")
	require.NoError(t, err)
	assert.Equal(t, "hourly-1", got.Name)
	assert.False(t, got.Stateful, "stateful default is false")
}

func TestSnapshot_CreateConflict(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	project, instance := bootstrapSnapshotInstance(t, p)

	_, err := p.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project: project, Instance: instance, Name: "dup",
	})
	require.NoError(t, err)

	_, err = p.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project: project, Instance: instance, Name: "dup",
	})
	require.Error(t, err, "duplicate snapshot name must error")
}

func TestSnapshot_RenameDelete(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	project, instance := bootstrapSnapshotInstance(t, p)

	_, err := p.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project: project, Instance: instance, Name: "old",
	})
	require.NoError(t, err)

	_, err = p.RenameSnapshot(ctx, project, instance, "old", "new")
	require.NoError(t, err)

	snaps, err := p.ListInstanceSnapshots(ctx, project, instance)
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "new", snaps[0].Name)

	_, err = p.DeleteSnapshot(ctx, project, instance, "new")
	require.NoError(t, err)

	snaps, err = p.ListInstanceSnapshots(ctx, project, instance)
	require.NoError(t, err)
	assert.Empty(t, snaps, "snapshot list must be empty after delete")
}

func TestSnapshot_Restore(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	project, instance := bootstrapSnapshotInstance(t, p)

	_, err := p.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project: project, Instance: instance, Name: "r1",
	})
	require.NoError(t, err)

	op, err := p.RestoreSnapshot(ctx, project, instance, "r1", false)
	require.NoError(t, err)
	require.NotNil(t, op)
	assert.Equal(t, "Success", op.Status)
}

func TestSnapshot_Export(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	project, instance := bootstrapSnapshotInstance(t, p)

	_, err := p.CreateSnapshot(ctx, incus.CreateSnapshotParams{
		Project: project, Instance: instance, Name: "exp",
	})
	require.NoError(t, err)

	// The fake seeds Size=1024 on snapshot create; the export must deliver
	// exactly that many bytes.
	body, err := p.ExportSnapshot(ctx, project, instance, "exp")
	require.NoError(t, err)
	require.Len(t, body, 1024)

	// Confirm the bytes are all zero (the fake's contract).
	zeros := bytes.Repeat([]byte{0}, 1024)
	assert.True(t, bytes.Equal(body, zeros), "export body must be zero-filled")
}
