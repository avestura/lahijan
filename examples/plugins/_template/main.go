// Command plugin is the entrypoint of a Lahijan WASM plugin template.
//
// This template uses the Lahijan Go SDK (sdk-go) — the recommended path
// for Go plugin authors. The SDK owns all unsafe pointer management,
// (ptr, len) encoding, and status-code translation; you write only
// business logic.
//
// To turn this template into a real plugin:
//
//  1. Copy this directory and rename the package in main.go.
//  2. Edit lahijan.manifest.yaml to declare the permissions you need.
//  3. Uncomment the SDK imports your plugin needs below.
//  4. Implement the entrypoints your manifest declares (on_event,
//     on_request, on_tick, ...).
//  5. Run `make build` to produce plugin.wasm.
//
// For non-Go authors (Rust, AssemblyScript, Zig), see the "Host ABI
// porting guide" appendix in docs/architecture/plugins.md for the raw
// //go:wasm-import signatures.
package main

import (
	// Uncomment the SDK packages your plugin needs:

	// "github.com/avestura/lahijan/sdk-go/config"
	// "github.com/avestura/lahijan/sdk-go/events"
	"github.com/avestura/lahijan/sdk-go/mem"
	// "github.com/avestura/lahijan/sdk-go/kv"
	// "github.com/avestura/lahijan/sdk-go/jobs"
	// "github.com/avestura/lahijan/sdk-go/network"
	// "github.com/avestura/lahijan/sdk-go/api"
)

// on_event is the entrypoint the runtime calls whenever a matching event
// fires. The runtime writes the event payload into the plugin's memory
// and calls on_event(payloadPtr, payloadLen). The payload is UTF-8 JSON;
// decode it according to the event's documented schema.
//
//export on_event
func on_event(payloadPtr, payloadLen uint32) {
	payload := mem.Read(payloadPtr, payloadLen)
	_ = payload // do something useful with the event payload

	// Example: read a config value
	// val, err := config.GetString("my_key")
	// if err != nil { return }

	// Example: write to KV
	// kv.Set("last_event", payload, 0)

	// Example: make an outbound HTTP request
	// network.PostJSON("https://example.test/hook", payload)
}

// main is required by TinyGo so the module has a start function. The
// runtime never calls it; it exists only to make `tinygo build` happy.
func main() {}
