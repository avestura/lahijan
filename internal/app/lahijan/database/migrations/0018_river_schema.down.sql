-- Reverses 0018_river_schema: drops every River-owned object in dependency
-- order and removes the rows we inserted into river_migration on the way up.
-- River's tables are global (no tenant_id) so there is no tenant scoping to
-- preserve. Reversibility here means "the schema is gone"; River does not
-- promise to preserve queued jobs across a downgrade.

DROP TABLE IF EXISTS river_notification;
DROP TABLE IF EXISTS river_queue;

DROP INDEX IF EXISTS river_job_unique_idx;
DROP FUNCTION IF EXISTS river_job_state_in_bitmask;

DROP INDEX IF EXISTS river_job_args_index;
DROP INDEX IF EXISTS river_job_metadata_index;
DROP INDEX IF EXISTS river_job_prioritized_fetching_index;
DROP INDEX IF EXISTS river_job_state_and_finalized_at_index;
DROP INDEX IF EXISTS river_job_kind;
DROP TABLE IF EXISTS river_job;

DROP TYPE IF EXISTS river_job_state;

DROP TABLE IF EXISTS river_leader;

-- Drop last so a partial roll-forward that left river_migration in place can
-- still roll back. The table itself was created in upstream 001 and rebuilt
-- in upstream 005; we just drop it.
DROP TABLE IF EXISTS river_migration;
