//go:build tinygo

package api

//go:wasm-import lahijan_api register_handler
func apiRegisterHandler(methodPtr, methodLen, pathPtr, pathLen, handlerPtr, handlerLen uint32) int32

//go:wasm-import lahijan_api unregister_handler
func apiUnregisterHandler(methodPtr, methodLen, pathPtr, pathLen uint32) int32
