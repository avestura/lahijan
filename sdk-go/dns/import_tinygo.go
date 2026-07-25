//go:build tinygo

package dns

//go:wasm-import lahijan_dns zone_create
func zoneCreate(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_dns zone_get
func zoneGet(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_dns zone_list
func zoneList(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_dns zone_delete
func zoneDelete(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_dns record_create
func recordCreate(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_dns record_list
func recordList(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_dns record_delete
func recordDelete(argsPtr, argsLen, bufPtr, bufCap uint32) int32
