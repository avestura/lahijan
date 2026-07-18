// Command plugin is the entrypoint of a Lahijan WASM plugin template.
//
// Lahijan plugins target plain wasm32-unknown-unknown (per ADR-0023). The
// only imports a plugin may call are the Lahijan host functions; every
// import is gated by the permission enforcer at runtime. TinyGo is the
// only Go compiler that emits this shape cleanly; standard `go build`
// pulls in WASI imports that Lahijan's runtime does not register.
//
// To turn this template into a real plugin:
//
//   1. Copy this directory and rename the package in main.go.
//   2. Edit lahijan.manifest.yaml to declare the permissions you need.
//   3. Declare the matching host-function imports below (use the
//      signatures in docs/architecture/plugins.md).
//   4. Implement the entrypoints your manifest declares (on_event,
//      on_request, on_tick, ...).
//   5. Run `make build` to produce plugin.wasm.
//
// All strings + byte arrays are exchanged with the host via the
// (ptr, len) convention from ADR-0024. The host function reads from /
// writes to the plugin's linear memory; the plugin allocates the buffer
// and passes the offset. Use the helper functions below to encode Go
// strings into memory.
package main

import (
	"unsafe"
)

// Host function imports. Each import names a (module, function) pair the
// Lahijan runtime registers; the second token is the function name. The
// function bodies live in the host (Go); TinyGo emits a wasm import for
// each one. The parameter + return types are i32 only (per ADR-0024) —
// strings and byte arrays travel via (ptr, len) into the caller's linear
// memory.
//
// Uncomment the imports your plugin needs. Every import MUST have a
// matching permission entry in lahijan.manifest.yaml; the runtime rejects
// instantiation when an import is not backed by a granted permission.

// — lahijan_kv ————————————————————————————————————————————

//go:wasm-import lahijan_kv get    func(keyPtr, keyLen, bufPtr, bufCap uint32) int32
//go:wasm-import lahijan_kv set    func(keyPtr, keyLen, valPtr, valLen uint32, ttlMs int64) int32
//go:wasm-import lahijan_kv delete func(keyPtr, keyLen uint32) int32

// — lahijan_events —————————————————————————————————————————

//go:wasm-import lahijan_events emit       func(topicPtr, topicLen, payloadPtr, payloadLen uint32) int32
//go:wasm-import lahijan_events subscribe  func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32
//go:wasm-import lahijan_events unsubscribe func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32

// — lahijan_network ————————————————————————————————————————

//go:wasm-import lahijan_network http_request func(methodPtr, methodLen, urlPtr, urlLen, headersPtr, headersLen, bodyPtr, bodyLen, respBufPtr, respBufCap uint32) int32

// — lahijan_config ————————————————————————————————————————

//go:wasm-import lahijan_config get func(keyPtr, keyLen, bufPtr, bufCap uint32) int32

// — lahijan_jobs ————————————————————————————————————————————

//go:wasm-import lahijan_jobs schedule func(namePtr, nameLen, argsPtr, argsLen uint32, runAtMs int64) int32

// — lahijan_api ————————————————————————————————————————————

//go:wasm-import lahijan_api register_handler   func(methodPtr, methodLen, pathPtr, pathLen, handlerPtr, handlerLen uint32) int32
//go:wasm-import lahijan_api unregister_handler func(methodPtr, methodLen, pathPtr, pathLen uint32) int32

// on_event is the entrypoint the runtime calls whenever a matching event
// fires. The runtime writes the event payload into the plugin's memory
// and calls on_event(payloadPtr, payloadLen). The payload is UTF-8 JSON;
// decode it according to the event's documented schema.
//
// The (eventPayloadPtr, eventPayloadLen) pair is valid only for the
// duration of the call. Copy the bytes into a Go-managed buffer (e.g.
// a slice backed by an array) before yielding back to the host.
//
//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	payload := readMem(payloadPtr, payloadLen)
	_ = payload // do something useful with the event payload
}

// main is required by TinyGo so the module has a start function. The
// runtime never calls it; it exists only to make `tinygo build` happy.
// Real work happens in the exported entrypoints.
func main() {}

// — memory helpers ——————————————————————————————————————————

// readMem returns a Go slice view over the bytes at [ptr, ptr+len) in the
// plugin's linear memory. The slice is valid only inside the calling
// export; the host may grow (move) memory between calls. Copy the bytes
// when the value must survive past the export's return.
func readMem(ptr, length uint32) []byte {
	if length == 0 {
		return []byte{}
	}
	// TinyGo represents unsafe.Pointer as a raw wasm i32; the conversion
	// produces a pointer into the linear memory. The slice header points
	// at the same memory; copying is the caller's responsibility.
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

// writeString copies a Go string into the linear memory at offset ptr.
// Returns the number of bytes written (the caller compares against the
// buffer capacity to detect truncation).
func writeString(ptr uint32, cap_ uint32, s string) int {
	n := uint32(len(s))
	if n > cap_ {
		n = cap_
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n)
	copy(dst, s)
	return int(n)
}
