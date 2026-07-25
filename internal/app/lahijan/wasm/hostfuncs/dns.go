// Package hostfuncs: dns.go builds the lahijan_dns host module (WS-10f).
// Plugins manage DNS zones and records through gated host functions that
// delegate to the DNS service. Every call enforces tenant scoping + audit.
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

	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/permission"
)

const dnsModuleName = "lahijan_dns"

type dnsZoneCreateArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
}

type dnsIDArgs struct {
	ID string `json:"id"`
}

type dnsRecordCreateArgs struct {
	ZoneID  string `json:"zone_id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type dnsRecordListArgs struct {
	ZoneID string `json:"zone_id"`
	Limit  int32  `json:"limit"`
	Offset int32  `json:"offset"`
}

type dnsRecordDeleteArgs struct {
	ZoneID   string `json:"zone_id"`
	RecordID string `json:"record_id"`
}

func (r *registrar) buildDNSModule(ctx context.Context, rt wazero.Runtime) error {
	mod := rt.NewHostModuleBuilder(dnsModuleName)
	params4 := []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}
	resultI32 := []api.ValueType{api.ValueTypeI32}

	mk := func(name string, fn func(ctx context.Context, m api.Module, s []uint64)) {
		mod.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(fn), params4, resultI32).
			Export(name)
	}

	mk("zone_create", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsZoneCreate(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("zone_get", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsZoneGet(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("zone_list", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsZoneList(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("zone_delete", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsZoneDelete(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("record_create", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsRecordCreate(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("record_list", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsRecordList(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})
	mk("record_delete", func(ctx context.Context, m api.Module, s []uint64) {
		s[0] = api.EncodeI32(r.dnsRecordDelete(ctx, m, api.DecodeU32(s[0]), api.DecodeU32(s[1]), api.DecodeU32(s[2]), api.DecodeU32(s[3])))
	})

	if _, err := mod.Instantiate(ctx); err != nil {
		return fmt.Errorf("instantiate dns module: %w", err)
	}
	return nil
}

func (r *registrar) dnsZoneCreate(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "zone_create", permission.CapDNSZoneCreate,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsZoneCreateArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.DNS.CreateZone(tctx, tid, uuid.Nil, dns.ZoneCreateParams{
				Name: a.Name, Description: a.Description, Kind: a.Kind,
			})
		})
}

func (r *registrar) dnsZoneGet(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "zone_get", permission.CapDNSZoneRead,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsIDArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			zoneID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid zone id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.DNS.GetZone(tctx, tid, zoneID)
		})
}

func (r *registrar) dnsZoneList(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "zone_list", permission.CapDNSZoneRead,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
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
			return r.deps.DNS.ListZones(tctx, tid, a.Limit, a.Offset)
		})
}

func (r *registrar) dnsZoneDelete(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "zone_delete", permission.CapDNSZoneDelete,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsIDArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			zoneID, err := uuid.Parse(a.ID)
			if err != nil {
				return nil, fmt.Errorf("invalid zone id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			if err := r.deps.DNS.DeleteZone(tctx, tid, uuid.Nil, zoneID); err != nil {
				return nil, err
			}
			return deleteResult{Deleted: true, ID: a.ID}, nil
		})
}

func (r *registrar) dnsRecordCreate(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "record_create", permission.CapDNSRecordCreate,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsRecordCreateArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			zoneID, err := uuid.Parse(a.ZoneID)
			if err != nil {
				return nil, fmt.Errorf("invalid zone id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.DNS.CreateRecord(tctx, tid, uuid.Nil, zoneID, dns.RecordCreateParams{
				Name: a.Name, Type: a.Type, Content: a.Content, TTL: a.TTL,
			})
		})
}

func (r *registrar) dnsRecordList(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "record_list", permission.CapDNSRecordRead,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsRecordListArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			zoneID, err := uuid.Parse(a.ZoneID)
			if err != nil {
				return nil, fmt.Errorf("invalid zone id: %w", err)
			}
			if a.Limit <= 0 {
				a.Limit = 50
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			return r.deps.DNS.ListRecords(tctx, tid, zoneID, a.Limit, a.Offset)
		})
}

func (r *registrar) dnsRecordDelete(ctx context.Context, m api.Module, argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	return r.jsonCall(ctx, m, dnsModuleName, "record_delete", permission.CapDNSRecordDelete,
		argsPtr, argsLen, bufPtr, bufCap, r.deps.DNS != nil,
		func(ctx context.Context, args []byte) (any, error) {
			var a dnsRecordDeleteArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, err
			}
			zoneID, err := uuid.Parse(a.ZoneID)
			if err != nil {
				return nil, fmt.Errorf("invalid zone id: %w", err)
			}
			recordID, err := uuid.Parse(a.RecordID)
			if err != nil {
				return nil, fmt.Errorf("invalid record id: %w", err)
			}
			tctx, tid, err := r.resolveTenantCtx(ctx, pidFromContext(ctx))
			if err != nil {
				return nil, err
			}
			if err := r.deps.DNS.DeleteRecord(tctx, tid, uuid.Nil, zoneID, recordID); err != nil {
				return nil, err
			}
			return deleteResult{Deleted: true, ID: a.RecordID}, nil
		})
}
