//go:build tinygo

package compute

//go:wasm-import lahijan_compute instance_create
func instanceCreate(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_compute instance_get
func instanceGet(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_compute instance_list
func instanceList(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_compute instance_set_state
func instanceSetState(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_compute instance_delete
func instanceDelete(argsPtr, argsLen, bufPtr, bufCap uint32) int32
