// runtime_test.go covers the WS-10a DoD items "memory + time limits are
// enforced" + "a plugin with no granted permissions loads but cannot call
// any host func" using hand-encoded wasm modules (see wasm_test_modules.go).
// No external Wat parser or pre-built .wasm file required.

package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

// newTestRuntime builds a runtime with a small memory cap and a short
// timeout so tests don't have to wait. Discards logs.
func newTestRuntime(t *testing.T, maxBytes int, timeout time.Duration) *Runtime {
	t.Helper()
	if maxBytes == 0 {
		maxBytes = 1 * WasmPageBytes // 1 page default
	}
	if timeout == 0 {
		timeout = 500 * time.Millisecond
	}
	rt, err := New(context.Background(), permission.NewMapEnforcer(), Config{
		MaxMemoryBytes: maxBytes,
		ExecTimeout:    timeout,
		Logger:         slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRuntime_CompileAndCall_Add(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t, 4*WasmPageBytes, time.Second)

	m := &manifest.Manifest{Name: "add", Version: "1.0.0"}
	hash, err := rt.Compile(context.Background(), addModule(), m)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, rt.HasModule(hash))

	inst, err := rt.Instantiate(context.Background(), hash, uuid.New())
	require.NoError(t, err)
	defer func() { _ = inst.Close(context.Background()) }()

	out, err := inst.Call(context.Background(), "add", 7, 35)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(42), out[0])
}

func TestRuntime_CompileIsIdempotentByHash(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t, 0, 0)

	bytes := addModule()
	h1, err := rt.Compile(context.Background(), bytes, &manifest.Manifest{Name: "x", Version: "1"})
	require.NoError(t, err)
	h2, err := rt.Compile(context.Background(), bytes, &manifest.Manifest{Name: "x", Version: "1"})
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "same bytes -> same hash")
}

func TestRuntime_RejectsModuleWithTooMuchMemory(t *testing.T) {
	t.Parallel()
	// Cap at 2 pages (128 KiB); bigMemoryModule declares max 600 pages.
	rt := newTestRuntime(t, 2*WasmPageBytes, 0)

	_, err := rt.Compile(context.Background(), bigMemoryModule(), &manifest.Manifest{Name: "big", Version: "1.0.0"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMemoryLimitExceeded)
}

func TestRuntime_PerCallTimeoutKillsLoop(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t, 4*WasmPageBytes, 100*time.Millisecond)

	hash, err := rt.Compile(context.Background(), loopModule(), &manifest.Manifest{Name: "loop", Version: "1.0.0"})
	require.NoError(t, err)

	inst, err := rt.Instantiate(context.Background(), hash, uuid.New())
	require.NoError(t, err)
	defer func() { _ = inst.Close(context.Background()) }()

	start := time.Now()
	_, err = inst.Call(context.Background(), "loop_forever")
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExecTimeout)
	// Should be well under a second; leave slack for CI.
	assert.Less(t, elapsed, 5*time.Second, "loop should be killed quickly")
}

func TestRuntime_PluginWithImportCannotInstantiate(t *testing.T) {
	t.Parallel()
	// No HostFunctions registered: any module that imports from "env" must
	// fail at instantiation. This is the WS-10a DoD item "a plugin with no
	// granted permissions loads but cannot call any host func" taken to its
	// strongest form — there are no host funcs to call at all in WS-10a.
	rt := newTestRuntime(t, 4*WasmPageBytes, time.Second)

	hash, err := rt.Compile(context.Background(), importModule(), &manifest.Manifest{Name: "importy", Version: "1.0.0"})
	require.NoError(t, err, "compile should succeed (imports are resolved at instantiation)")

	_, err = rt.Instantiate(context.Background(), hash, uuid.New())
	require.Error(t, err, "instantiation must fail because the env.log import is missing")
}

func TestRuntime_RequiresEnforcer(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), nil, Config{MaxMemoryBytes: 1024, ExecTimeout: time.Second})
	require.Error(t, err)
}

func TestRuntime_RequiresPositiveLimits(t *testing.T) {
	t.Parallel()
	e := permission.NewMapEnforcer()
	_, err := New(context.Background(), e, Config{MaxMemoryBytes: 0, ExecTimeout: time.Second})
	require.Error(t, err)
	_, err = New(context.Background(), e, Config{MaxMemoryBytes: 1024, ExecTimeout: 0})
	require.Error(t, err)
}

func TestRuntime_CallUnknownExportFails(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t, 2*WasmPageBytes, time.Second)
	hash, err := rt.Compile(context.Background(), addModule(), &manifest.Manifest{Name: "x", Version: "1"})
	require.NoError(t, err)

	inst, err := rt.Instantiate(context.Background(), hash, uuid.New())
	require.NoError(t, err)
	defer func() { _ = inst.Close(context.Background()) }()

	_, err = inst.Call(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrExecTimeout))
}

func TestRuntime_InstantiateUnknownHashFails(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t, 0, 0)
	_, err := rt.Instantiate(context.Background(), strings.Repeat("0", 64), uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownModule)
}
