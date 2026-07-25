//go:build !tinygo

package compute

var instanceCreate = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("compute.instanceCreate: requires TinyGo")
}

var instanceGet = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("compute.instanceGet: requires TinyGo")
}

var instanceList = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("compute.instanceList: requires TinyGo")
}

var instanceSetState = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("compute.instanceSetState: requires TinyGo")
}

var instanceDelete = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("compute.instanceDelete: requires TinyGo")
}
