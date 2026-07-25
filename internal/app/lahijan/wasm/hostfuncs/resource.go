// Package hostfuncs: resource.go provides the shared helpers used by the
// compute, dns, and storage host modules. The jsonCall pattern reduces
// each JSON-in/JSON-out host function to a small closure.
package hostfuncs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// jsonCall is the shared pattern for JSON-args host functions (WS-10f).
// Every compute/dns/storage function follows this flow:
//
//  1. Permission gate (slug).
//  2. Check the service is wired (available).
//  3. Read JSON args from the caller's linear memory.
//  4. Call the closure, which returns a Go value (or error).
//  5. Marshal the result to JSON.
//  6. Write into the caller-supplied buffer (or BufferTooSmall).
//  7. Return bytes written (>0) or a negative status code.
//
// The closure receives a context with the plugin's tenant_id injected so
// the service layer enforces tenant scoping automatically.
func (r *registrar) jsonCall(
	ctx context.Context,
	m api.Module,
	mod, fn, slug string,
	argsPtr, argsLen, bufPtr, bufCap uint32,
	available bool,
	call func(ctx context.Context, args []byte) (any, error),
) int32 {
	pid, code := r.gate(ctx, mod, fn, slug)
	if code != StatusSuccess {
		return code
	}
	if !available {
		return r.end(ctx, mod, fn, pid, slug, StatusUnavailable)
	}
	args, err := readMemory(m, argsPtr, argsLen)
	if err != nil {
		return r.end(ctx, mod, fn, pid, slug, StatusInvalidMemory)
	}
	result, err := call(ctx, args)
	if err != nil {
		if database.IsNoRows(err) {
			return r.end(ctx, mod, fn, pid, slug, StatusNotFound)
		}
		r.log.Warn("hostfuncs: "+mod+"."+fn+" error", "plugin", pid, "error", err)
		return r.end(ctx, mod, fn, pid, slug, StatusGenericFailure)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		r.log.Warn("hostfuncs: "+mod+"."+fn+" marshal error", "plugin", pid, "error", err)
		return r.end(ctx, mod, fn, pid, slug, StatusGenericFailure)
	}
	if uint32(len(resultJSON)) > bufCap {
		return r.end(ctx, mod, fn, pid, slug, StatusBufferTooSmall)
	}
	if err := writeMemory(m, bufPtr, resultJSON); err != nil {
		return r.end(ctx, mod, fn, pid, slug, StatusInvalidMemory)
	}
	return r.end(ctx, mod, fn, pid, slug, int32(len(resultJSON)))
}

// resolveTenantID looks up the calling plugin's tenant_id from the plugins
// table. Returns an error when the plugin is global (tenant_id is NULL) or
// the repo is unavailable. The returned context has the tenant_id injected
// via database.WithTenant so downstream repos enforce scoping automatically.
func (r *registrar) resolveTenantCtx(ctx context.Context, pluginID uuid.UUID) (context.Context, uuid.UUID, error) {
	if r.deps.Repos == nil || r.deps.Repos.Plugins == nil {
		return ctx, uuid.Nil, errors.New("hostfuncs: no plugins repo wired")
	}
	plugin, err := r.deps.Repos.Plugins.Get(ctx, pluginID)
	if err != nil {
		return ctx, uuid.Nil, fmt.Errorf("hostfuncs: lookup plugin %s: %w", pluginID, err)
	}
	if plugin.TenantID == nil {
		return ctx, uuid.Nil, errors.New("hostfuncs: plugin has no tenant_id (global plugin)")
	}
	tid := *plugin.TenantID
	return database.WithTenant(ctx, tid), tid, nil
}

// deleteResult is the JSON shape returned by delete operations.
type deleteResult struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}
