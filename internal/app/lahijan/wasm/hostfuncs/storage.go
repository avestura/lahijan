// Package hostfuncs: storage.go builds the lahijan_storage host module
// (WS-10f). Plugins manage S3 buckets through gated host functions that
// delegate to the storage service. Every call enforces tenant scoping +
// audit.
//
// ABI: each function takes (args_ptr, args_len, buf_ptr, buf_cap) and
// returns bytes-written (>0, JSON in buf) or a negative StatusXxx code.
package hostfuncs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/avestura/lahijan/internal/app/lahijan/storage"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const storageModuleName = "lahijan_storage"

type storageBucketCreateArgs struct {
	Slug         string `json:"slug"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	QuotaBytes   int64  `json:"quota_bytes"`
	QuotaObjects int64  `json:"quota_objects"`
}

func (r *registrar) buildStorageModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(storageModuleName)
	params4 := []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}
	resultI32 := []api.ValueType{api.ValueTypeI32}

	mk := func(name string, fn func(ctx context.Context, m api.Module, s []uint64)) {
		mod.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(fn), params4, resultI32).
			Export(name)
	}

	mk("bucket_create", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.storageBucketCreate(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("bucket_get", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.storageBucketGet(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("bucket_list", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.storageBucketList(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("bucket_delete", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.storageBucketDelete(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate storage module: %w", err)
	}
	return nil
}

func (r *registrar) storageBucketCreate(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, storageModuleName, "bucket_create", permission.CapStorageBucketCreate,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.Storage != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a storageBucketCreateArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Storage.CreateBucket(tctx, tid, uuid.Nil, storage.BucketCreateParams{
				Slug: a.Slug, Label: a.Label, Description: a.Description,
				QuotaBytes: a.QuotaBytes, QuotaObjects: a.QuotaObjects,
			})
		})
}

func (r *registrar) storageBucketGet(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, storageModuleName, "bucket_get", permission.CapStorageBucketRead,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.Storage != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsIDArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			bucketID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid bucket id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Storage.GetBucket(tctx, tid, bucketID)
		})
}

func (r *registrar) storageBucketList(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, storageModuleName, "bucket_list", permission.CapStorageBucketRead,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.Storage != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a computeListArgs
			if len(args) > 0 {
				_ = json.Unmarshal(args, &a)
			}
			if a.Limit <= 0 {
				a.Limit = 50
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.Storage.ListBuckets(tctx, tid, a.Limit, a.Offset)
		})
}

func (r *registrar) storageBucketDelete(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, storageModuleName, "bucket_delete", permission.CapStorageBucketDelete,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.Storage != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsIDArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			bucketID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid bucket id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			if err := r.deps.Storage.DeleteBucket(tctx, tid, uuid.Nil, bucketID); err != nil {
				return nil, err
			}
			return deleteResult{Deleted: true, ID: a.ID}, nil
		})
}
