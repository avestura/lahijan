// Package compute: snapshot_integration_test.go extends the fakeIncus
// fixture with the WS-25 snapshot surface and exercises the snapshot +
// policy + backup-target service paths against a real Postgres via
// testcontainers-go. Run with:  go test -tags integration ./internal/app/lahijan/compute/...

//go:build integration

package compute_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshot-state extension to fakeIncus. The fields live on the same
// struct (no shadow type) so the existing tests keep working; the methods
// below satisfy compute.incusSnapshotOps.

// snapshotState is the per-fakeIncus snapshot map. Lazy-initialised on
// first CreateSnapshot. Keyed by "<project>:<instance>/<snapshot>".
type snapshotState struct {
	mu        sync.Mutex
	snapshots map[string]*incus.InstanceSnapshot
}

// snapshots returns the per-fake snapshot map, lazy-init.
func (f *fakeIncus) snapshots() *snapshotState {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapState == nil {
		f.snapState = &snapshotState{snapshots: make(map[string]*incus.InstanceSnapshot)}
	}
	return f.snapState
}

// CreateSnapshot implements compute.incusSnapshotOps.
func (f *fakeIncus) CreateSnapshot(_ context.Context, params incus.CreateSnapshotParams) (*incus.Operation, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	st := f.snapshots()
	st.mu.Lock()
	defer st.mu.Unlock()
	key := params.Project + ":" + params.Instance + "/" + params.Name
	if _, exists := st.snapshots[key]; exists {
		return nil, fmt.Errorf("snapshot %q already exists", params.Name)
	}
	st.snapshots[key] = &incus.InstanceSnapshot{
		Name:         params.Name,
		InstanceName: params.Instance + "/" + params.Name,
		Size:         1024,
		Stateful:     params.Stateful,
	}
	return &incus.Operation{ID: "snap-create", Status: "Success", StatusCode: 200}, nil
}

// ListInstanceSnapshots implements compute.incusSnapshotOps.
func (f *fakeIncus) ListInstanceSnapshots(_ context.Context, project, instance string) ([]incus.InstanceSnapshot, error) {
	st := f.snapshots()
	st.mu.Lock()
	defer st.mu.Unlock()
	prefix := project + ":" + instance + "/"
	out := make([]incus.InstanceSnapshot, 0)
	for k, v := range st.snapshots {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, *v)
		}
	}
	return out, nil
}

// GetSnapshot implements compute.incusSnapshotOps.
func (f *fakeIncus) GetSnapshot(_ context.Context, project, instance, snapshot string) (*incus.InstanceSnapshot, error) {
	st := f.snapshots()
	st.mu.Lock()
	defer st.mu.Unlock()
	if v, ok := st.snapshots[project+":"+instance+"/"+snapshot]; ok {
		return v, nil
	}
	return nil, incus.ErrNotFound
}

// RenameSnapshot implements compute.incusSnapshotOps.
func (f *fakeIncus) RenameSnapshot(_ context.Context, project, instance, snapshot, newName string) (*incus.Operation, error) {
	st := f.snapshots()
	st.mu.Lock()
	defer st.mu.Unlock()
	oldKey := project + ":" + instance + "/" + snapshot
	newKey := project + ":" + instance + "/" + newName
	v, ok := st.snapshots[oldKey]
	if !ok {
		return nil, incus.ErrNotFound
	}
	v.Name = newName
	v.InstanceName = instance + "/" + newName
	st.snapshots[newKey] = v
	delete(st.snapshots, oldKey)
	return &incus.Operation{ID: "snap-rename", Status: "Success", StatusCode: 200}, nil
}

// DeleteSnapshot implements compute.incusSnapshotOps.
func (f *fakeIncus) DeleteSnapshot(_ context.Context, project, instance, snapshot string) (*incus.Operation, error) {
	st := f.snapshots()
	st.mu.Lock()
	defer st.mu.Unlock()
	oldKey := project + ":" + instance + "/" + snapshot
	if _, ok := st.snapshots[oldKey]; !ok {
		return nil, incus.ErrNotFound
	}
	delete(st.snapshots, oldKey)
	return &incus.Operation{ID: "snap-delete", Status: "Success", StatusCode: 200}, nil
}

// RestoreSnapshot implements compute.incusSnapshotOps.
func (f *fakeIncus) RestoreSnapshot(_ context.Context, project, instance, snapshot string, _ bool) (*incus.Operation, error) {
	if _, err := f.GetSnapshot(context.Background(), project, instance, snapshot); err != nil {
		return nil, err
	}
	return &incus.Operation{ID: "snap-restore", Status: "Success", StatusCode: 200}, nil
}

// ExportSnapshot implements compute.incusSnapshotOps. Returns a small
// deterministic payload so the backup-worker integration test can assert
// the bytes round-trip through the LocalDriver.
func (f *fakeIncus) ExportSnapshot(_ context.Context, project, instance, snapshot string) ([]byte, error) {
	v, err := f.GetSnapshot(context.Background(), project, instance, snapshot)
	if err != nil {
		return nil, err
	}
	// Synthesise a body the size of v.Size. The bytes are zero-filled.
	body := make([]byte, v.Size)
	return body, nil
}

// snapState accessor declared as a field on fakeIncus. We add it here
// (not in the original struct) so the build-tagged file is the only
// place that references it; the original fakeIncus in
// service_integration_test.go carries the field via a forward-declared
// pointer.
//
// Note: the field is added to fakeIncus via the embedding trick below —
// Go does not allow adding fields post-declaration, so we add it to the
// original struct in service_integration_test.go instead. The reader
// will find the field there as "snapState *snapshotState".

// -----------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------

func TestSnapshot_TakeListDelete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	createCtx := f.tenantCtx()
	inst, err := f.svc.CreateInstance(createCtx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "snap-host",
		ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	snap, err := f.svc.TakeSnapshot(createCtx, f.tenantID, f.userID, compute.CreateSnapshotParams{
		InstanceID: inst.ID,
		Name:       "r1",
	}, nil)
	require.NoError(t, err)
	assert.NotEqual(t, uuidNil(), snap.ID)

	got, err := f.svc.GetSnapshot(createCtx, f.tenantID, snap.ID)
	require.NoError(t, err)
	assert.Equal(t, "r1", got.Name)

	list, err := f.svc.ListSnapshots(createCtx, f.tenantID, inst.ID, 100, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, f.svc.DeleteSnapshot(createCtx, f.tenantID, f.userID, snap.ID))

	_, err = f.svc.GetSnapshot(createCtx, f.tenantID, snap.ID)
	require.ErrorIs(t, err, compute.ErrSnapshotNotFound)
}

func TestSnapshot_TakeDuplicateNameRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	createCtx := f.tenantCtx()
	inst, err := f.svc.CreateInstance(createCtx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "snap-dup", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = f.svc.TakeSnapshot(createCtx, f.tenantID, f.userID, compute.CreateSnapshotParams{
		InstanceID: inst.ID, Name: "s",
	}, nil)
	require.NoError(t, err)

	_, err = f.svc.TakeSnapshot(createCtx, f.tenantID, f.userID, compute.CreateSnapshotParams{
		InstanceID: inst.ID, Name: "s",
	}, nil)
	require.ErrorIs(t, err, compute.ErrSnapshotNameTaken)
}

func TestSnapshotPolicy_CRUD(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	createCtx := f.tenantCtx()

	pol, err := f.svc.CreateSnapshotPolicy(createCtx, f.tenantID, f.userID, compute.CreateSnapshotPolicyParams{
		Name:        "hourly",
		Cadence:     "PT1H",
		RetainCount: 6,
		Enabled:     true,
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuidNil(), pol.ID)

	got, err := f.svc.GetSnapshotPolicy(createCtx, f.tenantID, pol.ID)
	require.NoError(t, err)
	assert.Equal(t, "PT1H", got.Cadence)

	updated, err := f.svc.UpdateSnapshotPolicy(createCtx, f.tenantID, f.userID, pol.ID, compute.UpdateSnapshotPolicyParams{
		Name:        "hourly", Cadence: "PT2H", RetainCount: 12, Enabled: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "PT2H", updated.Cadence)
	assert.Equal(t, int32(12), updated.RetainCount)

	require.NoError(t, f.svc.DeleteSnapshotPolicy(createCtx, f.tenantID, f.userID, pol.ID))
	_, err = f.svc.GetSnapshotPolicy(createCtx, f.tenantID, pol.ID)
	require.ErrorIs(t, err, compute.ErrSnapshotPolicyNotFound)
}

func TestSnapshotPolicy_RejectsBadCadence(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	createCtx := f.tenantCtx()

	_, err := f.svc.CreateSnapshotPolicy(createCtx, f.tenantID, f.userID, compute.CreateSnapshotPolicyParams{
		Name: "bad", Cadence: "garbage", Enabled: true,
	})
	require.ErrorIs(t, err, compute.ErrInvalidCadence)
}

// uuidNil returns uuid.Nil without forcing every test file to import
// the uuid package directly. Local helper.
func uuidNil() uuid.UUID { return uuid.Nil }
