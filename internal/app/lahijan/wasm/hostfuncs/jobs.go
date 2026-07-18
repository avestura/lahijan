// Package hostfuncs: jobs.go builds the lahijan_jobs host module:
// schedule. Plugins queue work that fires at a future time; the work
// itself is a WASM function exported by the plugin (looked up by name
// at execution time).
//
// ABI (ADR-0024):
//
//	(import "lahijan_jobs" "schedule"
//	  (func (param i32 i32 i32 i32 i64) (result i32)))
//
// schedule(name_ptr, name_len, args_ptr, args_len, run_at_unix_ms) -> status
//   - StatusSuccess (0)        : job queued.
//   - StatusDenied (-2)        : enforcer rejected job.schedule.
//   - StatusInvalidArgument (-5): name empty OR run_at too far in the
//                                 future (capped by MaxRunAtOffset).
//
// The host enqueues a River job of kind wasm.plugin.invoke with the
// plugin id + export name + args; the worker (wasm.runner package,
// lives next to jobs in production wiring) instantiates the plugin and
// calls the export. The job is durable across process restarts (River
// guarantee) and inherits River's retry + DLQ semantics.
package hostfuncs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const (
	jobsModuleName = "lahijan_jobs"
	// MaxJobNameLen caps a job export name. Same as handler names.
	MaxJobNameLen = 256
	// MaxJobArgsLen caps a job args payload. Same as event payload cap.
	MaxJobArgsLen = 64 * 1024
	// DefaultMaxRunAtOffsetSeconds caps how far in the future a plugin
	// may schedule a job. 30 days is generous for billing-cycle hooks
	// without opening a "queue work for 2099" attack surface.
	DefaultMaxRunAtOffsetSeconds int64 = 30 * 24 * 60 * 60
)

// PluginInvokeArgs is the River job args the host enqueues. The worker
// (a separate River worker that instantiates the plugin and calls the
// named export) decodes it.
type PluginInvokeArgs struct {
	PluginID string `json:"plugin_id"`
	Export   string `json:"export"`
	Args     []byte `json:"args"`
}

// Kind implements river.JobArgs. The kind is stable across versions so
// previously-queued jobs continue to work after a deploy.
func (PluginInvokeArgs) Kind() string { return "wasm.plugin.invoke" }

func (r *registrar) buildJobsModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(jobsModuleName)

	schedule := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		namePtr := api.DecodeU32(stack[0])
		nameLen := api.DecodeU32(stack[1])
		argsPtr := api.DecodeU32(stack[2])
		argsLen := api.DecodeU32(stack[3])
		runAtMS := int64(stack[4]) // uint64 is the raw i64 type
		stack[0] = api.EncodeI32(r.jobsSchedule(ctx, m, namePtr, nameLen, argsPtr, argsLen, runAtMS))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(schedule,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("schedule")

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate jobs module: %w", err)
	}
	return nil
}

// jobsSchedule is the Go-side implementation.
func (r *registrar) jobsSchedule(
	ctx context.Context,
	m api.Module,
	namePtr, nameLen, argsPtr, argsLen uint32,
	runAtMS int64,
) int32 {
	pid, code := r.gate(ctx, jobsModuleName, "schedule", permission.CapJobSchedule)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Jobs == nil {
		return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusUnavailable)
	}
	if nameLen == 0 || nameLen > MaxJobNameLen || argsLen > MaxJobArgsLen {
		return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusInvalidArgument)
	}
	// Cap how far into the future the plugin may schedule.
	maxOff := r.deps.MaxRunAtOffset
	if maxOff <= 0 {
		maxOff = DefaultMaxRunAtOffsetSeconds
	}
	now := time.Now().UnixMilli()
	if runAtMS < now {
		// Past times are allowed (River runs immediately) but the host
		// clips very old timestamps to "now" so a plugin cannot enqueue
		// a flood of historical work.
		runAtMS = now
	}
	if runAtMS-now > maxOff*1000 {
		return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusInvalidArgument)
	}
	nameBytes, err := readMemory(m, namePtr, nameLen)
	if err != nil {
		return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusInvalidMemory)
	}
	args, err := readMemory(m, argsPtr, argsLen)
	if err != nil {
		return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusInvalidMemory)
	}
	runAt := time.UnixMilli(runAtMS)
	if _, err := r.deps.Jobs.Insert(ctx, PluginInvokeArgs{
		PluginID: pid.String(),
		Export:   string(nameBytes),
		Args:     args,
	}, &river.InsertOpts{
		ScheduledAt: runAt,
	}); err != nil {
		r.log.Warn("hostfuncs: jobs.schedule river error", "plugin", pid, "error", err)
		return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusGenericFailure)
	}
	return r.end(ctx, jobsModuleName, "schedule", pid, permission.CapJobSchedule, StatusSuccess)
}

// ErrNoJobsClient is returned by tests that build a registrar without
// a River client. Production never sees this — the registrar returns
// StatusUnavailable instead.
var ErrNoJobsClient = errors.New("hostfuncs: no jobs client wired")
