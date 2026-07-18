-- plugin_kv (WS-10b). The (plugin_id, key) pair is the natural key; the
-- repository wrapper enforces plugin scoping by always passing plugin_id
-- from the host-function context. Reads filter out expired rows.

-- name: UpsertPluginKV :one
-- Idempotent insert-or-update by (plugin_id, key). On conflict, the
-- value + expires_at are replaced and updated_at is bumped. The
-- repository wrapper wraps this in a SELECT-after-INSERT for callers
-- that need the row; the conflict target is the unique index.
INSERT INTO plugin_kv (tenant_id, plugin_id, key, value, expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (plugin_id, key) DO UPDATE
SET value       = EXCLUDED.value,
    expires_at  = EXCLUDED.expires_at,
    updated_at  = now()
RETURNING *;

-- name: GetPluginKV :one
-- Read a single (plugin_id, key) row. The repo wrapper applies the
-- expiry filter (expires_at IS NULL OR expires_at > now()) so callers
-- never see stale data.
SELECT * FROM plugin_kv
WHERE plugin_id = $1 AND key = $2
  AND (expires_at IS NULL OR expires_at > now());

-- name: DeletePluginKV :exec
-- Remove a single (plugin_id, key) row. Missing rows are a no-op.
DELETE FROM plugin_kv
WHERE plugin_id = $1 AND key = $2;

-- name: DeleteExpiredPluginKV :execrows
-- Bulk-delete every expired row. The cleanup job (post-MVP) calls this
-- periodically; the rowsize is small so a single bulk delete is fine.
DELETE FROM plugin_kv
WHERE expires_at IS NOT NULL AND expires_at <= now();
