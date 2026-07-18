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
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
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

// — Config tests ————————————————————————————————————————————

func TestConfig_GetReturnsAdminValue_NeverSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "cfg")
	require.NoError(t, testutil.Repos().Plugins.GrantPermission(ctx, plugin.ID, user.ID, permission.CapConfigRead))

	repos := testutil.Repos()
	// Admin sets two keys: one public, one secret.
	_, err := repos.PluginConfig.Set(ctx, database.UpsertPluginConfigParams{
		PluginID: plugin.ID,
		Key:      "webhook_url",
		Value:    json.RawMessage(`"https://example.test/hook"`),
		IsSecret: false,
	})
	require.NoError(t, err)
	_, err = repos.PluginConfig.Set(ctx, database.UpsertPluginConfigParams{
		PluginID: plugin.ID,
		Key:      "api_token",
		Value:    json.RawMessage(`"super-secret"`),
		IsSecret: true,
	})
	require.NoError(t, err)

	rt := buildRuntime(t, Deps{Enforcer: permission.NewDBEnforcer(repos.Plugins), Repos: repos})

	// Public key reads successfully.
	out := callModule(t, rt, emitConfigGetModule("webhook_url"), plugin.ID)
	require.Len(t, out, 1)
	assert.Greater(t, int32(out[0]), int32(0), "config.get should return > 0 (bytes written)")

	// Secret key reads as "not found" (StatusNotFound = -6).
	out = callModule(t, rt, emitConfigGetModule("api_token"), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusNotFound, int32(out[0]),
		"secret config keys must surface as NotFound to the plugin")

	// Missing key also reads as "not found".
	out = callModule(t, rt, emitConfigGetModule("nonexistent"), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusNotFound, int32(out[0]))
}

// — API register_handler tests ————————————————————————————————

func TestAPI_RegisterHandler_PersistsMount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "api-plugin")
	// Grant the specific path the plugin will mount.
	require.NoError(t, testutil.Repos().Plugins.GrantPermission(ctx, plugin.ID, user.ID,
		permission.CapAPIHandlerRegister+":/webhook"))

	repos := testutil.Repos()
	rt := buildRuntime(t, Deps{Enforcer: permission.NewDBEnforcer(repos.Plugins), Repos: repos})

	out := callModule(t, rt,
		emitRegisterHandlerModule("POST", "/webhook", "on_request"), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusSuccess, int32(out[0]), "register_handler should succeed")

	// Mount landed in the DB.
	mounts, err := repos.PluginHTTPHandlers.ListForPlugin(ctx, plugin.ID)
	require.NoError(t, err)
	require.Len(t, mounts, 1)
	assert.Equal(t, "POST", mounts[0].Method)
	assert.Equal(t, "/webhook", mounts[0].Path)
	assert.Equal(t, "on_request", mounts[0].Handler)
}

func TestAPI_RegisterHandler_WithoutGrant_Denies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	// No api.handler.register grant.
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "no-api-grant")

	repos := testutil.Repos()
	rt := buildRuntime(t, Deps{Enforcer: permission.NewDBEnforcer(repos.Plugins), Repos: repos})

	out := callModule(t, rt,
		emitRegisterHandlerModule("POST", "/webhook", "on_request"), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusDenied, int32(out[0]))

	// Nothing landed in the DB.
	mounts, err := repos.PluginHTTPHandlers.ListForPlugin(ctx, plugin.ID)
	require.NoError(t, err)
	assert.Empty(t, mounts)
}

// — Jobs schedule tests ————————————————————————————————————

// fakeJobsClient is a stub jobs.Client substitute for tests that need
// to assert the schedule path enqueues a River job. The real
// *jobs.Client requires a running Postgres + River schema; we fake it
// here so the test asserts at the call boundary without depending on
// River timing.
type fakeJobsClient struct {
	insertCalls chan PluginInvokeArgs
}

func (f *fakeJobsClient) Insert(ctx context.Context, args PluginInvokeArgs) error {
	select {
	case f.insertCalls <- args:
	default: // drop on backpressure; tests use buffer=1
	}
	return nil
}

// TODO(WS-10c): the jobs.Client.Insert signature uses river.JobArgs; we
// need an adapter. For now the integration test asserts the permission
// gate + arg-validation paths through the registrar's Go-side helper
// directly.

func TestJobs_Schedule_RejectsWhenNoClient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "job-plugin")
	require.NoError(t, testutil.Repos().Plugins.GrantPermission(ctx, plugin.ID, user.ID,
		permission.CapJobSchedule))

	repos := testutil.Repos()
	rt := buildRuntime(t, Deps{Enforcer: permission.NewDBEnforcer(repos.Plugins), Repos: repos})
	// No Jobs client wired: returns StatusUnavailable.

	out := callModule(t, rt,
		emitScheduleModule("on_tick", "{}", time.Now().UnixMilli()), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusUnavailable, int32(out[0]),
		"jobs.schedule without a River client should return StatusUnavailable")
}

func TestJobs_Schedule_RejectsFarFutureRunAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "future-job")
	require.NoError(t, testutil.Repos().Plugins.GrantPermission(ctx, plugin.ID, user.ID,
		permission.CapJobSchedule))

	repos := testutil.Repos()
	rt := buildRuntime(t, Deps{
		Enforcer: permission.NewDBEnforcer(repos.Plugins),
		Repos:    repos,
		// 1-second cap on future scheduling for this test.
		MaxRunAtOffset: 1,
	})

	tenYears := time.Now().Add(10 * 365 * 24 * time.Hour).UnixMilli()
	out := callModule(t, rt, emitScheduleModule("on_tick", "{}", tenYears), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusInvalidArgument, int32(out[0]),
		"schedule beyond MaxRunAtOffset must be rejected")
}

// — Network tests ————————————————————————————————————————————

// fakeHTTPDoer is a stub HTTPDoer used to assert the network host
// function invokes the client with the right method/URL and forwards
// the response status code.
type fakeHTTPDoer struct {
	lastReq OutboundRequest
	resp    OutboundResponse
}

func (f *fakeHTTPDoer) Do(req OutboundRequest) (OutboundResponse, error) {
	f.lastReq = req
	return f.resp, nil
}

func TestNetwork_HttpRequest_GatedAndCallsClient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	user := testutil.NewUser(ctx, t, pool, true)
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "net-plugin")
	require.NoError(t, testutil.Repos().Plugins.GrantPermission(ctx, plugin.ID, user.ID,
		permission.CapNetworkOutbound))

	repos := testutil.Repos()
	doer := &fakeHTTPDoer{resp: OutboundResponse{StatusCode: 200, Body: []byte("ok")}}
	rt := buildRuntime(t, Deps{
		Enforcer:   permission.NewDBEnforcer(repos.Plugins),
		Repos:      repos,
		HTTPClient: doer,
	})

	out := callModule(t, rt,
		emitHTTPRequestModule("GET", "https://example.test/"), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, int32(200), int32(out[0]),
		"http_request should return the HTTP status code on success")
	assert.Equal(t, "GET", doer.lastReq.Method)
	assert.Equal(t, "https://example.test/", doer.lastReq.URL)
}

func TestNetwork_HttpRequest_DeniesWithoutGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	tenant := testutil.NewTenant(ctx, t, pool)
	// No network.outbound grant.
	plugin := testutil.NewPlugin(ctx, t, pool, &tenant.ID, "no-net")

	repos := testutil.Repos()
	doer := &fakeHTTPDoer{resp: OutboundResponse{StatusCode: 200}}
	rt := buildRuntime(t, Deps{
		Enforcer:   permission.NewDBEnforcer(repos.Plugins),
		Repos:      repos,
		HTTPClient: doer,
	})

	out := callModule(t, rt,
		emitHTTPRequestModule("GET", "https://example.test/"), plugin.ID)
	require.Len(t, out, 1)
	assert.Equal(t, StatusDenied, int32(out[0]))
}
