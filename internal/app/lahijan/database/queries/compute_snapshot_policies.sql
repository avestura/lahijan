-- compute_snapshot_policies (WS-25): tenant-scoped snapshot schedules.
-- Every query here filters by tenant_id (set by WithTenant at the repo seam).

-- name: CreateComputeSnapshotPolicy :one
--: tenant-scoped
INSERT INTO compute_snapshot_policies (
    tenant_id,
    instance_id,
    name,
    cadence,
    retain_count,
    target_id,
    enabled,
    next_run_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetComputeSnapshotPolicyByID :one
--: tenant-scoped
SELECT * FROM compute_snapshot_policies
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetComputeSnapshotPolicyByName :one
--: tenant-scoped
SELECT * FROM compute_snapshot_policies
WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL;

-- name: ListComputeSnapshotPolicies :many
--: tenant-scoped
SELECT * FROM compute_snapshot_policies
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListComputeSnapshotPoliciesByInstance :many
--: tenant-scoped; returns the per-instance policy (if any) + the tenant-default.
SELECT * FROM compute_snapshot_policies
WHERE tenant_id = $1 AND deleted_at IS NULL
  AND (instance_id = $2 OR instance_id IS NULL)
ORDER BY instance_id NULLS LAST
LIMIT $3 OFFSET $4;

-- name: CountComputeSnapshotPolicies :one
--: tenant-scoped
SELECT count(*) FROM compute_snapshot_policies
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- name: ListDueComputeSnapshotPolicies :many
--: cross-tenant; the take worker scans the whole table for due rows.
--: Tenant scoping is enforced at the worker level (WithTenant is set per
--: row before any DB write). Returns at most $1 rows so the worker
--: processes in bounded batches.
SELECT * FROM compute_snapshot_policies
WHERE deleted_at IS NULL AND enabled = true
  AND next_run_at IS NOT NULL
  AND next_run_at <= $2
ORDER BY next_run_at ASC
LIMIT $1;

-- name: UpdateComputeSnapshotPolicy :exec
--: tenant-scoped; replaces the user-editable fields. The cadence change
--: recomputes next_run_at via MarkComputeSnapshotPolicyDue when the
--: caller wants the new cadence to take effect immediately.
UPDATE compute_snapshot_policies
SET instance_id = $3, name = $4, cadence = $5, retain_count = $6,
    target_id = $7, enabled = $8, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: MarkComputeSnapshotPolicyRun :exec
--: tenant-scoped; records that the policy just ran at $3 and schedules the
--: next run at $3 + cadence (the caller computes the next_run_at). Used by
--: the take worker after every run, success or failure.
UPDATE compute_snapshot_policies
SET last_run_at = $3, next_run_at = $4, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: SoftDeleteComputeSnapshotPolicy :exec
--: tenant-scoped; marks the row deleted_at=now() + disabled so the worker
--: stops picking it up. Existing snapshots created by this policy stay
--: (their policy_id still points at the row) but are no longer pruned by it.
UPDATE compute_snapshot_policies
SET deleted_at = now(), enabled = false, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
