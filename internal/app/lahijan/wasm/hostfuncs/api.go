// Package hostfuncs: api.go builds the lahijan_api host module:
// register_handler and unregister_handler. Plugins mount HTTP routes
// under /api/v1/plugins/<plugin-name>/...; the host writes the mount
// into the plugin_http_handlers table and the HTTP layer (api package)
// consults that table on every matching request.
//
// ABI (ADR-0024):
//
//	(import "lahijan_api" "register_handler"
//	  (func (param i32 i32 i32 i32 i32 i32) (result i32)))
//	(import "lahijan_api" "unregister_handler"
//	  (func (param i32 i32 i32 i32) (result i32)))
//
// register_handler(method_ptr, method_len, path_ptr, path_len,
//
//	               handler_ptr, handler_len) -> status
//	- StatusSuccess (0). Idempotent on (plugin, method, path).
//	- StatusDenied (-2)          : enforcer rejected api.handler.register:<path>.
//	- StatusInvalidArgument (-5) : method empty, path missing leading
//	                              slash, path escapes prefix.
//
// unregister_handler(method_ptr, method_len, path_ptr, path_len) -> status
//   - StatusSuccess (0).
//
// Per WS-10b "Open questions" item 3, plugins may only register routes
// under their own /api/v1/plugins/<plugin-name>/ prefix. The host
// rejects any path that would escape this prefix.
package hostfuncs

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const (
	apiModuleName = "lahijan_api"
	// MaxHTTPMethodLen caps a method name. Standard methods are <= 7
	// chars (CONNECT, DELETE, OPTIONS). 16 is generous.
	MaxHTTPMethodLen = 16
	// MaxHTTPPathLen caps a path. 1024 covers every realistic API path.
	MaxHTTPPathLen = 1024
)

func (r *registrar) buildAPIModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(apiModuleName)

	register := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		methodPtr := api.DecodeU32(stack[0])
		methodLen := api.DecodeU32(stack[1])
		pathPtr := api.DecodeU32(stack[2])
		pathLen := api.DecodeU32(stack[3])
		handlerPtr := api.DecodeU32(stack[4])
		handlerLen := api.DecodeU32(stack[5])
		stack[0] = api.EncodeI32(r.apiRegister(ctx, m, methodPtr, methodLen, pathPtr, pathLen, handlerPtr, handlerLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(register,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("register_handler")

	unregister := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		methodPtr := api.DecodeU32(stack[0])
		methodLen := api.DecodeU32(stack[1])
		pathPtr := api.DecodeU32(stack[2])
		pathLen := api.DecodeU32(stack[3])
		stack[0] = api.EncodeI32(r.apiUnregister(ctx, m, methodPtr, methodLen, pathPtr, pathLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(unregister,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("unregister_handler")

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate api module: %w", err)
	}
	return nil
}

// apiRegister is the Go-side implementation of register_handler.
func (r *registrar) apiRegister(
	ctx context.Context,
	m api.Module,
	methodPtr, methodLen, pathPtr, pathLen, handlerPtr, handlerLen uint32,
) int32 {
	methodBytes, err := readMemory(m, methodPtr, methodLen)
	if err != nil {
		return StatusInvalidMemory
	}
	pathBytes, err := readMemory(m, pathPtr, pathLen)
	if err != nil {
		return StatusInvalidMemory
	}
	if !validateMethodLen(methodLen) || !validatePathLen(pathLen) {
		return StatusInvalidArgument
	}
	method := strings.ToUpper(string(methodBytes))
	path := string(pathBytes)
	if !validatePath(path) {
		return StatusInvalidArgument
	}
	// The slug is qualified by the sub-path; a grant of
	// api.handler.register:/webhook lets the plugin mount exactly
	// "/webhook". A grant of api.handler.register:* allows any sub-path
	// under the plugin's prefix.
	slug := permission.CapAPIHandlerRegister + ":" + path
	pid, code := r.gate(ctx, apiModuleName, "register_handler", slug)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginHTTPHandlers == nil {
		return r.end(ctx, apiModuleName, "register_handler", pid, slug, StatusUnavailable)
	}
	handlerBytes, err := readMemory(m, handlerPtr, handlerLen)
	if err != nil {
		return r.end(ctx, apiModuleName, "register_handler", pid, slug, StatusInvalidMemory)
	}
	if handlerLen == 0 || handlerLen > MaxHandlerNameLen {
		return r.end(ctx, apiModuleName, "register_handler", pid, slug, StatusInvalidArgument)
	}
	if _, err := r.deps.Repos.PluginHTTPHandlers.Register(ctx, database.CreateHTTPHandlerParams{
		PluginID: pid,
		Method:   method,
		Path:     path,
		Handler:  string(handlerBytes),
	}); err != nil {
		r.log.Warn("hostfuncs: api.register_handler repo error", "plugin", pid, "error", err)
		return r.end(ctx, apiModuleName, "register_handler", pid, slug, StatusGenericFailure)
	}
	return r.end(ctx, apiModuleName, "register_handler", pid, slug, StatusSuccess)
}

// apiUnregister is the Go-side implementation of unregister_handler.
func (r *registrar) apiUnregister(
	ctx context.Context,
	m api.Module,
	methodPtr, methodLen, pathPtr, pathLen uint32,
) int32 {
	pid := pidFromContext(ctx)
	methodBytes, err := readMemory(m, methodPtr, methodLen)
	if err != nil {
		return r.end(ctx, apiModuleName, "unregister_handler", pid, permission.CapAPIHandlerRegister, StatusInvalidMemory)
	}
	pathBytes, err := readMemory(m, pathPtr, pathLen)
	if err != nil {
		return r.end(ctx, apiModuleName, "unregister_handler", pid, permission.CapAPIHandlerRegister, StatusInvalidMemory)
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginHTTPHandlers == nil {
		return r.end(ctx, apiModuleName, "unregister_handler", pid, permission.CapAPIHandlerRegister, StatusUnavailable)
	}
	method := strings.ToUpper(string(methodBytes))
	if err := r.deps.Repos.PluginHTTPHandlers.Unregister(ctx, pid, method, string(pathBytes)); err != nil {
		r.log.Warn("hostfuncs: api.unregister_handler repo error", "plugin", pid, "error", err)
		return r.end(ctx, apiModuleName, "unregister_handler", pid, permission.CapAPIHandlerRegister, StatusGenericFailure)
	}
	return r.end(ctx, apiModuleName, "unregister_handler", pid, permission.CapAPIHandlerRegister, StatusSuccess)
}

// validateMethodLen rejects empty or oversized method names.
func validateMethodLen(n uint32) bool { return n > 0 && n <= MaxHTTPMethodLen }

// validatePathLen rejects empty or oversized paths.
func validatePathLen(n uint32) bool { return n > 0 && n <= MaxHTTPPathLen }

// validatePath rejects paths that do not start with "/" or that try to
// escape the plugin prefix via ".." or "//". Per WS-10b "Open questions"
// item 3, the canonical mount path is /api/v1/plugins/<plugin-name>/<path>;
// the <path> portion must be a single, well-formed relative URL.
func validatePath(p string) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	if strings.Contains(p, "..") || strings.Contains(p, "//") {
		return false
	}
	return true
}
