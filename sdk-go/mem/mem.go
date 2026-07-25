// Package mem provides low-level linear-memory helpers shared across the
// SDK. The Len function is pure Go and safe under every toolchain.
// Ptr and Read involve unsafe.Pointer conversions and live in
// build-tagged files (mem_tinygo.go / mem_std.go).
package mem

// Len returns the length of a byte slice as uint32, the type expected by
// every host function's len parameter.
func Len(b []byte) uint32 {
	return uint32(len(b))
}
