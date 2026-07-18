// Package database: plugin_http_handlers_repo.go wraps the
// sqlc-generated plugin_http_handlers queries (WS-10b). The api layer
// consults this table per request to /api/v1/plugins/<plugin-slug>/...
// to find the handler; on a hit it instantiates the plugin and calls the
// named export.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// PluginHTTPHandlersRepository is the persistence boundary for the
// plugin_http_handlers table.
type PluginHTTPHandlersRepository struct {
	q *gen.Queries
}

// NewPluginHTTPHandlersRepository wraps the given sqlc queries.
func NewPluginHTTPHandlersRepository(q *gen.Queries) *PluginHTTPHandlersRepository {
	return &PluginHTTPHandlersRepository{q: q}
}

// CreateHTTPHandlerParams carries the user-controlled fields of a handler
// mount row.
type CreateHTTPHandlerParams struct {
	TenantID *uuid.UUID
	PluginID uuid.UUID
	Method   string
	Path     string
	Handler  string
}

// Register idempotently adds a handler mount.
func (r *PluginHTTPHandlersRepository) Register(
	ctx context.Context, arg CreateHTTPHandlerParams,
) (gen.PluginHttpHandler, error) {
	return r.q.CreatePluginHTTPHandler(ctx, gen.CreatePluginHTTPHandlerParams{
		TenantID: arg.TenantID,
		PluginID: arg.PluginID,
		Method:   arg.Method,
		Path:     arg.Path,
		Handler:  arg.Handler,
	})
}

// Unregister removes a single handler mount. Missing rows are a no-op.
func (r *PluginHTTPHandlersRepository) Unregister(
	ctx context.Context,
	pluginID uuid.UUID, method, path string,
) error {
	return r.q.DeletePluginHTTPHandler(ctx, gen.DeletePluginHTTPHandlerParams{
		PluginID: pluginID, Method: method, Path: path,
	})
}

// ListForPlugin returns every mount owned by the plugin.
func (r *PluginHTTPHandlersRepository) ListForPlugin(
	ctx context.Context, pluginID uuid.UUID,
) ([]gen.PluginHttpHandler, error) {
	return r.q.ListPluginHTTPHandlers(ctx, pluginID)
}

// Find returns the row matching the (plugin_id, method, path) tuple.
// Used by the HTTP router; returns ErrNoRows when no row matches.
func (r *PluginHTTPHandlersRepository) Find(
	ctx context.Context,
	pluginID uuid.UUID, method, path string,
) (gen.PluginHttpHandler, error) {
	return r.q.FindPluginHTTPHandler(ctx, gen.FindPluginHTTPHandlerParams{
		PluginID: pluginID, Method: method, Path: path,
	})
}

// DeleteAllForPlugin removes every mount owned by the plugin. CASCADE on
// plugin_id already covers plugin deletes; this covers the "admin
// force-unmount" path used when a plugin is disabled (WS-10b DoD: "removes
// it when the plugin is disabled").
func (r *PluginHTTPHandlersRepository) DeleteAllForPlugin(
	ctx context.Context, pluginID uuid.UUID,
) (int64, error) {
	return r.q.DeletePluginHTTPHandlersForPlugin(ctx, pluginID)
}
