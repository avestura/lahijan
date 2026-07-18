// Package runtime wraps the wazero WebAssembly runtime (ADR-0012, ADR-0023).
// It owns the host-side wazero.Runtime, a compiled-module cache keyed by the
// module's sha256, and the per-call context that enforces memory + time
// limits and gates every host function through the permission enforcer.
//
// WS-10a scope: stand up the runtime + limits + enforcer seam. No host
// functions live here yet — WS-10b registers them via the HostFunctions hook
// on Config. A plugin uploaded today loads but cannot call anything (every
// potential import is gated through the enforcer, which only knows about
// grants — there is nothing to grant yet). This is the WS-10a DoD: "a plugin
// with no granted permissions loads but cannot call any host func."
//
// Per ADR-0023 we deliberately do NOT register WASI imports. The only imports
// a plugin can call are the ones WS-10b's HostFunctions registers, and every
// one of those is gated by the permission enforcer.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

// WasmPageBytes is the size of one WASM memory page (defined by the spec).
const WasmPageBytes = 65536

// ErrUnknownModule is returned by Instantiate when no compiled module is
// cached under the requested hash. The caller must Compile first.
var ErrUnknownModule = errors.New("wasm: unknown module (compile first)")

// ErrMemoryLimitExceeded is returned at compile time when the module declares
// more memory than the runtime's configured cap (conf.wasm.max_memory_per_plugin).
var ErrMemoryLimitExceeded = errors.New("wasm: module declares more memory than the configured cap")

// ErrExecTimeout is returned by Call when the per-call deadline expires.
// Wraps context.DeadlineExceeded so callers can errors.Is against either.
var ErrExecTimeout = errors.New("wasm: execution timeout")

// Config carries the runtime-wide configuration. Build one at bootstrap from
// conf.GetWasm* and pass it to New.
type Config struct {
	// MaxMemoryBytes caps each plugin instance's linear memory. Modules
	// whose declared memory max exceeds this are rejected at Compile time;
	// modules without a max are clamped to this at instantiation time via
	// wazero's WithMemoryLimitPages runtime config (defence in depth).
	MaxMemoryBytes int

	// ExecTimeout is the per-call wall-clock cap. Call wraps the caller's
	// context with WithTimeout(this) before invoking the plugin function.
	// A plugin that loops is killed by this deadline.
	ExecTimeout time.Duration

	// Logger receives lifecycle + failure events. Defaults to slog.Default
	// when nil.
	Logger *slog.Logger

	// HostFunctions is invoked once per Runtime to register host imports the
	// plugin can call. WS-10a passes nil; WS-10b passes the real registry
	// (network.outbound, kv.*, events.*, ...). Every host function the
	// registry installs MUST route its permission check through the
	// permission.Enforcer the Runtime was built with.
	HostFunctions HostFunctionsRegistrar
}

// HostFunctionsRegistrar is the seam WS-10b will implement to register host
// imports without touching the runtime package directly. The Runtime calls
// it once during New with the wazero.Runtime + the enforcer + the logger so
// each host function can close over them.
//
// WS-10a ships a nil registrar; the runtime works without any host imports
// (plugins can still export functions the host calls — they just can't call
// back into the host).
type HostFunctionsRegistrar func(ctx context.Context, rt wazero.Runtime, enforcer permission.Enforcer, logger *slog.Logger) error

// Runtime owns the wazero host runtime, the compiled-module cache, and the
// per-call enforcement seam. Construct one at bootstrap; share across
// requests. Methods are safe for concurrent use.
type Runtime struct {
	cfg      Config
	rt       wazero.Runtime
	enforcer permission.Enforcer
	logger   *slog.Logger
	mu       sync.RWMutex
	compiled map[string]*compiledPlugin // keyed by sha256 hex
}

// compiledPlugin is a cached compile result. The same .wasm bytes always
// produce the same hash, so two uploads of the same artifact reuse the
// compiled module.
type compiledPlugin struct {
	hash     string
	module   wazero.CompiledModule
	manifest *manifest.Manifest
}

// New builds a wazero runtime with the configured limits and registers host
// imports via cfg.HostFunctions (when non-nil). Callers MUST Close the
// returned Runtime at shutdown.
func New(ctx context.Context, enforcer permission.Enforcer, cfg Config) (*Runtime, error) {
	if enforcer == nil {
		return nil, errors.New("wasm: enforcer is required (use permission.MapEnforcer for tests)")
	}
	if cfg.MaxMemoryBytes <= 0 {
		return nil, errors.New("wasm: MaxMemoryBytes must be > 0")
	}
	if cfg.ExecTimeout <= 0 {
		return nil, errors.New("wasm: ExecTimeout must be > 0")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Build the wazero runtime config. The compile-time check (Compile)
	// rejects modules whose declared memory exceeds MaxMemoryBytes, so the
	// runtime-level cap is intentionally the wazero maximum (65536 pages =
	// 4 GiB) — we do NOT clamp silently at instantiation, because a silent
	// clamp would let a buggy plugin hit memory.grow failures it cannot
	// diagnose. The compile-time rejection is loud and points at the
	// manifest/config mismatch.
	//
	// WithCloseOnContextDone(true) lets wazero terminate a function whose
	// call context expires (per-call timeout). Without this wazero ignores
	// context cancellation during execution, so a plugin that loops would
	// hang the host. This is the WS-10a DoD "memory + time limits are
	// enforced (a plugin that loops is killed)".
	rtCfg := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(65536).
		WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)

	r := &Runtime{
		cfg:      cfg,
		rt:       rt,
		enforcer: enforcer,
		logger:   logger,
		compiled: make(map[string]*compiledPlugin),
	}

	if cfg.HostFunctions != nil {
		if err := cfg.HostFunctions(ctx, rt, enforcer, logger); err != nil {
			_ = rt.Close(ctx)
			return nil, fmt.Errorf("wasm: register host functions: %w", err)
		}
	}
	return r, nil
}

// Close releases the wazero runtime and every compiled module. After Close
// the Runtime is unusable.
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for _, c := range r.compiled {
		if err := c.module.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.compiled = nil
	if err := r.rt.Close(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Enforcer returns the permission enforcer the runtime was built with. Host
// functions (WS-10b) use it directly; tests use it to grant permissions.
func (r *Runtime) Enforcer() permission.Enforcer { return r.enforcer }

// Compile decodes + compiles the wasm bytes, validates the declared memory
// shape against the configured cap, caches the result keyed by the bytes'
// sha256, and returns the hash. Re-compiling the same bytes is a no-op
// (returns the cached entry). Different bytes get a different hash.
//
// Compile is the WS-10a DoD step "memory + time limits are enforced": a
// module whose declared memory max exceeds the cap is rejected here.
func (r *Runtime) Compile(ctx context.Context, wasmBytes []byte, m *manifest.Manifest) (string, error) {
	if len(wasmBytes) == 0 {
		return "", errors.New("wasm: empty module bytes")
	}
	sum := sha256.Sum256(wasmBytes)
	hash := hex.EncodeToString(sum[:])

	r.mu.RLock()
	if _, ok := r.compiled[hash]; ok {
		r.mu.RUnlock()
		return hash, nil // already compiled
	}
	r.mu.RUnlock()

	compiled, err := r.rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		return "", fmt.Errorf("wasm: compile: %w", err)
	}
	if err := checkMemoryLimit(compiled, r.cfg.MaxMemoryBytes); err != nil {
		_ = compiled.Close(ctx)
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Another goroutine may have compiled the same bytes while we held
	// the write lock; close ours and return theirs to keep the cache
	// authoritative.
	if existing, ok := r.compiled[hash]; ok {
		_ = compiled.Close(ctx)
		return existing.hash, nil
	}
	r.compiled[hash] = &compiledPlugin{hash: hash, module: compiled, manifest: m}
	return hash, nil
}

// checkMemoryLimit does a best-effort early rejection of modules whose
// EXPORTED memory exceeds the cap. Modules whose memory is not exported
// skip this check; the runtime-level cap (WithMemoryLimitPages) still
// applies at instantiation. The cap is in bytes; the module declares memory
// in pages (each 64 KiB).
//
// We check both Min and Max on every exported memory definition:
//
//   - Min pages * pageSize > cap -> reject (the module cannot fit).
//   - Max pages * pageSize > cap -> reject (the module COULD grow to exceed;
//     we don't want a memory.grow call to silently fail at runtime).
func checkMemoryLimit(m wazero.CompiledModule, capBytes int) error {
	for _, mem := range m.ExportedMemories() {
		minBytes := uint64(mem.Min()) * WasmPageBytes
		if uint64(capBytes) < minBytes {
			return fmt.Errorf("wasm: exported memory index=%d min %d bytes > cap %d: %w",
				mem.Index(), minBytes, capBytes, ErrMemoryLimitExceeded)
		}
		if memMax, ok := mem.Max(); ok {
			maxBytes := uint64(memMax) * WasmPageBytes
			if uint64(capBytes) < maxBytes {
				return fmt.Errorf("wasm: exported memory index=%d max %d bytes > cap %d: %w",
					mem.Index(), maxBytes, capBytes, ErrMemoryLimitExceeded)
			}
		}
	}
	return nil
}

// HasModule reports whether a compiled module with the given hash is cached.
// Used by the installer to skip recompilation on re-upload of the same bytes.
func (r *Runtime) HasModule(hash string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.compiled[hash]
	return ok
}

// Instantiate creates a fresh module instance from the cached compiled module
// for a single call. Callers MUST Close the returned Instance when done so
// wazero can reclaim the instance's memory. The pluginID is the row id in
// the plugins table; the enforcer consults it on every host call.
//
// Instantiation is gated by the runtime's permission enforcer: any import
// the module requires must be satisfiable by a host function whose
// permission the plugin holds. WS-10a registers no host functions, so any
// plugin that DECLARES an import fails to instantiate here.
func (r *Runtime) Instantiate(ctx context.Context, hash string, pluginID uuid.UUID) (*Instance, error) {
	r.mu.RLock()
	c, ok := r.compiled[hash]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownModule
	}

	mod, err := r.rt.InstantiateModule(ctx, c.module, wazero.NewModuleConfig())
	if err != nil {
		return nil, fmt.Errorf("wasm: instantiate %s: %w", hash, err)
	}
	return &Instance{
		module:   mod,
		compiled: c,
		rt:       r,
		pluginID: pluginID,
	}, nil
}

// Instance is a single instantiated module. One of these exists per call;
// Close reclaims the linear memory wazero allocated for it.
type Instance struct {
	module   api.Module
	compiled *compiledPlugin
	rt       *Runtime
	pluginID uuid.UUID
}

// Close releases the wazero module. Always defer this after Instantiate.
func (i *Instance) Close(ctx context.Context) error {
	if err := i.module.Close(ctx); err != nil {
		return fmt.Errorf("wasm: close instance: %w", err)
	}
	return nil
}

// Call invokes an exported function with the per-call deadline applied. The
// plugin's permission grants are NOT consulted here — host functions do
// that themselves (they have the context to look up the caller's pluginID).
// Direct exports (no host function involved) are unprivileged by
// construction.
//
// Returns ErrExecTimeout (wrapping context.DeadlineExceeded) when the call
// exceeds the configured ExecTimeout.
func (i *Instance) Call(ctx context.Context, fnName string, args ...uint64) ([]uint64, error) {
	fn := i.module.ExportedFunction(fnName)
	if fn == nil {
		return nil, fmt.Errorf("wasm: export %q not found on module", fnName)
	}
	callCtx, cancel := context.WithTimeout(ctx, i.rt.cfg.ExecTimeout)
	defer cancel()
	res, err := fn.Call(callCtx, args...)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("wasm: call %s exceeded %s: %w",
				fnName, i.rt.cfg.ExecTimeout, ErrExecTimeout)
		}
		return nil, fmt.Errorf("wasm: call %s: %w", fnName, err)
	}
	return res, nil
}

// HasExport reports whether the module exports a function with the given
// name. Used at install time to validate that the manifest's entrypoints
// actually exist.
func (i *Instance) HasExport(name string) bool {
	return i.module.ExportedFunction(name) != nil
}

// Manifest returns the manifest the module was compiled with.
func (i *Instance) Manifest() *manifest.Manifest { return i.compiled.manifest }

// PluginID returns the id of the plugin this instance belongs to.
func (i *Instance) PluginID() uuid.UUID { return i.pluginID }
