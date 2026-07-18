// Package hostfuncs is the single HostFunctionsRegistrar wired into
// runtime.Config.HostFunctions (WS-10b). It builds the six host modules
// (network, kv, events, jobs, api, config) and registers every host
// function in them. Every host function follows the WS-10b + ADR-0024
// contract:
//
//  1. Resolve the caller's plugin_id from the call context.
//  2. Call the permission enforcer for the function's slug; fail closed.
//  3. Open an OTel span named wasm.host.<module>.<func>.
//  4. Perform the privileged action.
//  5. Return a status code (see codes.go).
//
// Tests live next to each host-function family (network_test.go, ...).
// Integration tests that exercise the full WASM path live in
// hostfuncs_integration_test.go.
package hostfuncs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/jobs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
	wasmruntime "github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

// Deps carries everything the registrar needs to build the six host
// modules. Every field is optional at the API contract level: nil
// dependencies cause the corresponding host module to register a stub
// that always returns StatusUnavailable, so a process with no DB (e.g.
// the dev mode without Postgres wired) still instantiates plugins that
// declare imports — they just cannot call them.
type Deps struct {
	// Enforcer is the permission enforcer from WS-10a. Required: the
	// registrar panics at construction time when nil because every host
	// function depends on it.
	Enforcer permission.Enforcer

	// Logger receives permission denials + lifecycle messages.
	Logger *slog.Logger

	// Repos provides database access for the durable host functions
	// (kv, config, events subscriptions, http handlers). When nil, the
	// corresponding host functions return StatusUnavailable.
	Repos *database.Repos

	// Bus is the in-process event bus. Required for the events host
	// function; nil degrades to StatusUnavailable.
	Bus *eventbus.Bus

	// Jobs is the River client used to enqueue plugin-scheduled work.
	// Required for the jobs host function; nil degrades to
	// StatusUnavailable.
	Jobs *jobs.Client

	// HTTPClient is the outbound HTTP client used by the network host
	// function. When nil, the registrar builds a default with sane
	// timeouts.
	HTTPClient HTTPDoer

	// MaxRunAtOffset caps how far in the future a plugin may schedule
	// a job. Default 30 days; the registrar falls back to that when 0.
	MaxRunAtOffset int64

	// AllowedURLGlobs is the per-process default URL allowlist for the
	// network host function. Each entry is a glob pattern (pathosix
	// path-match). A plugin's per-grant allowlist (Phase 7) will
	// override this; for MVP the process-wide list is the only filter.
	AllowedURLGlobs []string
}

// HTTPDoer is the minimal HTTP client surface the network host function
// needs. *http.Client satisfies it; tests inject a fake.
type HTTPDoer interface {
	Do(req OutboundRequest) (OutboundResponse, error)
}

// OutboundRequest is the host-side shape of a plugin's outbound HTTP
// request. The host translates from the WASM-side (ptr, len) pairs into
// this struct before invoking the HTTPDoer.
type OutboundRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// OutboundResponse is the host-side result. The host function translates
// this back into the WASM-side representation (status code + headers +
// body) for the plugin to read.
type OutboundResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// Registrar builds and returns the runtime.HostFunctionsRegistrar that
// wires every host module. The returned function is invoked once by the
// runtime during New; it closes over the deps so each host function can
// reach them without re-resolving.
func Registrar(deps Deps) (func(context.Context, wazero.Runtime, permission.Enforcer, *slog.Logger) error, error) {
	if deps.Enforcer == nil {
		return nil, errors.New("hostfuncs: Enforcer is required")
	}
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	r := &registrar{
		deps:     deps,
		enforcer: deps.Enforcer,
		log:      log,
	}
	return r.register, nil
}

// registrar is the state the host functions close over. Methods on it
// build the six host modules.
type registrar struct {
	deps     Deps
	enforcer permission.Enforcer
	log      *slog.Logger
}

// register is the HostFunctionsRegistrar signature. It builds each host
// module in turn; a failure building one is fatal because a plugin that
// imports from a missing module cannot instantiate.
func (r *registrar) register(
	ctx context.Context,
	rt wazero.Runtime,
	_ permission.Enforcer,
	_ *slog.Logger,
) error {
	if err := r.buildKVModule(ctx, rt); err != nil {
		return fmt.Errorf("hostfuncs: kv module: %w", err)
	}
	if err := r.buildConfigModule(ctx, rt); err != nil {
		return fmt.Errorf("hostfuncs: config module: %w", err)
	}
	if err := r.buildEventsModule(ctx, rt); err != nil {
		return fmt.Errorf("hostfuncs: events module: %w", err)
	}
	if err := r.buildJobsModule(ctx, rt); err != nil {
		return fmt.Errorf("hostfuncs: jobs module: %w", err)
	}
	if err := r.buildAPIModule(ctx, rt); err != nil {
		return fmt.Errorf("hostfuncs: api module: %w", err)
	}
	if err := r.buildNetworkModule(ctx, rt); err != nil {
		return fmt.Errorf("hostfuncs: network module: %w", err)
	}
	return nil
}

// resolvePluginID extracts the calling plugin's id from the call context.
// Host functions fail closed when the id is missing — that means the
// runtime did not inject it (programming bug).
//
// The actual key + lookup live in the runtime package to avoid an
// import cycle (runtime -> hostfuncs would close the loop because
// hostfuncs imports runtime for the wazero Registrar signature).
func resolvePluginID(ctx context.Context) (uuid.UUID, error) {
	return wasmruntime.PluginIDFromContext(ctx)
}

// errNoPluginInContext is the host-side sentinel that wraps the
// runtime's "no plugin id" error. Host functions translate it to
// StatusDenied (fail closed).
var errNoPluginInContext = wasmruntime.ErrNoPluginInContext

// WithPluginID is the test-side helper. Production callers go through
// runtime.Instance.Call which injects the id itself.
func WithPluginID(ctx context.Context, id uuid.UUID) context.Context {
	return wasmruntime.WithPluginID(ctx, id)
}

// readMemory reads len bytes from the calling module's linear memory at
// offset ptr. Returns a nil error slice when the read falls outside the
// memory range; callers translate that to StatusInvalidMemory.
func readMemory(m api.Module, ptr, length uint32) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}
	mem := m.Memory()
	if mem == nil {
		return nil, errors.New("hostfuncs: module has no memory")
	}
	out, ok := mem.Read(ptr, length)
	if !ok {
		return nil, fmt.Errorf("hostfuncs: memory read oob ptr=%d len=%d", ptr, length)
	}
	// Copy so the caller can hold the slice past the host call (memory
	// may move on memory.grow).
	cp := make([]byte, length)
	copy(cp, out)
	return cp, nil
}

// writeMemory writes b into the calling module's linear memory at ptr.
// Returns an error when the write falls outside the memory range. The
// caller (plugin) is responsible for sizing the buffer; the host never
// grows memory on the plugin's behalf.
func writeMemory(m api.Module, ptr uint32, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	mem := m.Memory()
	if mem == nil {
		return errors.New("hostfuncs: module has no memory")
	}
	ok := mem.Write(ptr, b)
	if !ok {
		return fmt.Errorf("hostfuncs: memory write oob ptr=%d len=%d", ptr, len(b))
	}
	return nil
}
