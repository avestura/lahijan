-- 0039_compute_snapshot_policies: per-instance + per-tenant schedule policies
-- (WS-25).
--
-- A policy says "take a snapshot of this instance (or every instance in
-- this tenant when instance_id is NULL) every <cadence>, keep the last
-- <retain_count>, and push each snapshot to <target_id> when set". The
-- compute.snapshot.take worker scans for due policies; the
-- compute.snapshot.prune worker enforces retain_count.
--
-- cadence is an ISO 8601 duration ("PT1H", "P1D", "P1W", "P1M"). The Go
-- side parses it via time.ParseDuration after a small ISO-8601 shim; the
-- raw string is stored so the UI can render it verbatim and a future WS
-- can swap the parser without a migration.
--
-- retain_count is the maximum number of snapshots to keep for the policy.
-- The prune worker deletes the oldest excess snapshots that were created
-- BY THIS POLICY (tracked via the snapshot's policy_id, which is added to
-- compute_snapshots in a companion migration step below). Snapshots
-- created manually (or by other policies) are never pruned by this one.
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns.

-- Add the policy_id column to compute_snapshots so the prune worker can
-- attribute each snapshot to the policy that created it. NULL means the
-- snapshot was created manually (POST /instances/{id}/snapshots) and is
-- exempt from automated retention.
ALTER TABLE compute_snapshots
    ADD COLUMN policy_id UUID;

CREATE TABLE compute_snapshot_policies (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- instance_id is NULL for a tenant-default policy (applies to every
    -- instance in the tenant that does not have its own policy). When
    -- non-NULL, it must reference a live compute_instances row within
    -- this tenant; the FK is loose (no REFERENCES clause) so a
    -- soft-deleted instance still anchors its historical policy rows.
    instance_id     UUID,
    name            TEXT        NOT NULL,
    cadence         TEXT        NOT NULL,
    retain_count    INT         NOT NULL DEFAULT 7,
    -- target_id is NULL when the policy only takes snapshots (no off-host
    -- backup). When set, the take worker queues a compute.backup.create
    -- job after each successful snapshot.
    target_id       UUID,
    enabled         BOOLEAN     NOT NULL DEFAULT true,
    -- last_run_at is updated by the take worker after each run, regardless
    -- of success/failure. Used by the operator UI to surface healthy vs
    -- stale schedules.
    last_run_at     TIMESTAMPTZ,
    -- next_run_at is the worker's cue: scan for policies where enabled=true
    -- AND next_run_at <= now(). The take worker computes next_run_at as
    -- last_run_at + cadence after each run.
    next_run_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_compute_snapshot_policies_tenant
    ON compute_snapshot_policies (tenant_id);
CREATE INDEX idx_compute_snapshot_policies_instance
    ON compute_snapshot_policies (tenant_id, instance_id);
CREATE INDEX idx_compute_snapshot_policies_due
    ON compute_snapshot_policies (next_run_at)
    WHERE deleted_at IS NULL AND enabled = true;
CREATE UNIQUE INDEX uq_compute_snapshot_policies_tenant_name
    ON compute_snapshot_policies (tenant_id, name)
    WHERE deleted_at IS NULL;
-- One policy per instance (the per-instance policy wins over the
-- tenant-default). The tenant-default (instance_id IS NULL) is unique
-- per tenant.
CREATE UNIQUE INDEX uq_compute_snapshot_policies_tenant_instance
    ON compute_snapshot_policies (tenant_id, instance_id)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE  compute_snapshot_policies              IS 'Per-instance + per-tenant snapshot schedules.';
COMMENT ON COLUMN compute_snapshot_policies.instance_id  IS 'NULL = tenant-default policy; non-NULL = per-instance policy.';
COMMENT ON COLUMN compute_snapshot_policies.cadence      IS 'ISO 8601 duration (PT1H, P1D, P1W, P1M).';
COMMENT ON COLUMN compute_snapshot_policies.retain_count IS 'Max snapshots to keep for this policy. Prune worker deletes the oldest excess.';
COMMENT ON COLUMN compute_snapshot_policies.target_id    IS 'Optional backup target. NULL = snapshot only.';
COMMENT ON COLUMN compute_snapshot_policies.next_run_at  IS 'When the take worker may next fire this policy. NULL = never run yet.';

-- The policy_id column on compute_snapshots is annotated here so the
-- COMMENT lives next to the policy table that owns the relationship.
COMMENT ON COLUMN compute_snapshots.policy_id IS 'The policy that created this snapshot. NULL = manual (exempt from prune).';
