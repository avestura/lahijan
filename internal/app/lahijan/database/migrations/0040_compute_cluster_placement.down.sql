-- 0040_compute_cluster_placement.down.sql
DROP INDEX IF EXISTS idx_compute_instances_cluster_member;
ALTER TABLE compute_instances DROP COLUMN IF EXISTS cluster_member;
