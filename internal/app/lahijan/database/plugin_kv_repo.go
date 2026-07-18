// Package database: plugin_kv_repo.go wraps the sqlc-generated plugin_kv
// queries (WS-10b). The (plugin_id, key) pair is the natural key; the host
// function injects plugin_id from the calling Instance's identity so a
// plugin cannot address another plugin's namespace.
//
// The repository intentionally takes plugin_id explicitly (NOT from context)
// because plugin identity is established by the runtime, not by middleware
// (a plugin is not a user). Tenant scoping is also explicit, mirroring
// plugins' NULL-tenant semantics.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// PluginKVRepository is the persistence boundary for the plugin_kv table.
type PluginKVRepository struct {
	q *gen.Queries
}

// NewPluginKVRepository wraps the given sqlc queries.
func NewPluginKVRepository(q *gen.Queries) *PluginKVRepository {
	return &PluginKVRepository{q: q}
}

// UpsertPluginKVParams carries the user-controlled fields of a KV row.
// TenantID is *uuid.UUID (nullable): pass nil for a platform-wide plugin.
// ExpiresAt is *time.Time (nullable): pass nil for no TTL.
type UpsertPluginKVParams struct {
	TenantID  *uuid.UUID
	PluginID  uuid.UUID
	Key       string
	Value     []byte
	ExpiresAt *time.Time
}

// Set upserts a (plugin_id, key) row. On conflict the value + expires_at are
// replaced and updated_at is bumped.
func (r *PluginKVRepository) Set(ctx context.Context, arg UpsertPluginKVParams) (gen.PluginKv, error) {
	return r.q.UpsertPluginKV(ctx, gen.UpsertPluginKVParams{
		TenantID:  arg.TenantID,
		PluginID:  arg.PluginID,
		Key:       arg.Key,
		Value:     arg.Value,
		ExpiresAt: arg.ExpiresAt,
	})
}

// Get returns the unexpired value for the (plugin_id, key) pair. Returns
// ErrNoRows when the key does not exist OR has expired; callers use
// database.IsNoRows to detect both.
func (r *PluginKVRepository) Get(ctx context.Context, pluginID uuid.UUID, key string) (gen.PluginKv, error) {
	return r.q.GetPluginKV(ctx, gen.GetPluginKVParams{PluginID: pluginID, Key: key})
}

// Delete removes a (plugin_id, key) row. Missing rows are a no-op.
func (r *PluginKVRepository) Delete(ctx context.Context, pluginID uuid.UUID, key string) error {
	return r.q.DeletePluginKV(ctx, gen.DeletePluginKVParams{PluginID: pluginID, Key: key})
}

// DeleteExpired removes every expired row. Returns the count deleted; the
// cleanup job (post-MVP) calls this periodically.
func (r *PluginKVRepository) DeleteExpired(ctx context.Context) (int64, error) {
	return r.q.DeleteExpiredPluginKV(ctx)
}
