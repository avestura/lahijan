-- 0027_compute_instances: per-tenant compute instances (WS-14).
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns. compute_instances is the canonical Lahijan-side record of
-- an Incus instance: the row is created before the Incus CreateInstance call,
-- updated as the lifecycle progresses (start/stop/restart/freeze), and
-- soft-deleted on instance delete (deleted_at) so historical audit + billing
-- records remain joinable.
--
-- The row NEVER holds the live Incus state (the only source of truth for
-- that is the daemon); instead it caches the last-known status + the
-- configuration the user asked for. The compute service reconciles on read.
--
-- project_name is the Incus project the instance lives in; it is derived
-- from tenant_id via the incus.ProjectName(tenant) helper so a row never
-- leaks across tenants (the project is the isolation boundary ADR-0010).
--
-- config_json stores the user-supplied config + devices + profiles snapshot
-- at create time. JSONB because the shape mirrors Incus' own (free-form
-- map[string]string + map[string]map[string]string) — locking it into typed
-- columns would force a migration every time Incus adds a knob.

CREATE TABLE compute_instances (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- Incus project name; derived from tenant_id at create time.
    project_name    TEXT        NOT NULL,
    -- Instance name; unique within (tenant_id, name).
    name            TEXT        NOT NULL,
    -- "container" or "virtual-machine". Empty means container (Incus default).
    type            TEXT        NOT NULL DEFAULT 'container',
    -- Cached last-known Incus status string ("Running", "Stopped", ...).
    status          TEXT        NOT NULL DEFAULT 'stopped',
    -- Cached numeric Incus status_code (Running=103, Stopped=102, ...).
    status_code     INT         NOT NULL DEFAULT 102,
    -- The image alias the instance was created from ("ubuntu/24.04", ...).
    image_alias     TEXT        NOT NULL DEFAULT '',
    -- The image fingerprint once resolved (filled in at create success).
    image_fingerprint TEXT      NOT NULL DEFAULT '',
    -- Comma-separated profile names applied to the instance ("default,...").
    -- Kept denormalised for fast UI rendering; the authoritative shape lives
    -- in config_json.
    profiles        TEXT[]      NOT NULL DEFAULT '{}',
    -- Free-form config snapshot mirroring Incus' shape:
    --   {config: {...}, devices: {...}, profiles: [...]}
    config_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Optional user-visible description.
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- Hot lookups: per-tenant list, status-by-tenant, (tenant, name) uniqueness.
CREATE INDEX idx_compute_instances_tenant_id
    ON compute_instances (tenant_id);
CREATE INDEX idx_compute_instances_tenant_status
    ON compute_instances (tenant_id, status);
CREATE UNIQUE INDEX uq_compute_instances_tenant_name
    ON compute_instances (tenant_id, name)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE  compute_instances              IS 'Per-tenant compute instances. Mirrors Incus state; soft-deleted on instance delete.';
COMMENT ON COLUMN compute_instances.project_name IS 'Incus project name; derived from tenant_id at create time.';
COMMENT ON COLUMN compute_instances.status       IS 'Cached Incus status string. Reconciled on read; the daemon is the source of truth.';
COMMENT ON COLUMN compute_instances.config_json  IS 'Snapshot of user-supplied config + devices + profiles at create time.';
COMMENT ON COLUMN compute_instances.profiles     IS 'Profile names applied to the instance (denormalised from config_json for fast UI).';

-- Status codes mirror Incus' Operation/Instance status constants. A CHECK
-- would lock us to the current Incus enum; we keep it loose so upgrades
-- don't require a migration. The Go-side type switch is authoritative.
