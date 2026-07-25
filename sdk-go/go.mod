// Package sdk-go is the Lahijan plugin SDK for Go (TinyGo).
//
// This module provides idiomatic Go wrappers around the Lahijan WASM host
// functions. Plugin authors import the sub-packages (kv, events, network,
// jobs, api, config) and call high-level functions instead of manually
// managing unsafe pointers, (ptr, len) pairs, and status-code switches.
//
// The module compiles under TinyGo to plain wasm32-unknown-unknown with
// no WASI imports (per ADR-0023). Every host-function sub-package declares
// its imports via //go:wasm-import; TinyGo dead-code-eliminates unused
// imports so a plugin only declares the permissions it actually exercises.
//
// Under the standard Go toolchain (go build / go vet / go test), the
// import declarations are replaced by mockable function variables so the
// SDK type-checks and is unit-testable without TinyGo.
module github.com/avestura/lahijan/sdk-go

go 1.23
