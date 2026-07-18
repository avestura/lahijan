-- plugins + plugin_permissions (WS-10a). The plugins table is the install
-- record; plugin_permissions is the grant table the enforcer consults at
-- every host call. tenant_id is nullable on plugins so the same table can
-- hold tenant-scoped AND platform-wide plugins (see migration 0020).
--
-- The repository wrapper (database/plugins_repo.go) handles the NULLable
-- tenant_id at the Go seam; queries here are explicit about whether they
-- pass it.

-- ===========================================================================
-- plugins
-- ===========================================================================

-- name: CreatePlugin :one
INSERT INTO plugins (
    tenant_id,
    name,
    version,
    description,
    wasm_hash,
    wasm_bytes,
    wasm_size,
    manifest_json,
    status,
    signature
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetPlugin :one
-- Lookup by id; works for both tenant-scoped and platform-wide plugins.
-- The repository wrapper enforces tenant scoping for non-platform callers.
SELECT * FROM plugins WHERE id = $1;

-- name: GetPluginForTenant :one
--: tenant-scoped; returns the row if it belongs to the tenant in ctx, OR is
--: platform-wide (tenant_id IS NULL) so tenants can read platform-wide
--: plugins they did not install. This mirrors the audit_log rule for
--: system-level events.
SELECT * FROM plugins
WHERE id = $2
  AND (tenant_id = $1 OR tenant_id IS NULL);

-- name: ListPluginsForTenant :many
--: tenant-scoped; the tenant's own plugins plus platform-wide plugins.
SELECT * FROM plugins
WHERE tenant_id = $1 OR tenant_id IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountPluginsForTenant :one
--: tenant-scoped; pagination counterpart to ListPluginsForTenant.
SELECT count(*) FROM plugins
WHERE tenant_id = $1 OR tenant_id IS NULL;

-- name: ListPluginsGlobal :many
--: admin-only; platform-wide listing across every tenant.
SELECT * FROM plugins
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPluginsGlobal :one
--: admin-only; pagination counterpart to ListPluginsGlobal.
SELECT count(*) FROM plugins;

-- name: SetPluginStatus :exec
-- Promote a plugin from pending -> active, or active -> disabled. The
-- CHECK constraint on the column rejects any other value at the DB layer.
UPDATE plugins
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: DeletePlugin :exec
-- Hard delete. Used by the admin uninstall endpoint. CASCADE removes the
-- associated plugin_permissions rows (FK ON DELETE CASCADE).
DELETE FROM plugins WHERE id = $1;

-- name: FindPluginsByNameGlobal :many
--: admin-only; every row across every tenant with the given name. Used by
--: the marketplace upgrade flow to locate the previous version(s) of a
--: plugin before swapping it for the new one. Ordered by created_at DESC
--: so the newest prior version comes first.
SELECT * FROM plugins
WHERE name = $1
ORDER BY created_at DESC;

-- name: FindPluginsByNameForTenant :many
--: tenant-scoped; every row visible to the tenant in ctx with the given
--: name (the tenant's own + platform-wide). Used by the tenant-scoped
--: marketplace upgrade flow.
SELECT * FROM plugins
WHERE name = $2
  AND (tenant_id = $1 OR tenant_id IS NULL)
ORDER BY created_at DESC;

-- ===========================================================================
-- plugin_permissions
-- ===========================================================================

-- name: GrantPluginPermission :exec
-- Idempotent: ON CONFLICT DO NOTHING so re-granting an already-held
-- permission is a no-op (the audit row still records the action).
INSERT INTO plugin_permissions (plugin_id, permission, granted_by_user_id)
VALUES ($1, $2, $3)
ON CONFLICT (plugin_id, permission) DO NOTHING;

-- name: RevokePluginPermission :exec
-- Removes a grant. The next host call that needs this permission will be
-- rejected by the enforcer; if the plugin is mid-execution it is cancelled.
DELETE FROM plugin_permissions
WHERE plugin_id = $1 AND permission = $2;

-- name: ListPluginPermissions :many
-- Every grant held by the plugin; used by the enforcer + the admin detail
-- endpoint.
SELECT * FROM plugin_permissions
WHERE plugin_id = $1
ORDER BY granted_at DESC;

-- name: GetPluginPermission :one
-- Single-row lookup used by the enforcer's fast path before falling back to
-- the prefix-match loop. Returns the row when the plugin holds an exact
-- grant for the permission string.
SELECT * FROM plugin_permissions
WHERE plugin_id = $1 AND permission = $2;

-- name: CountPluginPermissions :one
-- Number of grants held by the plugin; surfaced in the admin detail view.
SELECT count(*) FROM plugin_permissions WHERE plugin_id = $1;
