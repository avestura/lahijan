-- Reverses 0018_river_schema_base: drops the schema installed by the first
-- River migration. 0019_river_schema_rest must be rolled back first.
--
-- The 'pending' enum value added at the end of 0018 cannot be removed from
-- the river_job_state type (Postgres does not support removing enum values
-- without dropping + recreating the type); we instead drop the whole TYPE
-- here, which is only safe once every column / constraint that referenced
-- it is gone. river_job is the only such column, so the order below is
-- correct.

DROP TABLE IF EXISTS river_queue;
DROP TRIGGER IF EXISTS river_notify ON river_job;
DROP FUNCTION IF EXISTS river_job_notify;

DROP INDEX IF EXISTS river_job_args_index;
DROP INDEX IF EXISTS river_job_metadata_index;
DROP INDEX IF EXISTS river_job_prioritized_fetching_index;
DROP INDEX IF EXISTS river_job_state_and_finalized_at_index;
DROP INDEX IF EXISTS river_job_kind;
DROP TABLE IF EXISTS river_job;

DROP TYPE IF EXISTS river_job_state;

DROP TABLE IF EXISTS river_leader;

-- Drop last so a partial roll-forward that left river_migration in place
-- can still roll back.
DROP TABLE IF EXISTS river_migration;
