// plugin_invoke_test.go covers the River worker that executes scheduled
// jobs + async event deliveries on behalf of plugins. Uses an in-memory
// runtime + a stub module resolver so the test runs without Postgres.

package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/hostfuncs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	wasmruntime "github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

// minimal module: () -> (i32) returning 42. Same shape as
// runtime/runtime_test.go's addModule without the parameters.
// Hand-encoded WASM 1.0 bytes.
func noopReturn42Module() []byte {
	return []byte{
		// magic + version
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		// type section: 1 type () -> (i32)
		0x01, 0x05, 0x01,
		0x60, 0x00, 0x01, 0x7f,
		// function section: 1 function, type 0
		0x03, 0x02, 0x01, 0x00,
		// memory section: 1, min=max=1
		0x05, 0x04, 0x01, 0x01, 0x01, 0x01,
		// export section: 2 (memory + run)
		0x07, 0x10, 0x02,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x03, 0x72, 0x75, 0x6e, 0x00, 0x00,
		// code section: 1 function body returning 42
		0x0a, 0x06, 0x01,
		0x04,       // body size
		0x00,       // 0 locals
		0x41, 0x2a, // i32.const 42
		0x0b, // end
	}
}

func TestPluginInvokeWorker_HappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enf := permission.NewMapEnforcer()
	rt, err := wasmruntime.New(ctx, enf, wasmruntime.Config{
		MaxMemoryBytes: 1 * wasmruntime.WasmPageBytes,
		ExecTimeout:    500_000_000, // 500ms in ns
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	defer func() { _ = rt.Close(ctx) }()

	pid := uuid.New()
	hash, err := rt.Compile(ctx, noopReturn42Module(), &manifest.Manifest{Name: "noop", Version: "1"})
	require.NoError(t, err)

	resolver := func(_ context.Context, got uuid.UUID) (string, error) {
		assert.Equal(t, pid, got)
		return hash, nil
	}
	w := NewPluginInvokeWorker(rt, slog.New(slog.NewTextHandler(io.Discard, nil)), resolver)

	job := &river.Job[hostfuncs.PluginInvokeArgs]{
		Args: hostfuncs.PluginInvokeArgs{
			PluginID: pid.String(),
			Export:   "run",
			Args:     nil,
		},
	}
	require.NoError(t, w.Work(ctx, job))
}

func TestPluginInvokeWorker_UnknownExport_SucceedsSoftly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enf := permission.NewMapEnforcer()
	rt, err := wasmruntime.New(ctx, enf, wasmruntime.Config{
		MaxMemoryBytes: 1 * wasmruntime.WasmPageBytes,
		ExecTimeout:    500_000_000,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	defer func() { _ = rt.Close(ctx) }()

	pid := uuid.New()
	hash, err := rt.Compile(ctx, noopReturn42Module(), &manifest.Manifest{Name: "noop", Version: "1"})
	require.NoError(t, err)

	w := NewPluginInvokeWorker(rt, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, _ uuid.UUID) (string, error) { return hash, nil })

	job := &river.Job[hostfuncs.PluginInvokeArgs]{
		Args: hostfuncs.PluginInvokeArgs{
			PluginID: pid.String(),
			Export:   "nonexistent_export",
		},
	}
	// Soft success: the worker logs and returns nil so River does not retry.
	require.NoError(t, w.Work(ctx, job))
}

func TestPluginInvokeWorker_BadPluginID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	enf := permission.NewMapEnforcer()
	rt, err := wasmruntime.New(ctx, enf, wasmruntime.Config{
		MaxMemoryBytes: 1 * wasmruntime.WasmPageBytes,
		ExecTimeout:    500_000_000,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	defer func() { _ = rt.Close(ctx) }()

	w := NewPluginInvokeWorker(rt, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, _ uuid.UUID) (string, error) { return "", errors.New("never called") })

	job := &river.Job[hostfuncs.PluginInvokeArgs]{
		Args: hostfuncs.PluginInvokeArgs{
			PluginID: "not-a-uuid",
			Export:   "run",
		},
	}
	err = w.Work(ctx, job)
	require.Error(t, err)
}
