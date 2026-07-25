//go:build tinygo

package jobs

//go:wasm-import lahijan_jobs schedule
func jobsSchedule(namePtr, nameLen, argsPtr, argsLen uint32, runAtMs int64) int32
