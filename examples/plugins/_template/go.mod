module example.com/your-plugin

go 1.23

// This go.mod is consumed by `tinygo build`; the standard `go` toolchain
// is NOT used to compile WASM plugins. TinyGo reads go.mod only to
// resolve module paths; the runtime has no external dependencies.
