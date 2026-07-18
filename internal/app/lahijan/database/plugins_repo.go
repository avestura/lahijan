// Package database: plugins_repo.go wraps the sqlc-generated plugins and
// plugin_permissions queries (WS-10a). plugins.tenant_id is NULLABLE: a
// NULL row is platform-wide, a non-NULL row is tenant-scoped. The repository
// exposes both shapes — admin (platform-wide) callers pass nil for the
// tenant scope, tenant-scoped callers go through WithTenant as usual.
//
// plugin_permissions is the grant table the enforcer consults on every host
// call. Grants are stored as opaque strings ("scope.action" or
// "scope.action:qualifier"); the enforcer (wasm/permission) does the prefix
// matching.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// Plugin status values. Mirrors the CHECK constraint in migration 0020.
const (
	PluginStatusPending  = "pending"
	PluginStatusActive   = "active"
	PluginStatusDisabled = "disabled"
)

// PluginsRepository is the persistence boundary for the plugins and
// plugin_permissions tables.
type PluginsRepository struct {
	q *gen.Queries
}

// NewPluginsRepository wraps the given sqlc queries.
func NewPluginsRepository(q *gen.Queries) *PluginsRepository {
	return &PluginsRepository{q: q}
}

// CreatePluginParams carries the user-controlled fields of a new plugin.
// TenantID is *uuid.UUID (nullable): pass nil for a platform-wide plugin.
// Signature is also nullable; pass nil to omit.
type CreatePluginParams struct {
	TenantID    *uuid.UUID
	Name        string
	Version     string
	Description string
	WasmHash    string
	WasmBytes   []byte
	WasmSize    int64
	Manifest    json.RawMessage
	Status      string
	Signature   []byte
}

// Create inserts a new plugin row and returns it.
func (r *PluginsRepository) Create(ctx context.Context, arg CreatePluginParams) (gen.Plugin, error) {
	status := arg.Status
	if status == "" {
		status = PluginStatusPending
	}
	manifest := arg.Manifest
	if len(manifest) == 0 {
		manifest = json.RawMessage("{}")
	}
	return r.q.CreatePlugin(ctx, gen.CreatePluginParams{
		TenantID:     arg.TenantID,
		Name:         arg.Name,
		Version:      arg.Version,
		Description:  arg.Description,
		WasmHash:     arg.WasmHash,
		WasmBytes:    arg.WasmBytes,
		WasmSize:     arg.WasmSize,
		ManifestJson: manifest,
		Status:       status,
		Signature:    arg.Signature,
	})
}

// Get returns the plugin row by id, without tenant scoping. Admin-only.
func (r *PluginsRepository) Get(ctx context.Context, id uuid.UUID) (gen.Plugin, error) {
	return r.q.GetPlugin(ctx, id)
}

// GetForTenant returns the plugin row by id if it belongs to the tenant in
// ctx, OR is platform-wide (NULL tenant_id). A plugin owned by a different
// tenant is not visible.
func (r *PluginsRepository) GetForTenant(ctx context.Context, id uuid.UUID) (gen.Plugin, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.Plugin{}, err
	}
	t := &tenantID
	return r.q.GetPluginForTenant(ctx, gen.GetPluginForTenantParams{
		TenantID: t,
		ID:       id,
	})
}

// ListForTenant returns the tenant's plugins plus every platform-wide plugin
// (tenant_id IS NULL), newest first.
func (r *PluginsRepository) ListForTenant(
	ctx context.Context,
	limit, offset int32,
) ([]gen.Plugin, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	t := &tenantID
	return r.q.ListPluginsForTenant(ctx, gen.ListPluginsForTenantParams{
		TenantID: t, Limit: limit, Offset: offset,
	})
}

// CountForTenant counts plugins visible to the tenant in ctx (own +
// platform-wide).
func (r *PluginsRepository) CountForTenant(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	t := &tenantID
	return r.q.CountPluginsForTenant(ctx, t)
}

// ListGlobal returns every plugin across every tenant. Admin-only.
func (r *PluginsRepository) ListGlobal(
	ctx context.Context,
	limit, offset int32,
) ([]gen.Plugin, error) {
	return r.q.ListPluginsGlobal(ctx, gen.ListPluginsGlobalParams{
		Limit: limit, Offset: offset,
	})
}

// CountGlobal counts every plugin across every tenant.
func (r *PluginsRepository) CountGlobal(ctx context.Context) (int64, error) {
	return r.q.CountPluginsGlobal(ctx)
}

// SetStatus updates a plugin's status. Use PluginStatus* constants. The DB's
// CHECK constraint rejects unknown values.
func (r *PluginsRepository) SetStatus(ctx context.Context, id uuid.UUID, status string) error {
	return r.q.SetPluginStatus(ctx, gen.SetPluginStatusParams{ID: id, Status: status})
}

// Delete hard-deletes the plugin. CASCADE removes its grants.
func (r *PluginsRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.q.DeletePlugin(ctx, id)
}

// GrantPermission idempotently adds a grant. Re-granting an existing
// permission is a no-op (ON CONFLICT DO NOTHING).
func (r *PluginsRepository) GrantPermission(
	ctx context.Context,
	pluginID, grantedByUserID uuid.UUID,
	permission string,
) error {
	return r.q.GrantPluginPermission(ctx, gen.GrantPluginPermissionParams{
		PluginID:        pluginID,
		Permission:      permission,
		GrantedByUserID: grantedByUserID,
	})
}

// RevokePermission removes a grant. Missing rows are a no-op.
func (r *PluginsRepository) RevokePermission(
	ctx context.Context,
	pluginID uuid.UUID,
	permission string,
) error {
	return r.q.RevokePluginPermission(ctx, gen.RevokePluginPermissionParams{
		PluginID: pluginID, Permission: permission,
	})
}

// ListPermissions returns every grant held by the plugin.
func (r *PluginsRepository) ListPermissions(
	ctx context.Context,
	pluginID uuid.UUID,
) ([]gen.PluginPermission, error) {
	return r.q.ListPluginPermissions(ctx, pluginID)
}

// HasExactGrant reports whether the plugin holds an exact grant for the
// permission string. Used by the enforcer's fast path; the prefix-match
// fallback lives in the enforcer (wasm/permission) which uses ListPermissions.
func (r *PluginsRepository) HasExactGrant(
	ctx context.Context,
	pluginID uuid.UUID,
	permission string,
) (bool, error) {
	_, err := r.q.GetPluginPermission(ctx, gen.GetPluginPermissionParams{
		PluginID: pluginID, Permission: permission,
	})
	if err == nil {
		return true, nil
	}
	if IsNoRows(err) {
		return false, nil
	}
	return false, err
}

// CountPermissions returns the number of grants held by the plugin.
func (r *PluginsRepository) CountPermissions(ctx context.Context, pluginID uuid.UUID) (int64, error) {
	return r.q.CountPluginPermissions(ctx, pluginID)
}
