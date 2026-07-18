-- 0018_river_schema_base: installs the BASE schema required by River (the
-- PostgreSQL-native job queue chosen in ADR-0008). This is migration 1 of 2
-- for the WS-09 River integration:
--
--   * 0018 (this file)  -> upstream River migrations 001..003 + the
--                          non-enum-affecting parts of 004 + the
--                          ALTER TYPE ADD VALUE 'pending' as the LAST
--                          statement of this migration.
--   * 0019              -> the constraint changes from upstream 004 (which
--                          "use" the new 'pending' value) + upstream 005,
--                          006, 007 + INSERT INTO river_migration.
--
-- Why split? Postgres forbids referencing a freshly-added enum value within
-- the same transaction (the `ALTER TYPE ... ADD VALUE` is only visible
-- after COMMIT). golang-migrate applies each migration file in its own
-- transaction, so splitting here lets the new value land before any
-- subsequent statement references it. River's own `rivermigrate` framework
-- applies each upstream migration separately; we get the same property by
-- splitting our bundled migration at the enum boundary.
--
-- When upgrading River in the future (>= v0.41), add a NEW migration that
-- applies only the delta.

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
    elected_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
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
-- River upstream migration 004 — FIRST HALF: everything that does NOT
-- reference the new 'pending' enum value. The ALTER TYPE ADD VALUE itself
-- is intentionally the LAST statement of this migration so the new value
-- commits before the next migration's CHECK constraint change tries to use
-- it. (See the file header for the rationale.)
-- ===========================================================================

-- The args column never had a NOT NULL constraint or default value at the
-- database level, though we tried to ensure one at the application level.
ALTER TABLE river_job ALTER COLUMN args SET DEFAULT '{}';
UPDATE river_job SET args = '{}' WHERE args IS NULL;
ALTER TABLE river_job ALTER COLUMN args SET NOT NULL;
ALTER TABLE river_job ALTER COLUMN args DROP DEFAULT;

-- The metadata column never had a NOT NULL constraint or default value at
-- the database level, though we tried to ensure one at the application level.
ALTER TABLE river_job ALTER COLUMN metadata SET DEFAULT '{}';
UPDATE river_job SET metadata = '{}' WHERE metadata IS NULL;
ALTER TABLE river_job ALTER COLUMN metadata SET NOT NULL;

-- Drop the trigger + function early; upstream 004 (then later 007) replaces
-- them, and keeping them around means the new 'pending' value cannot be
-- inserted via the trigger's path either.
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

-- Add the new 'pending' value LAST so it commits with this migration's
-- transaction and is usable in the next migration's CHECK constraint.
ALTER TYPE river_job_state ADD VALUE IF NOT EXISTS 'pending' AFTER 'discarded';
