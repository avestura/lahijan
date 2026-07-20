-- 0037_compute_snapshots: per-tenant compute instance snapshots (WS-25).
--
-- A snapshot is a point-in-time copy of an instance's filesystem (and
-- optionally its runtime state, when stateful=true). Incus hosts the
-- actual snapshot bytes; this table is the Lahijan-side record that lets
-- the API list/restore/delete snapshots and lets the schedule + prune
-- workers (compute.snapshot.take / compute.snapshot.prune) iterate due
-- work. The row is created BEFORE the Incus CreateSnapshot call so a
-- daemon timeout still leaves the tenant able to retry.
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns. The row is soft-deleted (deleted_at) when the snapshot
-- is deleted on the daemon side so historical audit + billing joins stay
-- valid.

CREATE TABLE compute_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- The instance this snapshot belongs to. Kept as a loose FK (no
    -- REFERENCES clause) so a soft-deleted instance row still anchors
    -- its historical snapshots; the join is by (tenant_id, instance_id).
    instance_id     UUID        NOT NULL,
    -- Snapshot name; unique within (tenant_id, instance_id, name).
    -- Incus scopes snapshot names per instance; Lahijan mirrors that.
    name            TEXT        NOT NULL,
    -- stateful mirrors Incus' stateful flag (whether runtime state was
    -- captured alongside the filesystem). Cached at create time.
    stateful        BOOLEAN     NOT NULL DEFAULT false,
    -- size_bytes is filled in after the daemon reports it. Zero until
    -- then; the UI shows "—" while unknown.
    size_bytes      BIGINT      NOT NULL DEFAULT 0,
    -- expires_at is set when the snapshot is created by a schedule
    -- policy with retention. NULL means "no expiry; keep until deleted".
    -- The compute.snapshot.prune worker reads this column to find work.
    expires_at      TIMESTAMPTZ,
    -- Optional human-readable description (free-form).
    description     TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- Hot lookups: per-tenant list, per-instance list, expiry scan,
-- (tenant, instance, name) uniqueness among live rows.
CREATE INDEX idx_compute_snapshots_tenant_id
    ON compute_snapshots (tenant_id);
CREATE INDEX idx_compute_snapshots_instance
    ON compute_snapshots (tenant_id, instance_id);
CREATE INDEX idx_compute_snapshots_expires_at
    ON compute_snapshots (expires_at)
    WHERE deleted_at IS NULL AND expires_at IS NOT NULL;
CREATE UNIQUE INDEX uq_compute_snapshots_tenant_instance_name
    ON compute_snapshots (tenant_id, instance_id, name)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE  compute_snapshots                IS 'Per-tenant compute instance snapshots. Mirrors Incus state; soft-deleted on snapshot delete.';
COMMENT ON COLUMN compute_snapshots.instance_id    IS 'The compute_instances.id this snapshot belongs to (loose FK).';
COMMENT ON COLUMN compute_snapshots.stateful       IS 'Whether runtime state was captured (Incus stateful=true).';
COMMENT ON COLUMN compute_snapshots.size_bytes     IS 'Snapshot size in bytes (filled in after daemon reports it).';
COMMENT ON COLUMN compute_snapshots.expires_at     IS 'When the prune worker may delete this snapshot. NULL = keep until manually deleted.';
