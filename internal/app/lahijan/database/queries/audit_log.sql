-- Audit log: append-only. tenant_id is nullable for system-level events.
-- The audit_log_block_mutation trigger (migration 0005) rejects UPDATE/DELETE,
-- so this query file intentionally exposes only INSERT and SELECT.
-- Optional fields use explicit params; the repository wrapper supplies defaults.

-- name: CreateAuditLog :one
INSERT INTO audit_log (
    tenant_id,
    actor_user_id,
    actor_type,
    action,
    resource_type,
    resource_id,
    status,
    request_id,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetAuditLog :one
SELECT * FROM audit_log WHERE id = $1;

-- name: ListAuditLogForTenant :many
--: tenant-scoped
SELECT * FROM audit_log
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountAuditLogForTenant :one
--: tenant-scoped
SELECT count(*) FROM audit_log WHERE tenant_id = $1;

-- name: ListAuditLogGlobal :many
--: admin-only; system-wide query, not tenant-scoped
SELECT * FROM audit_log
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;
