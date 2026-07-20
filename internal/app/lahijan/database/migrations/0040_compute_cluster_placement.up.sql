-- 0040_compute_cluster_placement: per-instance cluster placement metadata
-- (WS-26).
--
-- The cluster_member column mirrors the Incus daemon's per-instance
-- "Location" field: the cluster member hosting the instance. The column
-- is informational; the daemon's GET /1.0/instances/<n> response is the
-- source of truth and the compute service reconciles this column from
-- the daemon's view on read.
--
-- The column is NULL for single-node deployments (the LocalPlacementDriver
-- does not set a target; the daemon reports an empty Location for a
-- single-node daemon). For cluster deployments the column carries the
-- member's ServerName so the UI can render "where does this instance
-- live" without a daemon round-trip per row.
--
-- Per ADR-0033 the column is intentionally NOT a foreign key into a
-- hypothetical compute_nodes table: cluster membership is volatile
-- (members join + leave), and a stale FK would block reconcile.

ALTER TABLE compute_instances
    ADD COLUMN cluster_member TEXT;

-- Index for "list instances on this member" queries (used by the
-- cluster admin UI + by the evacuate pre-flight that needs to know
-- which instances a member is hosting).
CREATE INDEX idx_compute_instances_cluster_member
    ON compute_instances (tenant_id, cluster_member)
    WHERE deleted_at IS NULL;

COMMENT ON COLUMN compute_instances.cluster_member IS
    'Incus cluster member hosting the instance. NULL = single-node daemon. Reconciled from the daemon on read.';
