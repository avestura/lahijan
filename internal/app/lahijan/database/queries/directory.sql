-- Directory connections (LDAP / SAML) and the users + groups imported from
-- them. Global (platform-level) tables; see migration 0050.

-- name: CreateDirectoryConnection :one
INSERT INTO directory_connections (kind, name, enabled, config, secret_encrypted, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetDirectoryConnection :one
SELECT * FROM directory_connections WHERE id = $1;

-- name: ListDirectoryConnections :many
SELECT * FROM directory_connections ORDER BY created_at DESC, id;

-- name: UpdateDirectoryConnection :one
-- The secret is only replaced when a new one is supplied ($5 IS NOT NULL).
UPDATE directory_connections
SET name = $2,
    enabled = $3,
    config = $4,
    secret_encrypted = COALESCE($5, secret_encrypted),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteDirectoryConnection :exec
DELETE FROM directory_connections WHERE id = $1;

-- name: SetDirectoryConnectionSyncResult :exec
UPDATE directory_connections
SET last_sync_at = now(),
    last_sync_status = $2,
    last_sync_message = $3,
    last_sync_users = $4,
    last_sync_groups = $5,
    updated_at = now()
WHERE id = $1;

-- name: UpsertDirectoryGroup :one
INSERT INTO directory_groups (connection_id, external_id, name, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (connection_id, external_id)
DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description, synced_at = now()
RETURNING *;

-- name: DeleteStaleDirectoryGroups :exec
-- Drop groups of a connection that were not seen in the latest sync.
DELETE FROM directory_groups
WHERE connection_id = $1 AND NOT (external_id = ANY($2::text[]));

-- name: ListDirectoryGroups :many
SELECT g.*, (SELECT count(*) FROM directory_group_members gm WHERE gm.group_id = g.id)::int AS member_count
FROM directory_groups g
WHERE g.connection_id = $1
ORDER BY g.name
LIMIT $2 OFFSET $3;

-- name: CountDirectoryGroups :one
SELECT count(*) FROM directory_groups WHERE connection_id = $1;

-- name: ClearDirectoryGroupMembers :exec
DELETE FROM directory_group_members WHERE group_id = $1;

-- name: AddDirectoryGroupMember :exec
INSERT INTO directory_group_members (group_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: UpsertDirectoryUserLink :exec
INSERT INTO directory_user_links (connection_id, user_id, external_id)
VALUES ($1, $2, $3)
ON CONFLICT (connection_id, user_id)
DO UPDATE SET external_id = EXCLUDED.external_id, synced_at = now();

-- name: GetDirectoryUserLinkByExternalID :one
SELECT * FROM directory_user_links WHERE connection_id = $1 AND external_id = $2;

-- name: ListDirectoryGroupsForUser :many
SELECT g.id, g.connection_id, g.name, c.name AS connection_name
FROM directory_group_members gm
JOIN directory_groups g ON g.id = gm.group_id
JOIN directory_connections c ON c.id = g.connection_id
WHERE gm.user_id = $1
ORDER BY g.name;

-- name: GetDirectoryConnectionByName :one
SELECT * FROM directory_connections WHERE name = $1;

-- name: RemoveUserFromConnectionGroups :exec
-- Clears a user's membership in every group of one connection (used before
-- re-recording the groups a SAML assertion carries).
DELETE FROM directory_group_members gm
USING directory_groups g
WHERE gm.group_id = g.id AND g.connection_id = $1 AND gm.user_id = $2;
