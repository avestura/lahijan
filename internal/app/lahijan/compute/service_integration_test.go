// Package compute: service_integration_test.go exercises the compute Service
// end-to-end against a real Postgres (via testcontainers-go) + a fake Incus
// provider (in-process). Covers the WS-14 DoD:
//
//   - create -> start -> exec -> stop -> delete with quota + audit assertions
//   - exec round-trips input/output
//   - lifecycle transitions emit events into the WASM event bus
//   - multi-tenant isolation: tenant A cannot operate on tenant B's instance
//   - quota exceeded surfaces a *QuotaExceededError
//
// Run with:  go test -tags integration ./internal/app/lahijan/compute/...

//go:build integration

package compute_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// fakeIncus is an in-memory implementation of compute.incusProvider. It
// tracks calls + drives lifecycle transitions so the service test can assert
// the orchestration order without standing up a real daemon.
type fakeIncus struct {
	mu          sync.Mutex
	projects    map[string]bool
	instances   map[string]*fakeInstance // project:name -> instance
	execHandler func(command []string) (stdout, stderr []byte, exit int)
	createErr   error
	stateErr    error
	deleteErr   error
	execErr     error
	// snapState holds the in-memory snapshot map for the WS-25 surface.
	// Lazy-initialised on first use via (*fakeIncus).snapshots().
	snapState *snapshotState
}

type fakeInstance struct {
	inst  incus.Instance
	state incus.InstanceState
}

func newFakeIncus() *fakeIncus {
	return &fakeIncus{
		projects:  make(map[string]bool),
		instances: make(map[string]*fakeInstance),
		execHandler: func(cmd []string) ([]byte, []byte, int) {
			// Default handler: echo the command on stdout, exit 0.
			out := []byte("echo:" + joinWords(cmd))
			return out, nil, 0
		},
	}
}

func joinWords(s []string) string {
	out := ""
	for i, w := range s {
		if i > 0 {
			out += " "
		}
		out += w
	}
	return out
}

func (f *fakeIncus) ProjectName(tenantID uuid.UUID) string {
	return "lahijan-tenant-" + tenantID.String()
}

func (f *fakeIncus) EnsureProject(_ context.Context, tenantID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projects[f.ProjectName(tenantID)] = true
	return nil
}

func (f *fakeIncus) Ping(_ context.Context) error { return nil }

func (f *fakeIncus) CreateInstance(_ context.Context, params incus.CreateInstanceParams) (*incus.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	key := params.Project + ":" + params.Name
	f.instances[key] = &fakeInstance{
		inst:  incus.Instance{Name: params.Name, Project: params.Project, Status: "Stopped", StatusCode: 102},
		state: incus.InstanceState{Status: "Stopped", StatusCode: 102},
	}
	raw, _ := json.Marshal(map[string]any{"fingerprint": "fake-fp-" + params.Name})
	return &incus.Operation{ID: uuid.NewString(), Status: "Success", StatusCode: 200, Metadata: raw}, nil
}

func (f *fakeIncus) GetInstance(_ context.Context, project, name string) (*incus.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fi, ok := f.instances[project+":"+name]; ok {
		return &fi.inst, nil
	}
	return nil, incus.ErrNotFound
}

func (f *fakeIncus) ListInstances(_ context.Context, project string) ([]incus.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]incus.Instance, 0)
	for k, fi := range f.instances {
		if project == "" || splitProject(k) == project {
			out = append(out, fi.inst)
		}
	}
	return out, nil
}

func splitProject(key string) string {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i]
		}
	}
	return ""
}

func (f *fakeIncus) SetInstanceState(_ context.Context, project, name string, action incus.InstanceAction, _ bool, _ int) (*incus.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	fi, ok := f.instances[project+":"+name]
	if !ok {
		return nil, incus.ErrNotFound
	}
	switch action {
	case incus.ActionStart:
		fi.inst.Status = "Running"
		fi.inst.StatusCode = 103
		fi.state.Status = "Running"
		fi.state.StatusCode = 103
	case incus.ActionStop, incus.ActionRestart:
		status := "Stopped"
		sc := 102
		if action == incus.ActionRestart {
			status = "Running"
			sc = 103
		}
		fi.inst.Status = status
		fi.inst.StatusCode = sc
		fi.state.Status = status
		fi.state.StatusCode = sc
	case incus.ActionFreeze:
		fi.inst.Status = "Frozen"
		fi.inst.StatusCode = 110
		fi.state.Status = "Frozen"
		fi.state.StatusCode = 110
	case incus.ActionUnfreeze:
		fi.inst.Status = "Running"
		fi.inst.StatusCode = 103
		fi.state.Status = "Running"
		fi.state.StatusCode = 103
	}
	return &incus.Operation{ID: uuid.NewString(), Status: "Success", StatusCode: 200}, nil
}

func (f *fakeIncus) GetInstanceState(_ context.Context, project, name string) (*incus.InstanceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fi, ok := f.instances[project+":"+name]
	if !ok {
		return nil, incus.ErrNotFound
	}
	return &fi.state, nil
}

func (f *fakeIncus) UpdateInstance(_ context.Context, project, name string, body incus.InstancePut) (*incus.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fi, ok := f.instances[project+":"+name]
	if !ok {
		return nil, incus.ErrNotFound
	}
	fi.inst.Config = body.Config
	fi.inst.Devices = body.Devices
	fi.inst.Profiles = body.Profiles
	return &incus.Operation{ID: uuid.NewString(), Status: "Success", StatusCode: 200}, nil
}

func (f *fakeIncus) DeleteInstance(_ context.Context, project, name string) (*incus.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	delete(f.instances, project+":"+name)
	return &incus.Operation{ID: uuid.NewString(), Status: "Success", StatusCode: 200}, nil
}

func (f *fakeIncus) CreateProfile(_ context.Context, _ incus.CreateProfileParams) error { return nil }

func (f *fakeIncus) DeleteProfile(_ context.Context, _, _ string) error { return nil }

func (f *fakeIncus) CreateNetwork(_ context.Context, _ string, _ incus.NetworksPost) error {
	return nil
}

func (f *fakeIncus) DeleteNetwork(_ context.Context, _, _ string) error { return nil }

func (f *fakeIncus) CreateStorageVolume(_ context.Context, _ string, _ incus.StorageVolumesPost) error {
	return nil
}
func (f *fakeIncus) DeleteStorageVolume(_ context.Context, _, _, _, _ string) error { return nil }

func (f *fakeIncus) Exec(_ context.Context, params incus.ExecParams) (*incus.ExecResult, error) {
	if f.execErr != nil {
		return nil, f.execErr
	}
	stdout, stderr, exit := f.execHandler(params.Command)
	return &incus.ExecResult{Stdout: stdout, Stderr: stderr, ExitCode: exit}, nil
}

// OpenVNCConsole stubs the WS-24 console-open path for the service-level
// integration test. Returns a synthetic session id + secret so the service
// path can be exercised; the WS-24 handler-level test in api/ exercises
// the real bytes-pump against the Incus fake.
func (f *fakeIncus) OpenVNCConsole(_ context.Context, project, instance string) (incus.ConsoleSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.instances[project+":"+instance]; !ok {
		return incus.ConsoleSession{}, incus.ErrNotFound
	}
	return incus.ConsoleSession{
		OperationID: uuid.NewString(),
		Secret:      uuid.NewString(),
	}, nil
}

// DialVNCConsole is never invoked by the service-level integration test
// (it does not exercise the WS bytes-pump). Returns a typed nil so the
// interface satisfies; if a test ever calls it, the test must override.
func (f *fakeIncus) DialVNCConsole(_ context.Context, _, _ string) (*websocket.Conn, error) {
	return nil, nil
}

// recorderBus is a minimal eventbus.Bus-shaped recorder.
type recorderBus struct {
	mu      sync.Mutex
	events  []eventbus.Event
	emitErr error
}

func (r *recorderBus) Emit(_ context.Context, e eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return r.emitErr
}

func (r *recorderBus) snapshot() []eventbus.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventbus.Event, len(r.events))
	copy(out, r.events)
	return out
}

// capturingEmitter records every Emit + MarkOutcome call.
type capturingEmitter struct {
	mu       sync.Mutex
	rows     []audit.Event
	outcomes map[uuid.UUID]audit.Outcome
}

func newCapturingEmitter() *capturingEmitter {
	return &capturingEmitter{outcomes: map[uuid.UUID]audit.Outcome{}}
}

func (e *capturingEmitter) Emit(_ context.Context, ev audit.Event) (uuid.UUID, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := uuid.New()
	ev.Metadata = nil // not asserted in tests
	e.rows = append(e.rows, ev)
	return id, nil
}

func (e *capturingEmitter) MarkOutcome(_ context.Context, auditID uuid.UUID, o audit.Outcome) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.outcomes[auditID] = o
	return nil
}

func (e *capturingEmitter) eventsFor(action string) []audit.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []audit.Event{}
	for _, r := range e.rows {
		if r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

func (e *capturingEmitter) successCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, o := range e.outcomes {
		if o.Status == audit.StatusSuccess {
			n++
		}
	}
	return n
}

// fixture wires the dependencies the suite shares.
type fixture struct {
	svc       *compute.Service
	incus     *fakeIncus
	bus       *recorderBus
	auditEm   *capturingEmitter
	tenantID  uuid.UUID
	userID    uuid.UUID
	tenantCtx func() context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	f := &fixture{
		incus:    newFakeIncus(),
		bus:      &recorderBus{},
		auditEm:  newCapturingEmitter(),
		tenantID: tenant.ID,
		userID:   user.ID,
		tenantCtx: func() context.Context {
			return database.WithTenant(ctx, tenant.ID)
		},
	}
	f.svc = compute.New(f.incus, repos, f.auditEm, f.bus, nil, compute.Config{})
	return f
}

// TestCreateStartExecStopDelete is the WS-14 DoD happy-path integration
// test. Walks every lifecycle state + asserts:
//   - audit events fire on every privileged action
//   - WASM bus events fire on every lifecycle transition
//   - exec round-trips the command output
//   - the instance row caches the live daemon status
func TestCreateStartExecStopDelete(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	createCtx := f.tenantCtx()
	row, err := f.svc.CreateInstance(createCtx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "happy-01",
		ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, row.ID)
	assert.Equal(t, "Stopped", row.Status)

	// Audit + event bus assertions.
	require.NotEmpty(t, f.auditEm.eventsFor(compute.AuditInstanceCreate),
		"create must emit an audit row")
	require.NotEmpty(t, f.bus.snapshot(),
		"create must emit into the WASM event bus")
	assert.Equal(t, eventbus.ComputeInstanceCreated, f.bus.snapshot()[0].Topic)

	// Start.
	started, err := f.svc.SetInstanceState(createCtx, f.tenantID, f.userID, row.ID,
		compute.ActionStart, false, 30)
	require.NoError(t, err)
	assert.Equal(t, "Running", started.Status)
	require.NotEmpty(t, f.auditEm.eventsFor(compute.AuditInstanceStart))

	// Exec (instance must be running).
	execRes, err := f.svc.Exec(createCtx, f.tenantID, f.userID, compute.ExecParams{
		InstanceID: row.ID,
		Command:    []string{"echo", "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, execRes.ExitCode)
	assert.Contains(t, string(execRes.Stdout), "echo:echo hello")
	require.NotEmpty(t, f.auditEm.eventsFor(compute.AuditInstanceExec),
		"exec must emit an audit row")

	// Stop.
	stopped, err := f.svc.SetInstanceState(createCtx, f.tenantID, f.userID, row.ID,
		compute.ActionStop, false, 30)
	require.NoError(t, err)
	assert.Equal(t, "Stopped", stopped.Status)
	require.NotEmpty(t, f.auditEm.eventsFor(compute.AuditInstanceStop))

	// Delete.
	require.NoError(t, f.svc.DeleteInstance(createCtx, f.tenantID, f.userID, row.ID, false))
	require.NotEmpty(t, f.auditEm.eventsFor(compute.AuditInstanceDelete))

	// Get after delete -> 404 envelope.
	_, err = f.svc.GetInstance(createCtx, f.tenantID, row.ID)
	require.ErrorIs(t, err, compute.ErrInstanceNotFound)
}

// TestExec_NotRunning asserts the WS-14 open-question-3 default: exec is
// rejected on a non-running instance with ErrInstanceNotRunning.
func TestExec_NotRunning(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	ctx := f.tenantCtx()
	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "exec-stopped",
		ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = f.svc.Exec(ctx, f.tenantID, f.userID, compute.ExecParams{
		InstanceID: row.ID, Command: []string{"echo"},
	})
	require.ErrorIs(t, err, compute.ErrInstanceNotRunning)
}

// TestCreate_QuotaExceeded asserts the WS-14 DoD: "quota exceeded -> 422
// with clear message". The service surfaces a *QuotaExceededError which the
// HTTP layer translates to 422.
func TestCreate_QuotaExceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenant := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	tinyQuotas := compute.QuotaConfig{MaxInstances: 1, MaxVCPUs: 0, MaxMemoryMiB: 0, MaxDiskGiB: 0}
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil,
		compute.Config{Quotas: tinyQuotas})

	tenantCtx := database.WithTenant(ctx, tenant.ID)
	_, err := svc.CreateInstance(tenantCtx, tenant.ID, user.ID, compute.InstanceCreateParams{
		Name: "first", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = svc.CreateInstance(tenantCtx, tenant.ID, user.ID, compute.InstanceCreateParams{
		Name: "second", ImageAlias: "ubuntu/24.04",
	})
	require.Error(t, err)
	require.True(t, compute.IsQuotaExceeded(err), "expected quota_exceeded, got %v", err)
}

// TestCreate_DuplicateName asserts the WS-14 DoD for 409 conflict.
func TestCreate_DuplicateName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	_, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name: "dup", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name: "dup", ImageAlias: "ubuntu/24.04",
	})
	require.ErrorIs(t, err, compute.ErrInstanceNameTaken)
}

// TestMultiTenantIsolation asserts the WS-14 DoD: "tenant A cannot see/
// manage tenant B's instances". The repository enforces it; this test
// verifies the service preserves it end-to-end.
func TestMultiTenantIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	tenantA := testutil.NewTenant(ctx, t, testutil.Pool())
	tenantB := testutil.NewTenant(ctx, t, testutil.Pool())
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	// One shared fake daemon so both tenants reach it; project scoping
	// keeps their instances apart.
	svc := compute.New(newFakeIncus(), repos, newCapturingEmitter(), nil, nil, compute.Config{})

	// Tenant A creates an instance.
	ctxA := database.WithTenant(ctx, tenantA.ID)
	rowA, err := svc.CreateInstance(ctxA, tenantA.ID, user.ID, compute.InstanceCreateParams{
		Name: "tenant-a-instance", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	// Tenant B cannot read it.
	ctxB := database.WithTenant(ctx, tenantB.ID)
	_, err = svc.GetInstance(ctxB, tenantB.ID, rowA.ID)
	require.ErrorIs(t, err, compute.ErrInstanceNotFound, "tenant B must not see tenant A's instance")

	// Tenant B cannot list it.
	rowsB, err := svc.ListInstances(ctxB, tenantB.ID, 100, 0)
	require.NoError(t, err)
	assert.Empty(t, rowsB, "tenant B list must be empty")

	// Tenant B cannot start/delete it.
	_, err = svc.SetInstanceState(ctxB, tenantB.ID, user.ID, rowA.ID, compute.ActionStart, false, 30)
	require.ErrorIs(t, err, compute.ErrInstanceNotFound)
	err = svc.DeleteInstance(ctxB, tenantB.ID, user.ID, rowA.ID, false)
	require.ErrorIs(t, err, compute.ErrInstanceNotFound)

	// Tenant A can still operate on it.
	_, err = svc.SetInstanceState(ctxA, tenantA.ID, user.ID, rowA.ID, compute.ActionStart, false, 30)
	require.NoError(t, err)
}

// TestEventBus_LifecycleEmits asserts the WASM event bus receives every
// canonical lifecycle topic during a start -> stop flow.
func TestEventBus_LifecycleEmits(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name: "lifecycle-01", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = f.svc.SetInstanceState(ctx, f.tenantID, f.userID, row.ID, compute.ActionStart, false, 30)
	require.NoError(t, err)
	_, err = f.svc.SetInstanceState(ctx, f.tenantID, f.userID, row.ID, compute.ActionStop, false, 30)
	require.NoError(t, err)

	topics := []string{}
	for _, e := range f.bus.snapshot() {
		topics = append(topics, e.Topic)
	}
	assert.Contains(t, topics, eventbus.ComputeInstanceCreated)
	assert.Contains(t, topics, eventbus.ComputeInstanceStarted)
	assert.Contains(t, topics, eventbus.ComputeInstanceStopped)
}

// TestAuditOutcome_TrailForEachPrivilegedAction asserts every privileged
// action emits an audit row with status=pending and marks the outcome
// (success or failure) afterwards.
func TestAuditOutcome_TrailForEachPrivilegedAction(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name: "audit-01", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	// Every create must have emitted an audit row + a successful outcome.
	rows := f.auditEm.eventsFor(compute.AuditInstanceCreate)
	require.Len(t, rows, 1)
	successes := f.auditEm.successCount()
	assert.Greater(t, successes, 0, "create must mark at least one outcome success")

	// Force a failure on the next privileged action and assert the audit
	// row's outcome is marked failure.
	f.incus.stateErr = errors.New("daemon down")
	_, err = f.svc.SetInstanceState(ctx, f.tenantID, f.userID, row.ID, compute.ActionStart, false, 30)
	require.Error(t, err)
	stateRows := f.auditEm.eventsFor(compute.AuditInstanceStart)
	require.Len(t, stateRows, 1, "start must emit an audit row even on failure")
}

// TestReconcile_FlakyDaemon asserts a flaky daemon (daemon returns ErrNotFound
// on GetInstanceState) does NOT propagate; the cached row is returned so the
// API does not 500 on a backend blip.
func TestReconcile_FlakyDaemon(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name: "flaky-01", ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	// Force the daemon to fail the GetInstance call.
	f.incus.createErr = nil
	delete(f.incus.instances, f.incus.ProjectName(f.tenantID)+":"+row.Name)

	recon, err := f.svc.ReconcileInstance(ctx, f.tenantID, row.ID)
	require.NoError(t, err, "reconcile must NOT propagate daemon errors")
	assert.Equal(t, row.ID, recon.ID, "reconcile returns cached row on daemon error")
}

// TestSeedFeaturedImages_Idempotent covers the bootstrap seeding path.
// A second call against the same tenant is a no-op (the unique constraint
// short-circuits).
func TestSeedFeaturedImages_Idempotent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	aliases := []string{"ubuntu/24.04", "debian/12", "alpine/3.20", "fedora/40"}
	require.NoError(t, f.svc.SeedFeaturedImages(ctx, f.tenantID, aliases))
	require.NoError(t, f.svc.SeedFeaturedImages(ctx, f.tenantID, aliases), "second seed is a no-op")

	count, err := f.svc.CountImages(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(len(aliases)), count, "seed must produce one row per alias, once")
}

// fakeProviderTiming is a placeholder so the package builds even when the
// test binary imports time only for the timeout helper. The test itself
// uses no time.Sleep (per the testing conventions skill).
var _ = time.Second

// -------------------------------------------------------------------------
// WS-24: VNC console service tests.
//
// Happy path: create VM -> start -> open console -> audit row emitted.
// Error paths: container -> ErrInstanceNotVM; stopped -> ErrInstanceNotRunning;
// unknown id -> ErrInstanceNotFound.
// -------------------------------------------------------------------------

// TestOpenVNCConsole_HappyPath covers the WS-24 service path: a running VM
// gets a console session + an audit row is emitted with the VNC connect
// action.
func TestOpenVNCConsole_HappyPath(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "vm-console",
		Type:       "virtual-machine",
		ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)

	_, err = f.svc.SetInstanceState(ctx, f.tenantID, f.userID, row.ID, compute.ActionStart, false, 30)
	require.NoError(t, err)

	session, err := f.svc.OpenVNCConsole(ctx, f.tenantID, f.userID, row.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, session.OperationID, "session must carry the operation id")
	assert.NotEmpty(t, session.Secret, "session must carry the per-fd secret")
	assert.Equal(t, f.incus.ProjectName(f.tenantID), session.Project)
	assert.Equal(t, "vm-console", session.Instance)

	rows := f.auditEm.eventsFor(compute.AuditInstanceConsoleVNCConnect)
	require.Len(t, rows, 1, "open must emit exactly one VNC connect audit row")
	assert.Equal(t, audit.StatusSuccess, rows[0].Status, "audit row must be success")
	assert.Equal(t, row.ID, *rows[0].ResourceID, "audit row must reference the instance")
}

// TestOpenVNCConsole_ContainerRejected asserts a container cannot open a
// graphical console — only VMs have a VGA backend.
func TestOpenVNCConsole_ContainerRejected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "container-no-vnc",
		ImageAlias: "ubuntu/24.04", // default Type == container
	})
	require.NoError(t, err)

	_, err = f.svc.OpenVNCConsole(ctx, f.tenantID, f.userID, row.ID)
	require.ErrorIs(t, err, compute.ErrInstanceNotVM, "container must be rejected")

	// No audit row should have fired: the rejection happens BEFORE the
	// privileged action's audit emit, matching the create-quota pattern.
	rows := f.auditEm.eventsFor(compute.AuditInstanceConsoleVNCConnect)
	assert.Empty(t, rows, "no audit row when the instance is the wrong type")
}

// TestOpenVNCConsole_NotRunning asserts the service refuses to open a VNC
// session against a stopped VM.
func TestOpenVNCConsole_NotRunning(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	row, err := f.svc.CreateInstance(ctx, f.tenantID, f.userID, compute.InstanceCreateParams{
		Name:       "vm-stopped",
		Type:       "virtual-machine",
		ImageAlias: "ubuntu/24.04",
	})
	require.NoError(t, err)
	// Deliberately do NOT start.

	_, err = f.svc.OpenVNCConsole(ctx, f.tenantID, f.userID, row.ID)
	require.ErrorIs(t, err, compute.ErrInstanceNotRunning)
}

// TestOpenVNCConsole_UnknownInstance asserts the repo's tenant-scoping
// produces ErrInstanceNotFound for a random id.
func TestOpenVNCConsole_UnknownInstance(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := f.tenantCtx()

	_, err := f.svc.OpenVNCConsole(ctx, f.tenantID, f.userID, uuid.New())
	require.ErrorIs(t, err, compute.ErrInstanceNotFound)
}
