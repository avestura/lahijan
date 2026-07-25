// Package hostfuncs: compute.go builds the lahijan_compute host module
// (WS-10f). Plugins that manage infrastructure (autoscalers, deployment
// automation, etc.) call these functions to create, read, control, and
// delete compute instances. Every call is gated by a compute.instance.*
// permission and delegates to the compute service, which enforces tenant
// scoping, billing, and audit.
//
// ABI (ADR-0024, JSON-in/JSON-out variant):
//
//	(import "lahijan_compute" "instance_create"
//	  (func (param i32 i32 i32 i32) (result i32)))
//
// Each function takes (args_ptr, args_len, buf_ptr, buf_cap) and returns
// bytes-written (>0, JSON in buf) or a negative StatusXxx code.
package hostfuncs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const computeModuleName = "lahijan_compute"

// JSON arg types for the compute host functions. These use snake_case json
// tags (matching the project's API conventions) and are converted to the
// service's own param types inside the closure.

type computeCreateArgs struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	ImageAlias string            `json:"image_alias"`
	Profiles   []string          `json:"profiles"`
	Config     map[string]string `json:"config"`
}

type computeIDArgs struct {
	ID string `json:"id"`
}

type computeListArgs struct {
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
}

type computeSetStateArgs struct {
	ID           string `json:"id"`
	Action       string `json:"action"`
	Force        bool   `json:"force"`
	TimeoutSecs  int    `json:"timeout_secs"`
}

func (r *registrar) buildComputeModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(computeModuleName)
	params4 := []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}
	resultI32 := []api.ValueType{api.ValueTypeI32}

	mk := func(name string, fn func(ctx context.Context, m api.Module, s []uint64)) {
		mod.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(fn), params4, resultI32).
			Export(name)
	}

	mk("instance_create", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.computeInstanceCreate(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("instance_get", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.computeInstanceGet(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("instance_list", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.computeInstanceList(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("instance_set_state", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.computeInstanceSetState(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("instance_delete", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.computeInstanceDelete(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate compute module: %w", err)
	}
	return nil
}

func (r *registrar) computeInstanceCreate(
	ctx context.Context, m api.Module,
	argsPtr, argsLen, bufPtr, bufCap uint32,
) int32 {
	return r.jsonCall(ctx, m, computeModuleName, "instance_create",
		permission.CapComputeInstanceCreate,
		argsPtr, argsLen, bufPtr, bufCap,
		r.deps.Compute != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a computeCreateArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Compute.CreateInstance(tctx, tid, uuid.Nil, compute.InstanceCreateParams{
				Name:       a.Name,
				Type:       a.Type,
				ImageAlias: a.ImageAlias,
				Profiles:   a.Profiles,
				Config:     a.Config,
			})
		})
}

func (r *registrar) computeInstanceGet(
	ctx context.Context, m api.Module,
	argsPtr, argsLen, bufPtr, bufCap uint32,
) int32 {
	return r.jsonCall(ctx, m, computeModuleName, "instance_get",
		permission.CapComputeInstanceRead,
		argsPtr, argsLen, bufPtr, bufCap,
		r.deps.Compute != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a computeIDArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			instanceID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid instance id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Compute.GetInstance(tctx, tid, instanceID)
		})
}

func (r *registrar) computeInstanceList(
	ctx context.Context, m api.Module,
	argsPtr, argsLen, bufPtr, bufCap uint32,
) int32 {
	return r.jsonCall(ctx, m, computeModuleName, "instance_list",
		permission.CapComputeInstanceRead,
		argsPtr, argsLen, bufPtr, bufCap,
		r.deps.Compute != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a computeListArgs
			if len(args) > 0 {
				if err := json.Unmarshal(args, &a); err != nil {
					return nil, err
				}
			}
			if a.Limit <= 0 {
				a.Limit = 50
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Compute.ListInstances(tctx, tid, a.Limit, a.Offset)
		})
}

func (r *registrar) computeInstanceSetState(
	ctx context.Context, m api.Module,
	argsPtr, argsLen, bufPtr, bufCap uint32,
) int32 {
	return r.jsonCall(ctx, m, computeModuleName, "instance_set_state",
		permission.CapComputeInstanceControl,
		argsPtr, argsLen, bufPtr, bufCap,
		r.deps.Compute != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a computeSetStateArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			instanceID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid instance id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Compute.SetInstanceState(tctx, tid, uuid.Nil,
				instanceID, compute.InstanceLifecycleAction(a.Action),
				a.Force, a.TimeoutSecs)
		})
}

func (r *registrar) computeInstanceDelete(
	ctx context.Context, m api.Module,
	argsPtr, argsLen, bufPtr, bufCap uint32,
) int32 {
	return r.jsonCall(ctx, m, computeModuleName, "instance_delete",
		permission.CapComputeInstanceDelete,
		argsPtr, argsLen, bufPtr, bufCap,
		r.deps.Compute != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a computeIDArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			instanceID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid instance id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			if err := r.deps.Compute.DeleteInstance(tctx, tid, uuid.Nil, instanceID, false); err != nil {
				return nil, err
			}
			return deleteResult{Deleted: true, ID: a.ID}, nil
		})
}
