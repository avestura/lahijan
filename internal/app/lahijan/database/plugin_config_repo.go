// Package database: plugin_config_repo.go wraps the sqlc-generated
// plugin_config queries (WS-10b). One row per (plugin_id, key); the admin
// sets these via the admin plugin API and the plugin reads them through
// the config_get host function. Rows flagged is_secret = true are NEVER
// surfaced through config_get — that filtering happens in this repo's
// PublicGet / PublicList methods.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// PluginConfigRepository is the persistence boundary for plugin_config.
type PluginConfigRepository struct {
	q *gen.Queries
}

// NewPluginConfigRepository wraps the given sqlc queries.
func NewPluginConfigRepository(q *gen.Queries) *PluginConfigRepository {
	return &PluginConfigRepository{q: q}
}

// UpsertPluginConfigParams carries the user-controlled fields of a config
// row. TenantID is *uuid.UUID (nullable): pass nil for a platform-wide
// plugin.
type UpsertPluginConfigParams struct {
	TenantID *uuid.UUID
	PluginID uuid.UUID
	Key      string
	Value    []byte
	IsSecret bool
}

// Set upserts a (plugin_id, key) row.
func (r *PluginConfigRepository) Set(ctx context.Context, arg UpsertPluginConfigParams) (gen.PluginConfig, error) {
	return r.q.UpsertPluginConfig(ctx, gen.UpsertPluginConfigParams{
		TenantID: arg.TenantID,
		PluginID: arg.PluginID,
		Key:      arg.Key,
		Value:    arg.Value,
		IsSecret: arg.IsSecret,
	})
}

// Get returns the raw row including secrets. Used by the admin UI; the
// plugin-facing read uses PublicGet.
func (r *PluginConfigRepository) Get(ctx context.Context, pluginID uuid.UUID, key string) (gen.PluginConfig, error) {
	return r.q.GetPluginConfig(ctx, gen.GetPluginConfigParams{PluginID: pluginID, Key: key})
}

// PublicGet returns the value for a (plugin_id, key) pair when the row is
// NOT marked secret. Returns ErrNoRows when the key does not exist OR is
// secret; this is the path config_get uses (WS-10b DoD: "config_get returns
// admin-set values, never secrets").
func (r *PluginConfigRepository) PublicGet(ctx context.Context, pluginID uuid.UUID, key string) (gen.PluginConfig, error) {
	row, err := r.q.GetPluginConfig(ctx, gen.GetPluginConfigParams{PluginID: pluginID, Key: key})
	if err != nil {
		return gen.PluginConfig{}, err
	}
	if row.IsSecret {
		// Treat as not-found so the plugin cannot distinguish "secret
		// exists" from "key does not exist" — both look the same to the
		// plugin.
		return gen.PluginConfig{}, ErrNoSecretVisible
	}
	return row, nil
}

// List returns every row including secrets. Used by the admin UI.
func (r *PluginConfigRepository) List(ctx context.Context, pluginID uuid.UUID) ([]gen.PluginConfig, error) {
	return r.q.ListPluginConfig(ctx, pluginID)
}

// PublicList returns every non-secret row. Used by the plugin's config_get
// enumeration path.
func (r *PluginConfigRepository) PublicList(ctx context.Context, pluginID uuid.UUID) ([]gen.PluginConfig, error) {
	return r.q.ListPluginConfigPublic(ctx, pluginID)
}

// Delete removes a (plugin_id, key) row. Missing rows are a no-op.
func (r *PluginConfigRepository) Delete(ctx context.Context, pluginID uuid.UUID, key string) error {
	return r.q.DeletePluginConfig(ctx, gen.DeletePluginConfigParams{PluginID: pluginID, Key: key})
}
