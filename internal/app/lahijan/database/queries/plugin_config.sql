-- plugin_config (WS-10b). One row per (plugin_id, key); the admin sets
-- these via the admin plugin API and the plugin reads them through the
-- config_get host function. Rows flagged is_secret = true are NEVER
-- surfaced through config_get.

-- name: UpsertPluginConfig :one
INSERT INTO plugin_config (tenant_id, plugin_id, key, value, is_secret)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (plugin_id, key) DO UPDATE
SET value       = EXCLUDED.value,
    is_secret   = EXCLUDED.is_secret,
    updated_at  = now()
RETURNING *;

-- name: GetPluginConfig :one
-- Read a single (plugin_id, key) row. The repository wrapper applies
-- the is_secret filter for plugin-facing reads (config_get returns
-- only non-secret values).
SELECT * FROM plugin_config
WHERE plugin_id = $1 AND key = $2;

-- name: ListPluginConfig :many
-- Every row for the plugin, including secrets (the admin UI shows
-- these). The plugin-facing read (config_get) uses a filtered path.
SELECT * FROM plugin_config
WHERE plugin_id = $1
ORDER BY key;

-- name: ListPluginConfigPublic :many
-- Every non-secret row for the plugin. This is the path config_get
-- uses; is_secret = true rows are deliberately excluded.
SELECT * FROM plugin_config
WHERE plugin_id = $1 AND is_secret = false
ORDER BY key;

-- name: DeletePluginConfig :exec
DELETE FROM plugin_config
WHERE plugin_id = $1 AND key = $2;
