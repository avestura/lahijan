// Package api provides idiomatic access to the Lahijan HTTP handler
// registration. Plugins mount routes under
// /api/v1/plugins/<plugin-name>/<path>; the host calls the registered WASM
// export on each matching request.
package api

import (
	"github.com/avestura/lahijan/sdk-go/mem"
	"github.com/avestura/lahijan/sdk-go/status"
)

// RegisterHandler mounts an HTTP route under the plugin's prefix. The
// handler is a WASM export (by name) the host calls on each matching
// request. Paths must start with "/" and must not contain ".." or "//".
// Idempotent on (plugin, method, path).
func RegisterHandler(method, path, handlerName string) error {
	m := []byte(method)
	p := []byte(path)
	h := []byte(handlerName)
	code := apiRegisterHandler(
		mem.Ptr(m), mem.Len(m),
		mem.Ptr(p), mem.Len(p),
		mem.Ptr(h), mem.Len(h),
	)
	return status.FromCode(code)
}

// UnregisterHandler removes a previously mounted HTTP route.
func UnregisterHandler(method, path string) error {
	m := []byte(method)
	p := []byte(path)
	code := apiUnregisterHandler(mem.Ptr(m), mem.Len(m), mem.Ptr(p), mem.Len(p))
	return status.FromCode(code)
}
