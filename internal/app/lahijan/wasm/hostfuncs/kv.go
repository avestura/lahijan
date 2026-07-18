// Package hostfuncs: kv.go builds the lahijan_kv host module: kv_get /
// kv_set / kv_delete. Every call enforces the kv.read:<ns> /
// kv.write:<ns> permission (per WS-10a's slug catalog).
//
// ABI (ADR-0024):
//
//	(import "lahijan_kv" "get" (func (param i32 i32 i32 i32) (result i32)))
//	(import "lahijan_kv" "set" (func (param i32 i32 i32 i32 i64) (result i32)))
//	(import "lahijan_kv" "delete" (func (param i32 i32) (result i32)))
//
// kv_get(key_ptr, key_len, buf_ptr, buf_cap) -> bytes_written
//   - 0       : key exists with empty value (zero-length value).
//   - > 0     : bytes written into buf.
//   - StatusNotFound (-6)        : key missing.
//   - StatusBufferTooSmall (-7)  : buf_cap < value size; caller retries.
//   - StatusDenied (-2)          : enforcer rejected kv.read:<ns>.
//
// kv_set(key_ptr, key_len, val_ptr, val_len, ttl_ms) -> status
//   - StatusSuccess (0) on success.
//   - ttl_ms = 0 means "no TTL"; otherwise the value expires ttl_ms
//     milliseconds from now.
//   - StatusDenied (-2)          : enforcer rejected kv.write:<ns>.
//
// kv_delete(key_ptr, key_len) -> status
//   - StatusSuccess (0) on success (also when the key was already gone).
//   - StatusDenied (-2)          : enforcer rejected kv.write:<ns>.
//
// The namespace qualifier in the permission slug is the plugin's own
// name (per WS-10a's slug catalog doc), so kv reads/writes are scoped
// to the calling plugin's own namespace. Cross-plugin reads are
// structurally impossible: the host function injects the calling
// plugin's id into every query, and the (plugin_id, key) pair is the
// unique key on plugin_kv.
package hostfuncs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

// kvModuleName is the WASM import module name.
const kvModuleName = "lahijan_kv"

// MaxKVKeyLen caps a single KV key length. 1 KiB covers every realistic
// use; the host rejects longer keys with StatusInvalidArgument so a
// buggy plugin cannot push a 1 GiB key into Postgres.
const MaxKVKeyLen = 1024

// MaxKVValueLen caps a single KV value length. 256 KiB covers binary
// blobs; larger values should reference an object in S3 (WS-16).
const MaxKVValueLen = 256 * 1024

// buildKVModule instantiates the lahijan_kv host module on the runtime.
// When deps.Repos is nil the module still instantiates but every call
// returns StatusUnavailable; this keeps plugin instantiation working
// in dev runs without a DB.
func (r *registrar) buildKVModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(kvModuleName)

	get := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		keyPtr := api.DecodeU32(stack[0])
		keyLen := api.DecodeU32(stack[1])
		bufPtr := api.DecodeU32(stack[2])
		bufCap := api.DecodeU32(stack[3])
		stack[0] = api.EncodeI32(r.kvGet(ctx, m, keyPtr, keyLen, bufPtr, bufCap))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(get,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("get")

	set := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		keyPtr := api.DecodeU32(stack[0])
		keyLen := api.DecodeU32(stack[1])
		valPtr := api.DecodeU32(stack[2])
		valLen := api.DecodeU32(stack[3])
		ttlMS := int64(stack[4]) // uint64 is the raw i64 type
		stack[0] = api.EncodeI32(r.kvSet(ctx, m, keyPtr, keyLen, valPtr, valLen, ttlMS))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(set,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI64},
			[]api.ValueType{api.ValueTypeI32}).
		Export("set")

	del := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		keyPtr := api.DecodeU32(stack[0])
		keyLen := api.DecodeU32(stack[1])
		stack[0] = api.EncodeI32(r.kvDelete(ctx, m, keyPtr, keyLen))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(del,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("delete")

	// Instantiate the host module once after every function is added.
	// Calling Instantiate per-function would re-create the module under
	// the same name and fail with "module has already been instantiated".
	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate kv module: %w", err)
	}
	return nil
}

// kvGet is the Go-side implementation; tests can call it directly.
func (r *registrar) kvGet(
	ctx context.Context,
	m api.Module,
	keyPtr, keyLen, bufPtr, bufCap uint32,
) int32 {
	pid, code := r.gate(ctx, kvModuleName, "get", permission.CapKVRead)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginKV == nil {
		return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusUnavailable)
	}
	if keyLen > MaxKVKeyLen {
		return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusInvalidArgument)
	}
	key, err := readMemory(m, keyPtr, keyLen)
	if err != nil {
		return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusInvalidMemory)
	}
	row, err := r.deps.Repos.PluginKV.Get(ctx, pid, string(key))
	if err != nil {
		if database.IsNoRows(err) {
			return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusNotFound)
		}
		r.log.Warn("hostfuncs: kv.get repo error", "plugin", pid, "error", err)
		return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusGenericFailure)
	}
	if uint32(len(row.Value)) > bufCap {
		return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusBufferTooSmall)
	}
	if err := writeMemory(m, bufPtr, row.Value); err != nil {
		return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, StatusInvalidMemory)
	}
	return r.end(ctx, kvModuleName, "get", pid, permission.CapKVRead, int32(len(row.Value)))
}

// kvSet is the Go-side implementation of kv_set.
func (r *registrar) kvSet(
	ctx context.Context,
	m api.Module,
	keyPtr, keyLen, valPtr, valLen uint32,
	ttlMS int64,
) int32 {
	pid, code := r.gate(ctx, kvModuleName, "set", permission.CapKVWrite)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginKV == nil {
		return r.end(ctx, kvModuleName, "set", pid, permission.CapKVWrite, StatusUnavailable)
	}
	if keyLen > MaxKVKeyLen || valLen > MaxKVValueLen {
		return r.end(ctx, kvModuleName, "set", pid, permission.CapKVWrite, StatusInvalidArgument)
	}
	key, err := readMemory(m, keyPtr, keyLen)
	if err != nil {
		return r.end(ctx, kvModuleName, "set", pid, permission.CapKVWrite, StatusInvalidMemory)
	}
	val, err := readMemory(m, valPtr, valLen)
	if err != nil {
		return r.end(ctx, kvModuleName, "set", pid, permission.CapKVWrite, StatusInvalidMemory)
	}
	var expiresAt *time.Time
	if ttlMS > 0 {
		t := time.Now().Add(time.Duration(ttlMS) * time.Millisecond)
		expiresAt = &t
	}
	if _, err := r.deps.Repos.PluginKV.Set(ctx, database.UpsertPluginKVParams{
		PluginID:  pid,
		Key:       string(key),
		Value:     val,
		ExpiresAt: expiresAt,
	}); err != nil {
		r.log.Warn("hostfuncs: kv.set repo error", "plugin", pid, "error", err)
		return r.end(ctx, kvModuleName, "set", pid, permission.CapKVWrite, StatusGenericFailure)
	}
	return r.end(ctx, kvModuleName, "set", pid, permission.CapKVWrite, StatusSuccess)
}

// kvDelete is the Go-side implementation of kv_delete.
func (r *registrar) kvDelete(
	ctx context.Context,
	m api.Module,
	keyPtr, keyLen uint32,
) int32 {
	pid, code := r.gate(ctx, kvModuleName, "delete", permission.CapKVWrite)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginKV == nil {
		return r.end(ctx, kvModuleName, "delete", pid, permission.CapKVWrite, StatusUnavailable)
	}
	if keyLen > MaxKVKeyLen {
		return r.end(ctx, kvModuleName, "delete", pid, permission.CapKVWrite, StatusInvalidArgument)
	}
	key, err := readMemory(m, keyPtr, keyLen)
	if err != nil {
		return r.end(ctx, kvModuleName, "delete", pid, permission.CapKVWrite, StatusInvalidMemory)
	}
	if err := r.deps.Repos.PluginKV.Delete(ctx, pid, string(key)); err != nil {
		r.log.Warn("hostfuncs: kv.delete repo error", "plugin", pid, "error", err)
		return r.end(ctx, kvModuleName, "delete", pid, permission.CapKVWrite, StatusGenericFailure)
	}
	return r.end(ctx, kvModuleName, "delete", pid, permission.CapKVWrite, StatusSuccess)
}

// gate is the shared permission gate every host function calls first.
// It opens a span, resolves the plugin id from the call context, runs
// the enforcer, and returns either (pid, StatusSuccess) when allowed
// or (pid-or-zero, denialCode) when not.
//
// The slug is the scope.action prefix from the permission catalog. The
// enforcer's Allowed algorithm handles wildcards on the qualifier, so
// requesting the bare prefix matches any grant for that prefix
// including "prefix:*".
func (r *registrar) gate(ctx context.Context, mod, fn, slug string) (uuid.UUID, int32) {
	pid, err := resolvePluginID(ctx)
	if err != nil {
		// No plugin id => programming bug; fail closed with Denied so
		// the plugin gets a stable error shape.
		return uuid.Nil, r.end(ctx, mod, fn, uuid.Nil, slug, StatusDenied)
	}
	ok, err := r.enforcer.Allowed(ctx, pid, slug)
	if err != nil {
		r.log.Warn("hostfuncs: enforcer error",
			"plugin", pid, "slug", slug, "error", err)
		return pid, r.end(ctx, mod, fn, pid, slug, StatusGenericFailure)
	}
	if !ok {
		return pid, r.end(ctx, mod, fn, pid, slug, StatusDenied)
	}
	return pid, StatusSuccess
}

// end opens the result span, records the code, and returns the code.
// Host functions call this exactly once on every return path so the
// OTel span count matches the call count (WS-10b DoD item "every host
// function call is observable in OTel traces").
func (r *registrar) end(ctx context.Context, mod, fn string, pid uuid.UUID, slug string, code int32) int32 {
	_, span := startSpan(ctx, mod, fn, pid, slug)
	defer span.End()
	recordResult(span, code)
	if code < 0 && code != StatusNotFound {
		r.log.Debug("hostfuncs: call returned non-success",
			"module", mod, "fn", fn, "plugin", pid, "code", Explain(code))
	}
	return code
}
