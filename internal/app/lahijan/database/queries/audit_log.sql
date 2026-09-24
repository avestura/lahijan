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

-- name: GetAuditLogForTenant :one
--: tenant-scoped; single-row read for the GET /audit/{id} handler. Returns the
--: row if it belongs to the tenant in ctx, OR is a system-level event (NULL
--: tenant). System events are visible from any tenant so operators can trace
--: auth flows even when scoped.
SELECT * FROM audit_log
WHERE id = $2
  AND (tenant_id = $1 OR tenant_id IS NULL);

-- name: ListAuditLogForTenantFiltered :many
--: tenant-scoped; filtered + paginated read for GET /audit.
-- Each filter is NULL-able: a NULL means "do not filter on this column".
-- sqlc.narg declares a nullable parameter; the ::type cast tells sqlc the
-- concrete Go type to emit (*uuid.UUID, *string, *time.Time).
SELECT * FROM audit_log
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('actor_user_id')::uuid IS NULL OR actor_user_id = sqlc.narg('actor_user_id')::uuid)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('resource_id')::uuid IS NULL OR resource_id = sqlc.narg('resource_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts')::timestamptz)
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR created_at <= sqlc.narg('to_ts')::timestamptz)
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAuditLogForTenantFiltered :one
--: tenant-scoped; same filters as ListAuditLogForTenantFiltered, for pagination.
SELECT count(*) FROM audit_log
WHERE tenant_id = @tenant_id
  AND (sqlc.narg('actor_user_id')::uuid IS NULL OR actor_user_id = sqlc.narg('actor_user_id')::uuid)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('resource_id')::uuid IS NULL OR resource_id = sqlc.narg('resource_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts')::timestamptz)
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR created_at <= sqlc.narg('to_ts')::timestamptz);

-- name: ListAuditLogGlobalFiltered :many
--: admin-only; same shape as ListAuditLogForTenantFiltered but unscoped. Used
--: by the global audit export endpoint behind RequirePerm("audit.read_global").
SELECT * FROM audit_log
WHERE (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')::uuid)
  AND (sqlc.narg('actor_user_id')::uuid IS NULL OR actor_user_id = sqlc.narg('actor_user_id')::uuid)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts')::timestamptz)
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR created_at <= sqlc.narg('to_ts')::timestamptz)
ORDER BY created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAuditLogGlobalFiltered :one
--: admin-only; pagination counterpart to ListAuditLogGlobalFiltered.
SELECT count(*) FROM audit_log
WHERE (sqlc.narg('tenant_id')::uuid IS NULL OR tenant_id = sqlc.narg('tenant_id')::uuid)
  AND (sqlc.narg('actor_user_id')::uuid IS NULL OR actor_user_id = sqlc.narg('actor_user_id')::uuid)
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action')::text)
  AND (sqlc.narg('resource_type')::text IS NULL OR resource_type = sqlc.narg('resource_type')::text)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts')::timestamptz)
  AND (sqlc.narg('to_ts')::timestamptz IS NULL OR created_at <= sqlc.narg('to_ts')::timestamptz);

-- ===========================================================================
-- audit_log_outcomes: append-only outcome trail (WS-08 MarkOutcome pattern).
-- Same append-only contract as audit_log; the trigger on this table rejects
-- UPDATE and DELETE.
-- ===========================================================================

-- name: CreateAuditLogOutcome :one
INSERT INTO audit_log_outcomes (audit_id, status, details)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListAuditLogOutcomes :many
--: newest-first so the caller can pick the latest as the current status.
SELECT * FROM audit_log_outcomes
WHERE audit_id = $1
ORDER BY created_at DESC;


