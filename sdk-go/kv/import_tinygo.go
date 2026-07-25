//go:build tinygo

package kv

// Raw WASM host-function imports. Under TinyGo these are body-less function
// declarations resolved at link time from the lahijan_kv host module. The
// Go function names (kvGet, kvSet, kvDelete) are internal; the public API
// in kv.go wraps them with idiomatic signatures + error handling.

//go:wasm-import lahijan_kv get
func kvGet(keyPtr, keyLen, bufPtr, bufCap uint32) int32

//go:wasm-import lahijan_kv set
func kvSet(keyPtr, keyLen, valPtr, valLen uint32, ttlMs int64) int32

//go:wasm-import lahijan_kv delete
func kvDelete(keyPtr, keyLen uint32) int32
