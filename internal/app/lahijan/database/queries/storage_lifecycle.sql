-- Storage lifecycle rules: tenant-scoped per-bucket S3 lifecycle rules
-- (WS-29, ADR-0036). Every query is tenant-scoped via WithTenant
-- (database/tenant.go) so a cross-tenant bucket_id surfaces as
-- ErrNoRows, never as the row itself.

-- name: CreateStorageLifecycleRule :one
--: tenant-scoped
INSERT INTO storage_lifecycle_rules (
    tenant_id, bucket_id, rule_id, status, action,
    days, date_at, storage_class, prefix
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetStorageLifecycleRule :one
--: tenant-scoped
-- Lookup by (bucket_id, rule_id) — the natural key the storage service
-- uses on every privileged call (UpdateRule / DeleteRule).
SELECT * FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND bucket_id = $2 AND rule_id = $3;

-- name: GetStorageLifecycleRuleByID :one
--: tenant-scoped
SELECT * FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND id = $2;

-- name: ListStorageLifecycleRules :many
--: tenant-scoped
-- Lists every rule attached to the given bucket within the tenant in
-- ctx. Ordered by rule_id for deterministic UI rendering.
SELECT * FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND bucket_id = $2
ORDER BY rule_id ASC
LIMIT $3 OFFSET $4;

-- name: CountStorageLifecycleRules :one
--: tenant-scoped
SELECT count(*) FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND bucket_id = $2;

-- name: ListEnabledStorageLifecycleRules :many
--: tenant-scoped
-- Lists every ENABLED rule across the tenant. Used by the lifecycle
-- evaluator River worker on every tick so it can act on due rules in a
-- single query per tenant.
SELECT * FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND status = 'enabled'
ORDER BY bucket_id ASC, rule_id ASC;

-- name: UpdateStorageLifecycleRule :exec
--: tenant-scoped
-- Replaces the mutable fields of a rule. The rule_id (natural key) is
-- immutable; the bucket_id is immutable (a rule cannot hop buckets).
UPDATE storage_lifecycle_rules
SET status = $3,
    days = $4,
    date_at = $5,
    storage_class = $6,
    prefix = $7,
    updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: SetStorageLifecycleRuleStatus :exec
--: tenant-scoped
-- Convenience update for the enable/disable toggle without rewriting
-- the rest of the rule. Used by the service layer's Enable/DisableRule
-- shortcuts so the audit row metadata can show only the changed field.
UPDATE storage_lifecycle_rules
SET status = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteStorageLifecycleRule :exec
--: tenant-scoped
DELETE FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND id = $2;

-- name: DeleteAllStorageLifecycleRulesForBucket :exec
--: tenant-scoped
-- Bulk-delete used by the service layer when a user replaces the whole
-- policy via PUT /lifecycle (the new policy is then written rule by
-- rule in the same transaction).
DELETE FROM storage_lifecycle_rules
WHERE tenant_id = $1 AND bucket_id = $2;
