//go:build tinygo

package storage

//go:wasm-import lahijan_storage bucket_create
func bucketCreate(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_storage bucket_get
func bucketGet(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_storage bucket_list
func bucketList(argsPtr, argsLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_storage bucket_delete
func bucketDelete(argsPtr, argsLen, bufPtr, bufCap uint32) int32
