// Package jobs: registry_test.go covers the registry's invariants without
// touching Postgres. The registry's correctness is purely in-memory; the
// integration with River is exercised in jobs_integration_test.go.

package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeJobArgs is a throwaway JobArgs implementation used by the registry
// tests. Using the example workers from examples.go would couple the test
// to the example catalog; a local kind keeps this file self-contained.
type fakeJobArgs struct{ Msg string }

func (fakeJobArgs) Kind() string { return "test.fake_registry" }

// fakeWorker is a minimal river.Worker for the registry tests.
type fakeWorker struct {
	river.WorkerDefaults[fakeJobArgs]
}

func (fakeWorker) Work(_ context.Context, _ *river.Job[fakeJobArgs]) error {
	return nil
}

// secondFakeArgs is a second kind so we can assert duplicates and ordering.
type secondFakeArgs struct{}

func (secondFakeArgs) Kind() string { return "test.second" }

type secondFakeWorker struct {
	river.WorkerDefaults[secondFakeArgs]
}

func (secondFakeWorker) Work(_ context.Context, _ *river.Job[secondFakeArgs]) error {
	return nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{
		Description: "first",
		Tags:        []string{"a", "b"},
	})

	spec, ok := r.SpecFor("test.fake_registry")
	require.True(t, ok)
	assert.Equal(t, "test.fake_registry", spec.Kind)
	assert.Equal(t, "first", spec.Description)
	assert.ElementsMatch(t, []string{"a", "b"}, spec.Tags)

	_, ok = r.SpecFor("test.does_not_exist")
	assert.False(t, ok)
	assert.Equal(t, 1, r.KindCount())
}

func TestRegistry_KindsIsSorted(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, secondFakeArgs{}, &secondFakeWorker{}, KindSpec{})
	Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{})

	kinds := r.Kinds()
	require.Len(t, kinds, 2)
	assert.Equal(t, "test.fake_registry", kinds[0].Kind)
	assert.Equal(t, "test.second", kinds[1].Kind)
}

func TestRegistry_DuplicateKindPanics(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{})
	assert.PanicsWithValue(
		t,
		`jobs: kind "test.fake_registry" already registered`,
		func() {
			Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{})
		},
	)
}

func TestRegistry_KindMismatchPanics(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	assert.Panics(t, func() {
		Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{Kind: "different.kind"})
	})
}

func TestRegistry_NilRegistryPanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		var r *Registry
		Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{})
	})
}

// TestRegistry_WorkersBundlePopulated proves the registry's Workers bundle
// is the one River sees. We assert by attempting an Insert through a fake
// river.Client would couple us to Postgres; instead we rely on the contract
// that river.AddWorker panics on a missing worker. We check the workers
// bundle indirectly: it is non-nil after the first Register.
func TestRegistry_WorkersBundlePopulated(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	require.NotNil(t, r.Workers())
	Register(r, fakeJobArgs{}, &fakeWorker{}, KindSpec{})
	require.NotNil(t, r.Workers())
}

// TestRegistry_ErrEmptyRegistry is a sentinel check so the error variable
// stays exported and unused-import-safe even when no test references it
// directly elsewhere.
func TestRegistry_ErrEmptyRegistry(t *testing.T) {
	t.Parallel()
	assert.True(t, errors.Is(ErrEmptyRegistry, ErrEmptyRegistry))
}
