//go:build !tinygo

package mem

// Ptr returns 0 under the standard Go toolchain. The real implementation
// (mem_tinygo.go) returns the linear-memory offset of the slice's backing
// array. This stub exists so the SDK type-checks and is unit-testable
// without TinyGo; under standard Go, host-function wrappers panic before
// the result is used.
func Ptr(b []byte) uint32 {
	return 0
}

// Read returns nil under the standard Go toolchain. The real implementation
// (mem_tinygo.go) returns a slice view over WASM linear memory.
func Read(ptr, length uint32) []byte {
	return nil
}
