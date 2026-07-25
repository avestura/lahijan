// Package jobs provides idiomatic access to the Lahijan job scheduler.
// Plugins queue work that fires at a future time; the work itself is a
// WASM function exported by the plugin (looked up by name at execution
// time by the River-backed plugin_invoke worker).
package jobs

import (
	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// Schedule queues a future invocation of exportName (a WASM export in the
// same plugin) with the supplied args payload. runAtMs is the Unix-millis
// timestamp when the job should fire; it is capped at
// conf.wasm.max_run_at_offset in the future so a misbehaving plugin cannot
// queue work years out.
func Schedule(exportName string, args []byte, runAtMs int64) error {
	n := []byte(exportName)
	code := jobsSchedule(mem.Ptr(n), mem.Len(n), mem.Ptr(args), mem.Len(args), runAtMs)
	return status.FromCode(code)
}
