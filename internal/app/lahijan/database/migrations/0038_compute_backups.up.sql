-- 0038_compute_backups: off-host backup targets + per-snapshot backups (WS-25).
--
-- A backup target is an off-host destination for exported snapshots. The
-- admin configures one target per tenant (or several) with the connection
-- details; the backup worker (compute.backup.create) pulls a snapshot from
-- Incus and pushes it to the target. Three kinds are supported out of the
-- box: s3-compatible (any S3 API endpoint — includes SeaweedFS), nfs (a
-- locally-mounted directory; the operator mounts the share), and ssh
-- (rsync over SSH to a remote host). The "kind" discriminates the driver;
-- config_json carries the per-kind non-sensitive config (endpoint URL,
-- bucket name, remote path, host key fingerprint, ...). Sensitive bits
-- (access keys, SSH keys, NFS passwords) live in encrypted_secret_json
-- which is AES-GCM encrypted at write time using the process-wide
-- auth.secrets.encryptionKey envelope.
--
-- A backup row is the per-export record: "this snapshot was pushed to
-- this target at this time, the bytes landed at this remote location".
-- Backups are append-only for audit; a delete marks deleted_at (the
-- remote bytes are removed by the worker before the row flips).
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns.

CREATE TABLE compute_backup_targets (
    id                      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- name is unique within (tenant_id, name). User-facing label.
    name                    TEXT        NOT NULL,
    -- kind is one of "s3", "nfs", "ssh". The Go-side factory switches
    -- on this to build the right BackupTarget driver.
    kind                    TEXT        NOT NULL,
    -- description is an optional free-form note.
    description             TEXT        NOT NULL DEFAULT '',
    -- config_json carries the per-kind non-sensitive config. Schema:
    --   s3:  {endpoint, bucket, region, prefix, force_path_style}
    --   nfs: {mount_point, sub_path}
    --   ssh: {host, port, user, remote_path, key_fingerprint}
    config_json             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- encrypted_secret_json is the AES-GCM envelope for the sensitive
    -- bits (S3 access_key+secret, SSH private key, NFS password). The
    -- envelope is opaque to the DB; the Go side decrypts at use time.
    encrypted_secret_json   BYTEA       NOT NULL DEFAULT '\\x'::bytea,
    -- enabled gates whether new backups can be queued to this target.
    -- Existing backups stay readable when disabled.
    enabled                 BOOLEAN     NOT NULL DEFAULT true,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at              TIMESTAMPTZ
);

CREATE INDEX idx_compute_backup_targets_tenant
    ON compute_backup_targets (tenant_id);
CREATE UNIQUE INDEX uq_compute_backup_targets_tenant_name
    ON compute_backup_targets (tenant_id, name)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE  compute_backup_targets                  IS 'Off-host backup destinations (S3 / NFS / SSH). Per-tenant.';
COMMENT ON COLUMN compute_backup_targets.kind             IS 'Driver kind: "s3", "nfs", or "ssh".';
COMMENT ON COLUMN compute_backup_targets.config_json      IS 'Per-kind non-sensitive config (endpoint, bucket, path, host, ...).';
COMMENT ON COLUMN compute_backup_targets.encrypted_secret_json IS 'AES-GCM envelope for sensitive credentials. Opaque to the DB.';

CREATE TABLE compute_backups (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- snapshot_id anchors the backup to a compute_snapshots row. Loose FK
    -- (no REFERENCES clause) so a deleted snapshot still keeps its
    -- historical backup rows for audit.
    snapshot_id     UUID        NOT NULL,
    -- instance_id mirrors compute_snapshots.instance_id so the UI can list
    -- backups by instance without a join. Denormalised on insert.
    instance_id     UUID        NOT NULL,
    -- target_id is the destination (loose FK to compute_backup_targets).
    target_id       UUID        NOT NULL,
    -- remote_location is the target-relative URI the backup landed at.
    -- For S3: "s3://bucket/key.tar.gz"; for NFS: "/mnt/backups/..."; for
    -- SSH: "host:/path/key.tar.gz".
    remote_location TEXT        NOT NULL DEFAULT '',
    -- size_bytes is the exported backup size (compressed). Filled in
    -- after the upload completes; zero while in-flight.
    size_bytes      BIGINT      NOT NULL DEFAULT 0,
    -- checksum_sha256 is the post-upload checksum the worker computes so
    -- a future restore can verify integrity. Empty until upload completes.
    checksum_sha256 TEXT        NOT NULL DEFAULT '',
    -- status is one of: "pending", "uploading", "completed", "failed".
    -- The backup worker updates this as it progresses.
    status          TEXT        NOT NULL DEFAULT 'pending',
    -- error_message carries the failure detail when status="failed".
    error_message   TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_compute_backups_tenant
    ON compute_backups (tenant_id);
CREATE INDEX idx_compute_backups_snapshot
    ON compute_backups (tenant_id, snapshot_id);
CREATE INDEX idx_compute_backups_instance
    ON compute_backups (tenant_id, instance_id);
CREATE INDEX idx_compute_backups_target
    ON compute_backups (tenant_id, target_id);

COMMENT ON TABLE  compute_backups              IS 'Per-snapshot exported backups. Append-only for audit; soft-deleted on remote delete.';
COMMENT ON COLUMN compute_backups.status       IS 'Worker progress: "pending" | "uploading" | "completed" | "failed".';
COMMENT ON COLUMN compute_backups.remote_location IS 'Target-relative URI the backup landed at.';
