-- 0019_river_schema_rest: completes the River schema by applying the
-- constraint change from upstream 004 (now that the 'pending' enum value
-- has committed in 0018), plus upstream 005 (rebuilt river_migration +
-- river_job.unique_key), 006 (bulk unique), and 007 (notification outbox +
-- SQL cleanup). Finally marks every bundled upstream version as applied in
-- river_migration so a future `river migrate-up` from the CLI sees the
-- canonical state.
--
-- See 0018_river_schema_base.up.sql for the rationale behind the split.

-- ===========================================================================
-- River upstream migration 004 — SECOND HALF: constraint change that uses
-- the new 'pending' value (now committed by 0018).
-- ===========================================================================

ALTER TABLE river_job DROP CONSTRAINT finalized_or_finalized_at_null;
ALTER TABLE river_job ADD CONSTRAINT finalized_or_finalized_at_null CHECK (
    (finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded')) OR
    (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded'))
);

-- ===========================================================================
-- River upstream migration 005: rebuilt river_migration + unique_key + the
-- (later-dropped) river_client tables.
-- ===========================================================================

-- Rebuild river_migration to be keyed on (line, version) instead of just
-- (version). The original schema only had one row (version=1) so the data
-- migration is trivial; the DO block keeps it idempotent.
DO
$body$
BEGIN
    ALTER TABLE river_migration RENAME TO river_migration_old;

    CREATE TABLE river_migration(
        line TEXT NOT NULL,
        version bigint NOT NULL,
        created_at timestamptz NOT NULL DEFAULT NOW(),
        CONSTRAINT line_length CHECK (char_length(line) > 0 AND char_length(line) < 128),
        CONSTRAINT version_gte_1 CHECK (version >= 1),
        PRIMARY KEY (line, version)
    );

    INSERT INTO river_migration
        (created_at, line, version)
    SELECT created_at, 'main', version
    FROM river_migration_old;

    DROP TABLE river_migration_old;
END;
$body$
LANGUAGE 'plpgsql';

ALTER TABLE river_job
    ADD COLUMN IF NOT EXISTS unique_key bytea;

CREATE UNIQUE INDEX IF NOT EXISTS river_job_kind_unique_key_idx ON river_job (kind, unique_key) WHERE unique_key IS NOT NULL;

-- `river_client` and `river_client_queue` were added by upstream 005 and
-- dropped by upstream 007. We skip creating them here because they would be
-- dropped again just below; the net effect of upstream 001..007 is "no
-- river_client table".

-- ===========================================================================
-- River upstream migration 006: bulk unique.
-- ===========================================================================

CREATE OR REPLACE FUNCTION river_job_state_in_bitmask(bitmask BIT(8), state river_job_state)
RETURNS boolean
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE state
        WHEN 'available' THEN get_bit(bitmask, 7)
        WHEN 'cancelled' THEN get_bit(bitmask, 6)
        WHEN 'completed' THEN get_bit(bitmask, 5)
        WHEN 'discarded' THEN get_bit(bitmask, 4)
        WHEN 'pending'   THEN get_bit(bitmask, 3)
        WHEN 'retryable' THEN get_bit(bitmask, 2)
        WHEN 'running'   THEN get_bit(bitmask, 1)
        WHEN 'scheduled' THEN get_bit(bitmask, 0)
        ELSE 0
    END = 1;
$$;

ALTER TABLE river_job ADD COLUMN IF NOT EXISTS unique_states BIT(8);

CREATE UNIQUE INDEX IF NOT EXISTS river_job_unique_idx ON river_job (unique_key)
    WHERE unique_key IS NOT NULL
      AND unique_states IS NOT NULL
      AND river_job_state_in_bitmask(unique_states, state);

DROP INDEX river_job_kind_unique_key_idx;

-- ===========================================================================
-- River upstream migration 007: notification outbox + SQL cleanup.
-- ===========================================================================

CREATE TABLE river_notification (
    id bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    payload text NOT NULL,
    topic text NOT NULL,
    CONSTRAINT topic_length CHECK (length(topic) > 0 AND length(topic) < 128)
);

CREATE INDEX river_notification_created_at_idx ON river_notification (created_at);
CREATE INDEX river_notification_topic_id_idx ON river_notification (topic, id);

ALTER TABLE river_job
    ALTER COLUMN max_attempts SET DEFAULT 25;

ALTER TABLE river_queue
    ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;

-- ===========================================================================
-- Mark every bundled upstream migration as applied in river_migration so a
-- future `river migrate-up` from the CLI sees the canonical state and does
-- not re-apply. We use ON CONFLICT so the migration is idempotent against a
-- partial prior run (e.g. a manually half-applied state).
-- ===========================================================================

INSERT INTO river_migration (line, version) VALUES
    ('main', 1),
    ('main', 2),
    ('main', 3),
    ('main', 4),
    ('main', 5),
    ('main', 6),
    ('main', 7)
ON CONFLICT (line, version) DO NOTHING;

COMMENT ON TABLE  river_job            IS 'River durable job queue. Owned by github.com/riverqueue/river; treat as external.';
COMMENT ON TABLE  river_queue          IS 'River queue config (paused, metadata). Owned by river.';
COMMENT ON TABLE  river_leader         IS 'River leader-election table (unlogged). Owned by river.';
COMMENT ON TABLE  river_migration      IS 'River internal migration versioning (separate from schema_migrations). Owned by river.';
COMMENT ON TABLE  river_notification   IS 'River LISTEN/NOTIFY outbox. Owned by river.';
