-- 0039_compute_snapshot_policies.down.sql
DROP TABLE IF EXISTS compute_snapshot_policies;
ALTER TABLE compute_snapshots DROP COLUMN IF EXISTS policy_id;
