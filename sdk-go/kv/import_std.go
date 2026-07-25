//go:build !tinygo

// Package kv: stub implementations for the standard Go toolchain. Under
// TinyGo the real //go:wasm-import declarations (import_tinygo.go) replace
// these. Under go build / go vet / go test these are mockable variables
// so the SDK type-checks and is unit-testable without TinyGo.
package kv

var kvGet = func(keyPtr, keyLen, bufPtr, bufCap uint32) int32 {
	panic("kv.kvGet: requires TinyGo (this stub is for go vet/go test only)")
}

var kvSet = func(keyPtr, keyLen, valPtr, valLen uint32, ttlMs int64) int32 {
	panic("kv.kvSet: requires TinyGo (this stub is for go vet/go test only)")
}

var kvDelete = func(keyPtr, keyLen uint32) int32 {
	panic("kv.kvDelete: requires TinyGo (this stub is for go vet/go test only)")
}
