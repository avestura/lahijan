-- Runtime platform settings (global table; see migration 0051).

-- name: GetPlatformSetting :one
SELECT * FROM platform_settings WHERE key = $1;

-- name: UpsertPlatformSetting :one
INSERT INTO platform_settings (key, value, updated_by)
VALUES ($1, $2, $3)
ON CONFLICT (key)
DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: DeletePlatformSetting :exec
DELETE FROM platform_settings WHERE key = $1;
