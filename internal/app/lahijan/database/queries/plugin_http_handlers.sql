-- plugin_http_handlers (WS-10b). The api layer consults this table on
-- every request to /api/v1/plugins/<plugin-slug>/... to find the
-- handler; on a hit it instantiates the plugin and calls the named export.

-- name: CreatePluginHTTPHandler :one
-- Idempotent on (plugin_id, method, path).
INSERT INTO plugin_http_handlers (tenant_id, plugin_id, method, path, handler)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (plugin_id, method, path) DO NOTHING
RETURNING *;

-- name: DeletePluginHTTPHandler :exec
DELETE FROM plugin_http_handlers
WHERE plugin_id = $1 AND method = $2 AND path = $3;

-- name: ListPluginHTTPHandlers :many
SELECT * FROM plugin_http_handlers
WHERE plugin_id = $1
ORDER BY method, path;

-- name: FindPluginHTTPHandler :one
-- Single-row lookup the router uses per request. The router further
-- filters by tenant visibility (the tenant_id on the row must match
-- the request's tenant OR be NULL for platform-wide plugins).
SELECT * FROM plugin_http_handlers
WHERE plugin_id = $1 AND method = $2 AND path = $3;

-- name: DeletePluginHTTPHandlersForPlugin :execrows
DELETE FROM plugin_http_handlers
WHERE plugin_id = $1;
