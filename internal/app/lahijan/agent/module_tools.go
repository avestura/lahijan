// Package agent: module_tools.go is the production ToolExecutor that lets the
// agent read live data from the compute / DNS / storage / billing / audit
// modules. It maps a small set of READ-ONLY tool names to the existing
// repositories, enforces the tenant scoping that lives at the repository seam
// (the context the harness passes to Execute carries the tenant id), and
// projects each row to a small JSON payload so the model never sees internal
// columns (tenant_id, config blobs, soft-delete markers, ...).
//
// This bridge is intentionally read-only: every tool reports Destructive()
// false, so the LLMHarness executes them inline and loops to produce an
// answer grounded in real data. Destructive module actions (create / delete /
// modify) will land as a follow-on and will reuse the existing
// human-in-the-loop confirm flow.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// ModuleToolBridge exposes the read-only list tools over the compute, DNS,
// storage, billing, and audit repositories. The repos enforce tenant scoping
// from the context, so the bridge never handles a tenant id directly.
type ModuleToolBridge struct {
	zones     *database.DNSZonesRepository
	instances *database.ComputeInstancesRepository
	buckets   *database.StorageBucketsRepository
	usage     *database.BillingUsageRepository
	auditLog  *database.AuditLogRepository
}

var _ ToolExecutor = ModuleToolBridge{}

// NewModuleToolBridge builds the bridge from the shared repository aggregate.
// Passing the aggregate (rather than individual repos) keeps the program
// wiring stable as new tools are added.
func NewModuleToolBridge(repos *database.Repos) ModuleToolBridge {
	return ModuleToolBridge{
		zones:     repos.DNSZones,
		instances: repos.ComputeInstances,
		buckets:   repos.StorageBuckets,
		usage:     repos.BillingUsage,
		auditLog:  repos.AuditLog,
	}
}

// tool name constants keep the descriptor list, the switch in Execute, and the
// schema table in lockstep.
const (
	toolDNSListZones       = "dns.list_zones"
	toolComputeListInsts   = "compute.list_instances"
	toolStorageListBuckets = "storage.list_buckets"
	toolBillingListUsage   = "billing.list_usage"
	toolAuditListEvents    = "audit.list_events"
)

// listLimit bounds how many rows a single tool call returns, so a tenant with
// thousands of rows cannot blow out the model's context window. The model can
// still ask for fewer via the "limit" argument.
const listLimit = 50

// Destructive implements ToolExecutor. Every module tool exposed here is
// read-only, so this always returns false.
func (ModuleToolBridge) Destructive(string) bool { return false }

// Execute implements ToolExecutor.
func (b ModuleToolBridge) Execute(ctx context.Context, tool string, args json.RawMessage) (json.RawMessage, error) {
	limit := parseLimit(args)
	switch tool {
	case toolDNSListZones:
		rows, err := b.zones.List(ctx, limit, 0)
		if err != nil {
			return nil, fmt.Errorf("dns.list_zones: %w", err)
		}
		return marshalRows(rows, projectZone)
	case toolComputeListInsts:
		rows, err := b.instances.List(ctx, limit, 0)
		if err != nil {
			return nil, fmt.Errorf("compute.list_instances: %w", err)
		}
		return marshalRows(rows, projectInstance)
	case toolStorageListBuckets:
		rows, err := b.buckets.List(ctx, limit, 0)
		if err != nil {
			return nil, fmt.Errorf("storage.list_buckets: %w", err)
		}
		return marshalRows(rows, projectBucket)
	case toolBillingListUsage:
		rows, err := b.usage.ListForUser(
			ctx, actorUserIDFromContext(ctx),
			database.UsageListFilter{}, limit, 0,
		)
		if err != nil {
			return nil, fmt.Errorf("billing.list_usage: %w", err)
		}
		return marshalRows(rows, projectUsage)
	case toolAuditListEvents:
		rows, err := b.auditLog.ListForTenant(ctx, limit, 0)
		if err != nil {
			return nil, fmt.Errorf("audit.list_events: %w", err)
		}
		return marshalRows(rows, projectAudit)
	default:
		return nil, ErrToolNotFound
	}
}

// Describe implements ToolExecutor.
func (ModuleToolBridge) Describe(tool string) (ToolDescriptor, bool) {
	for _, d := range moduleToolDescriptors {
		if d.Name == tool {
			return d, true
		}
	}
	return ToolDescriptor{}, false
}

// All implements ToolExecutor.
func (ModuleToolBridge) All() []ToolDescriptor {
	out := make([]ToolDescriptor, len(moduleToolDescriptors))
	copy(out, moduleToolDescriptors)
	return out
}

// moduleToolDescriptors is the fixed catalog advertised to the model. The
// Parameters schemas are minimal (an optional "limit"); they exist so the
// model knows the tools are callable and how to constrain the result size.
var moduleToolDescriptors = []ToolDescriptor{
	{
		Name:        toolDNSListZones,
		Description: "List the caller's DNS zones (name, kind, DNSSEC + AXFR flags, description). Read-only.",
		Parameters:  listParamsSchema(),
	},
	{
		Name:        toolComputeListInsts,
		Description: "List the caller's compute instances (name, type, status, image, profiles). Read-only.",
		Parameters:  listParamsSchema(),
	},
	{
		Name:        toolStorageListBuckets,
		Description: "List the caller's object-storage buckets (name, label, quota, usage, versioning). Read-only.",
		Parameters:  listParamsSchema(),
	},
	{
		Name:        toolBillingListUsage,
		Description: "List the caller's recent metering/usage events (resource, quantity, unit, time). Read-only.",
		Parameters:  listParamsSchema(),
	},
	{
		Name:        toolAuditListEvents,
		Description: "List recent audit events in the tenant (action, actor, resource, status, time). Read-only.",
		Parameters:  listParamsSchema(),
	},
}

// listParamsSchema is the shared schema for the list tools: an optional
// integer "limit" (1..50) capping the number of rows returned.
func listParamsSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Max rows to return (default 50)."}
  },
  "additionalProperties": false
}`)
}

// parseLimit extracts the optional "limit" argument, clamped to [1, listLimit].
// Missing or malformed args fall back to listLimit rather than erroring, so a
// model that calls the tool with {} still gets a useful result.
func parseLimit(args json.RawMessage) int32 {
	if len(args) == 0 {
		return listLimit
	}
	var p struct {
		Limit *int32 `json:"limit"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Limit == nil {
		return listLimit
	}
	n := *p.Limit
	if n < 1 {
		return 1
	}
	if n > listLimit {
		return listLimit
	}
	return n
}

// marshalRows projects each row via fn and wraps the slice in an envelope that
// reports the count, so the model can answer "how many" without recounting.
func marshalRows[T, R any](rows []T, fn func(T) R) (json.RawMessage, error) {
	out := make([]R, 0, len(rows))
	for _, r := range rows {
		out = append(out, fn(r))
	}
	return json.Marshal(map[string]any{"count": len(out), "items": out})
}

// ----- row projectors (drop internal / sensitive columns) -------------------

func projectZone(z gen.DnsZone) map[string]any {
	return map[string]any{
		"id":                z.ID,
		"name":              z.Name,
		"kind":              z.Kind,
		"is_dnssec_enabled": z.IsDnssecEnabled,
		"is_axfr_enabled":   z.IsAxfrEnabled,
		"description":       z.Description,
	}
}

func projectInstance(i gen.ComputeInstance) map[string]any {
	return map[string]any{
		"id":          i.ID,
		"name":        i.Name,
		"type":        i.Type,
		"status":      i.Status,
		"image":       i.ImageAlias,
		"profiles":    i.Profiles,
		"description": i.Description,
	}
}

func projectBucket(b gen.StorageBucket) map[string]any {
	return map[string]any{
		"id":                  b.ID,
		"name":                b.Name,
		"label":               b.Label,
		"description":         b.Description,
		"quota_bytes":         b.QuotaBytes,
		"quota_objects":       b.QuotaObjects,
		"bytes_used":          b.BytesUsed,
		"objects_used":        b.ObjectsUsed,
		"versioning":          b.VersioningStatus,
		"object_lock_enabled": b.ObjectLockEnabled,
	}
}

func projectUsage(u gen.UsageEvent) map[string]any {
	return map[string]any{
		"id":            u.ID,
		"resource_type": u.ResourceType,
		"qty":           u.Qty,
		"unit":          u.Unit,
		"started_at":    u.StartedAt,
		"ended_at":      u.EndedAt,
	}
}

func projectAudit(a gen.AuditLog) map[string]any {
	out := map[string]any{
		"id":            a.ID,
		"action":        a.Action,
		"resource_type": a.ResourceType,
		"actor_type":    a.ActorType,
		"status":        a.Status,
		"created_at":    a.CreatedAt,
	}
	if a.ActorUserID != nil {
		out["actor_user_id"] = *a.ActorUserID
	}
	if a.ResourceID != nil {
		out["resource_id"] = *a.ResourceID
	}
	return out
}
