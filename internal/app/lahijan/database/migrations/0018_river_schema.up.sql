-- 0018_river_schema: installs the schema required by River (the PostgreSQL-
-- native job queue chosen in ADR-0008) so that any module can queue durable
-- work that survives restarts, retries on failure, and is inspectable in the
-- admin UI. River's tables are global (no tenant_id) per the glossary; the
-- `river_*` tables live in the same logical `lahijan` database as everything
-- else (ADR-0007) so that jobs are transactional with the data they touch.
--
-- This migration bundles the seven upstream River migrations
-- (riverpgxv5/migration/main/001..007 for river v0.40.x) into ONE Lahijan
-- migration because:
--
--   1. River's own migration system (rivermigrate.Migrator) is forward-only
--      within a major version and tracks state in `river_migration`. We use
--      golang-migrate (paired up/down, reversible — ADR-0003) which has its
--      own `schema_migrations` table. Bundling avoids 7 new entries in our
--      migrations dir for what is conceptually "the River schema".
--   2. The downstream migration is fully reversible: we drop every River
--      object in the correct order, including the `river_migration` rows we
--      inserted on the way up. (Reversibility here means "the schema is gone";
--      River does not promise to preserve user data on a downgrade.)
--   3. We INSERT the seven (line, version) rows into river_migration so a
--      future operator running `river migrate-up` from the CLI sees the
--      current state and does not try to re-apply upstream migrations.
--
-- When upgrading River in the future (>= v0.41), add a NEW migration that
-- applies only the delta (e.g. 0023_river_schema_008) and INSERTs the new
-- version row(s) into river_migration.

-- ===========================================================================
-- River upstream migration 001: create river_migration table.
-- ===========================================================================

CREATE TABLE river_migration(
  id bigserial PRIMARY KEY,
  created_at timestamptz NOT NULL DEFAULT NOW(),
  version bigint NOT NULL,
  CONSTRAINT version CHECK (version >= 1)
);

CREATE UNIQUE INDEX ON river_migration USING btree(version);

-- ===========================================================================
-- River upstream migration 002: initial schema.
-- ===========================================================================

CREATE TYPE river_job_state AS ENUM(
  'available',
  'cancelled',
  'completed',
  'discarded',
  'retryable',
  'running',
  'scheduled'
);

CREATE TABLE river_job(
  -- 8 bytes
  id bigserial PRIMARY KEY,

  -- 8 bytes (4 bytes + 2 bytes + 2 bytes)
  --
  -- `state` is kept near the top of the table for operator convenience -- when
  -- looking at jobs with `SELECT *` it'll appear first after ID. The other two
  -- fields aren't as important but are kept adjacent to `state` for alignment
  -- to get an 8-byte block.
  state river_job_state NOT NULL DEFAULT 'available',
  attempt smallint NOT NULL DEFAULT 0,
  max_attempts smallint NOT NULL,

  -- 8 bytes each (no alignment needed)
  attempted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT NOW(),
  finalized_at timestamptz,
  scheduled_at timestamptz NOT NULL DEFAULT NOW(),

  -- 2 bytes (some wasted padding probably)
  priority smallint NOT NULL DEFAULT 1,

  -- types stored out-of-band
  args jsonb,
  attempted_by text[],
  errors jsonb[],
  kind text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}',
  queue text NOT NULL DEFAULT 'default',
  tags varchar(255)[],

  CONSTRAINT finalized_or_finalized_at_null CHECK ((state IN ('cancelled', 'completed', 'discarded') AND finalized_at IS NOT NULL) OR finalized_at IS NULL),
  CONSTRAINT max_attempts_is_positive CHECK (max_attempts > 0),
  CONSTRAINT priority_in_range CHECK (priority >= 1 AND priority <= 4),
  CONSTRAINT queue_length CHECK (char_length(queue) > 0 AND char_length(queue) < 128),
  CONSTRAINT kind_length CHECK (char_length(kind) > 0 AND char_length(kind) < 128)
);

CREATE INDEX river_job_kind ON river_job USING btree(kind);

CREATE INDEX river_job_state_and_finalized_at_index ON river_job USING btree(state, finalized_at) WHERE finalized_at IS NOT NULL;

CREATE INDEX river_job_prioritized_fetching_index ON river_job USING btree(state, queue, priority, scheduled_at, id);

CREATE INDEX river_job_args_index ON river_job USING GIN(args);

CREATE INDEX river_job_metadata_index ON river_job USING GIN(metadata);

CREATE OR REPLACE FUNCTION river_job_notify()
  RETURNS TRIGGER
  AS $$
DECLARE
  payload json;
BEGIN
  IF NEW.state = 'available' THEN
    -- Notify will coalesce duplicate notifications within a transaction, so
    -- keep these payloads generalized:
    payload = json_build_object('queue', NEW.queue);
    PERFORM
      pg_notify('river_insert', payload::text);
  END IF;
  RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER river_notify
  AFTER INSERT ON river_job
  FOR EACH ROW
  EXECUTE PROCEDURE river_job_notify();

CREATE UNLOGGED TABLE river_leader(
    -- 8 bytes each (no alignment needed)
    elected_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,

    -- types stored out-of-band
    leader_id text NOT NULL,
    name text PRIMARY KEY,

    CONSTRAINT name_length CHECK (char_length(name) > 0 AND char_length(name) < 128),
    CONSTRAINT leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

-- ===========================================================================
-- River upstream migration 003: tags non-null.
-- ===========================================================================

ALTER TABLE river_job ALTER COLUMN tags SET DEFAULT '{}';
UPDATE river_job SET tags = '{}' WHERE tags IS NULL;
ALTER TABLE river_job ALTER COLUMN tags SET NOT NULL;

-- ===========================================================================
-- River upstream migration 004: pending state, args/metadata NOT NULL, river_queue.
-- ===========================================================================

ALTER TABLE river_job ALTER COLUMN args SET DEFAULT '{}';
UPDATE river_job SET args = '{}' WHERE args IS NULL;
ALTER TABLE river_job ALTER COLUMN args SET NOT NULL;
ALTER TABLE river_job ALTER COLUMN args DROP DEFAULT;

ALTER TABLE river_job ALTER COLUMN metadata SET DEFAULT '{}';
UPDATE river_job SET metadata = '{}' WHERE metadata IS NULL;
ALTER TABLE river_job ALTER COLUMN metadata SET NOT NULL;

ALTER TYPE river_job_state ADD VALUE IF NOT EXISTS 'pending' AFTER 'discarded';

ALTER TABLE river_job DROP CONSTRAINT finalized_or_finalized_at_null;
ALTER TABLE river_job ADD CONSTRAINT finalized_or_finalized_at_null CHECK (
    (finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded')) OR
    (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded'))
);

DROP TRIGGER river_notify ON river_job;
DROP FUNCTION river_job_notify;

CREATE TABLE river_queue (
    name text PRIMARY KEY NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}' ::jsonb,
    paused_at timestamptz,
    updated_at timestamptz NOT NULL
);

ALTER TABLE river_leader
    ALTER COLUMN name SET DEFAULT 'default',
    DROP CONSTRAINT name_length,
    ADD CONSTRAINT name_length CHECK (name = 'default');

-- ===========================================================================
-- River upstream migration 005: unique_key, rebuilt river_migration, client tbl.
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
