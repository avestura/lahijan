//go:build tinygo

package mem

import "unsafe"

// Ptr returns the linear-memory offset of a byte slice's backing array.
// Returns 0 for empty slices (the host treats ptr=0 as "no data"). The
// returned offset is valid as long as the slice is not garbage-collected;
// callers must ensure the slice survives until the host call returns.
func Ptr(b []byte) uint32 {
	if len(b) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0])))
}

// Read returns a Go slice view over [ptr, ptr+length) in linear memory.
// The slice is valid only for the duration of the calling export; copy the
// bytes when the value must survive past the export's return.
//
// In WASM linear memory, memory never "moves" (it only grows), so the
// uintptr-to-Pointer conversion that go vet normally warns about is safe
// here. This file is behind //go:build tinygo so the standard toolchain
// never compiles it.
func Read(ptr, length uint32) []byte {
	if length == 0 {
		return []byte{}
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}
