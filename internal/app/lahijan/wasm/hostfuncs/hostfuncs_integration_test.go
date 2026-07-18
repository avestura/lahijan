// hostfuncs_integration_test.go exercises every host function end-to-end
// against a real Postgres (via testutil.Setup) and a real wazero runtime.
// Each test builds a WASM module that imports the host function, calls
// it, and asserts the result code + side effect.
//
// Run with: go test -tags integration ./internal/app/lahijan/wasm/hostfuncs/...

//go:build integration

package hostfuncs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

func TestMain(m *testing.M) { testutil.Setup(m) }

// buildRuntime constructs a runtime with the supplied deps and
// registrar-wired host functions. Returns the runtime and a cleanup
// function.
func buildRuntime(t *testing.T, deps Deps) *runtime.Runtime {
	t.Helper()
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	regFn, err := Registrar(deps)
	require.NoError(t, err)
	enf := deps.Enforcer
	if enf == nil {
		enf = permission.NewMapEnforcer()
	}
	rt, err := runtime.New(context.Background(), enf, runtime.Config{
		MaxMemoryBytes: 4 * runtime.WasmPageBytes,
		ExecTimeout:    2 * time.Second,
		Logger:         deps.Logger,
		HostFunctions:  regFn,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}

// callModule compiles + instantiates a wasm module under the given
// plugin identity and calls its "run" export. Returns the result code.
func callModule(t *testing.T, rt *runtime.Runtime, wasmBytes []byte, pid uuid.UUID) []uint64 {
	t.Helper()
	hash, err := rt.Compile(context.Background(), wasmBytes,
		&manifest.Manifest{Name: "test-plugin", Version: "1.0.0"})
	require.NoError(t, err)
	inst, err := rt.Instantiate(context.Background(), hash, pid)
	require.NoError(t, err)
	defer func() { _ = inst.Close(context.Background()) }()
	out, err := inst.Call(context.Background(), "run")
	require.NoError(t, err)
	return out
}

// — KV tests ————————————————————————————————————————————

func TestKV_SetAndIsolateByPlugin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	pluginA := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "plugin-a")
	pluginB := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "plugin-b")
	user := testutil.NewUser(ctx, t, pool, true)

	repos := testutil.Repos()
	// Grant kv.write to plugin A only.
	require.NoError(t, repos.Plugins.GrantPermission(ctx, pluginA.ID, user.ID, permission.CapKVWrite))
	require.NoError(t, repos.Plugins.GrantPermission(ctx, pluginB.ID, user.ID, permission.CapKVWrite))
	require.NoError(t, repos.Plugins.GrantPermission(ctx, pluginA.ID, user.ID, permission.CapKVRead))
	require.NoError(t, repos.Plugins.GrantPermission(ctx, pluginB.ID, user.ID, permission.CapKVRead))

	enf := permission.NewDBEnforcer(repos.Plugins)
	rt := buildRuntime(t, Deps{Enforcer: enf, Repos: repos})

	// Plugin A writes "hello" -> "world".
	out := callModule(t, rt, emitKVSetModule("hello", "world"), pluginA.ID)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(0), out[0], "kv.set should return StatusSuccess")

	// Plugin B writes "hello" -> "B-was-here".
	out = callModule(t, rt, emitKVSetModule("hello", "B-was-here"), pluginB.ID)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(0), out[0])

	// Direct repo read: each plugin's namespace is distinct.
	aVal, err := repos.PluginKV.Get(ctx, pluginA.ID, "hello")
	require.NoError(t, err)
	assert.Equal(t, "world", string(aVal.Value), "plugin A's row is untouched")

	bVal, err := repos.PluginKV.Get(ctx, pluginB.ID, "hello")
	require.NoError(t, err)
	assert.Equal(t, "B-was-here", string(bVal.Value), "plugin B's row is in its own namespace")
}

func TestKV_WithoutGrant_DeniesAndReturnsMinus2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	// NO kv.write grant.
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "no-grant")

	repos := testutil.Repos()
	enf := permission.NewDBEnforcer(repos.Plugins)
	rt := buildRuntime(t, Deps{Enforcer: enf, Repos: repos})

	out := callModule(t, rt, emitKVSetModule("hello", "world"), plugin.ID)
	require.Len(t, out, 1)
	// Encode as signed i32 — wasm returns uint64 representation of the
	// i32 code; StatusDenied = -2.
	code := int32(out[0])
	assert.Equal(t, StatusDenied, code)
}

// — Events tests ————————————————————————————————————————————

func TestEvents_Emit_ReachesSyncListener(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "emit")
	require.NoError(t, testutil.Repos().Plugins.GrantPermission(ctx, plugin.ID, user.ID, permission.CapEventsEmit))

	repos := testutil.Repos()
	bus := eventbus.New(eventbus.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	got := make(chan string, 1)
	bus.Register("dns.record.*", func(_ context.Context, e eventbus.Event) error {
		got <- e.Topic
		return nil
	})
	rt := buildRuntime(t, Deps{Enforcer: permission.NewDBEnforcer(repos.Plugins), Repos: repos, Bus: bus})

	out := callModule(t, rt, emitEmitEventModule("dns.record.created", `{"id":"x"}`), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, uint64(0), out[0], "events.emit should return StatusSuccess")

	select {
	case topic := <-got:
		assert.Equal(t, "dns.record.created", topic)
	case <-time.After(time.Second):
		t.Fatal("listener did not receive the emitted event")
	}
}
