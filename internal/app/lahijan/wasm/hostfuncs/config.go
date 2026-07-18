// Package hostfuncs: config.go builds the lahijan_config host module:
// config_get. Plugins read their admin-set configuration through this
// host function. Per the WS-10b DoD "config_get returns admin-set
// values, never secrets", the host function uses the repository's
// PublicGet / PublicList path which filters is_secret = true rows.
//
// ABI (ADR-0024):
//
//	(import "lahijan_config" "get"
//	  (func (param i32 i32 i32 i32) (result i32)))
//
// config_get(key_ptr, key_len, buf_ptr, buf_cap) -> bytes_written
//   - > 0 : bytes written into buf (JSON-encoded value).
//   - 0   : key exists but value is the empty string "{}" / "null".
//   - StatusNotFound (-6)        : key missing OR marked secret. The
//                                 plugin cannot distinguish the two;
//                                 this is deliberate.
//   - StatusBufferTooSmall (-7)  : buf_cap < value size; retry larger.
//   - StatusDenied (-2)          : enforcer rejected config.read:<plugin>.
package hostfuncs

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const (
	configModuleName = "lahijan_config"
	// MaxConfigKeyLen caps a single config key. Mirrors typical
	// JSON-schema property name limits.
	MaxConfigKeyLen = 256
	// MaxConfigValueLen caps a single config value. 64 KiB is the same
	// cap as the event bus payload (WS-10b "Open questions" item 2).
	MaxConfigValueLen = 64 * 1024
)

func (r *registrar) buildConfigModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(configModuleName)

	get := api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
		keyPtr := api.DecodeU32(stack[0])
		keyLen := api.DecodeU32(stack[1])
		bufPtr := api.DecodeU32(stack[2])
		bufCap := api.DecodeU32(stack[3])
		stack[0] = api.EncodeI32(r.configGet(ctx, m, keyPtr, keyLen, bufPtr, bufCap))
	})
	mod.NewFunctionBuilder().
		WithGoModuleFunction(get,
			[]api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32},
			[]api.ValueType{api.ValueTypeI32}).
		Export("get")

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate config module: %w", err)
	}
	return nil
}

// configGet is the Go-side implementation; tests can call it directly.
func (r *registrar) configGet(
	ctx context.Context,
	m api.Module,
	keyPtr, keyLen, bufPtr, bufCap uint32,
) int32 {
	pid, code := r.gate(ctx, configModuleName, "get", permission.CapConfigRead)
	if code != StatusSuccess {
		return code
	}
	if r.deps.Repos == nil || r.deps.Repos.PluginConfig == nil {
		return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusUnavailable)
	}
	if keyLen > MaxConfigKeyLen {
		return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusInvalidArgument)
	}
	key, err := readMemory(m, keyPtr, keyLen)
	if err != nil {
		return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusInvalidMemory)
	}
	row, err := r.deps.Repos.PluginConfig.PublicGet(ctx, pid, string(key))
	if err != nil {
		if database.IsNoRows(err) {
			// PublicGet returns ErrNoSecretVisible (which wraps
			// pgx.ErrNoRows) for both "missing" and "secret" — the
			// plugin cannot tell them apart.
			return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusNotFound)
		}
		r.log.Warn("hostfuncs: config.get repo error", "plugin", pid, "error", err)
		return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusGenericFailure)
	}
	val := []byte(row.Value)
	if uint32(len(val)) > bufCap {
		return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusBufferTooSmall)
	}
	if err := writeMemory(m, bufPtr, val); err != nil {
		return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, StatusInvalidMemory)
	}
	return r.end(ctx, configModuleName, "get", pid, permission.CapConfigRead, int32(len(val)))
}
