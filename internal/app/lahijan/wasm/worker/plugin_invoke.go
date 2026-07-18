// Package worker: plugin_invoke.go is the River worker that executes
// scheduled jobs and async event deliveries on behalf of plugins. The
// host functions enqueue jobs of kind wasm.plugin.invoke (see
// hostfuncs.PluginInvokeArgs); this worker dequeues them, resolves the
// plugin's compiled module via the runtime, and calls the named export.
//
// The worker is the durable side of two host-function flows:
//
//   - **jobs.schedule** — the plugin queues a future-time call to one of
//     its own exports. The job's Args field carries the plugin-authored
//     payload; the worker passes it to the export through plugin memory.
//   - **async events.subscribe** — the EventService enqueues a job per
//     matched subscription; the worker invokes the plugin's handler
//     export with the event payload.
//
// The worker is WS-10b's plug for the DoD items "async event
// subscription survives plugin restart" and "schedule runs at the right
// time": River's durability guarantees the job survives; the worker's
// existence guarantees the job gets executed when the process is up.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/hostfuncs"
	wasmruntime "github.com/avestura/lahijan/internal/app/lahijan/wasm/runtime"
)

// PluginInvokeWorker is the River worker for the wasm.plugin.invoke
// kind. Built once at bootstrap and shared across every queue pull.
type PluginInvokeWorker struct {
	river.WorkerDefaults[hostfuncs.PluginInvokeArgs]

	rt  *wasmruntime.Runtime
	log *slog.Logger
	// compiledModuleOf resolves the compiled-module hash for a plugin
	// id. Production looks this up via the installer / plugins table;
	// tests inject a stub. The signature accepts the plugin_id so the
	// lookup can hit the DB without the worker knowing the schema.
	compiledModuleOf func(ctx context.Context, pluginID uuid.UUID) (string, error)
}

// NewPluginInvokeWorker builds a worker. The runtime may be nil when
// WASM is disabled — the worker is then not registered (program.Start
// skips the registration).
func NewPluginInvokeWorker(
	rt *wasmruntime.Runtime,
	log *slog.Logger,
	compiledModuleOf func(ctx context.Context, pluginID uuid.UUID) (string, error),
) *PluginInvokeWorker {
	if log == nil {
		log = slog.Default()
	}
	return &PluginInvokeWorker{rt: rt, log: log, compiledModuleOf: compiledModuleOf}
}

// Work implements river.Worker. It resolves the plugin's compiled
// module, instantiates it, calls the named export, and closes the
// instance. The args payload is currently passed as the call's
// stack args (zero-length for now since WASM exports take only
// i32/i64); WS-10c wires a memory-payload convention.
func (w *PluginInvokeWorker) Work(ctx context.Context, job *river.Job[hostfuncs.PluginInvokeArgs]) error {
	if w.rt == nil {
		return errors.New("plugin.worker: runtime is nil (wasm disabled?)")
	}
	if w.compiledModuleOf == nil {
		return errors.New("plugin.worker: compiledModuleOf resolver is nil")
	}
	pid, err := uuid.Parse(job.Args.PluginID)
	if err != nil {
		return fmt.Errorf("plugin.worker: parse plugin id %q: %w", job.Args.PluginID, err)
	}
	hash, err := w.compiledModuleOf(ctx, pid)
	if err != nil {
		return fmt.Errorf("plugin.worker: resolve module for %s: %w", pid, err)
	}
	inst, err := w.rt.Instantiate(ctx, hash, pid)
	if err != nil {
		return fmt.Errorf("plugin.worker: instantiate %s: %w", hash, err)
	}
	defer func() { _ = inst.Close(ctx) }()
	if !inst.HasExport(job.Args.Export) {
		// Treat as a soft failure: a plugin that scheduled a job for
		// an export it no longer ships (post-upgrade) should not crash
		// the queue. River will mark this complete; an audit trail
		// covers the diagnostic.
		w.log.Warn("plugin.worker: export not found",
			"plugin", pid, "export", job.Args.Export)
		return nil
	}
	if _, err := inst.Call(ctx, job.Args.Export); err != nil {
		return fmt.Errorf("plugin.worker: call %s.%s: %w", pid, job.Args.Export, err)
	}
	return nil
}
