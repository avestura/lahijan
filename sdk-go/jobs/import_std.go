//go:build !tinygo

package jobs

var jobsSchedule = func(namePtr, nameLen, argsPtr, argsLen uint32, runAtMs int64) int32 {
	panic("jobs.jobsSchedule: requires TinyGo")
}
