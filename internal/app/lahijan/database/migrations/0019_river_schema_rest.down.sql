-- Reverses 0019_river_schema_rest: rolls back upstream migrations 007, 006,
-- 005, and the constraint change from 004. Does NOT roll back the 'pending'
-- enum value because Postgres does not support removing enum values; that
-- value is dropped along with the river_job_state TYPE in 0018's down.

-- Upstream 007 down.
DROP TABLE IF EXISTS river_notification;

ALTER TABLE river_job
    ALTER COLUMN max_attempts DROP DEFAULT;

ALTER TABLE river_queue
    ALTER COLUMN updated_at DROP DEFAULT;

-- Upstream 006 down.
DROP INDEX IF EXISTS river_job_unique_idx;

ALTER TABLE river_job
    DROP COLUMN IF EXISTS unique_states;

CREATE UNIQUE INDEX IF NOT EXISTS river_job_kind_unique_key_idx ON river_job (kind, unique_key) WHERE unique_key IS NOT NULL;

DROP FUNCTION IF EXISTS river_job_state_in_bitmask;

-- Upstream 005 down (river_client tables never created in our bundle).
ALTER TABLE river_job
    DROP COLUMN IF EXISTS unique_key;

-- The river_migration rebuild from upstream 005: revert to (version) only.
-- We don't bother preserving non-main lines (none exist in our setup).
DO
$body$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'river_migration') THEN
        ALTER TABLE river_migration RENAME TO river_migration_old;

        CREATE TABLE river_migration(
            id bigserial PRIMARY KEY,
            created_at timestamptz NOT NULL DEFAULT NOW(),
            version bigint NOT NULL,
            CONSTRAINT version CHECK (version >= 1)
        );

        CREATE UNIQUE INDEX ON river_migration USING btree(version);

        INSERT INTO river_migration
            (created_at, version)
        SELECT created_at, version
        FROM river_migration_old
        WHERE line = 'main';

        DROP TABLE river_migration_old;
    END IF;
END;
$body$
LANGUAGE 'plpgsql';

-- Upstream 004 SECOND-HALF down: restore the original CHECK constraint that
-- did not consider 'pending'.
ALTER TABLE river_job DROP CONSTRAINT IF EXISTS finalized_or_finalized_at_null;
ALTER TABLE river_job ADD CONSTRAINT finalized_or_finalized_at_null CHECK (
    (state IN ('cancelled', 'completed', 'discarded') AND finalized_at IS NOT NULL) OR finalized_at IS NULL
);
