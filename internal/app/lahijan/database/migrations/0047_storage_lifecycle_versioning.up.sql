-- 0047_storage_lifecycle_versioning: per-bucket versioning + lifecycle +
-- object-lock configuration (WS-29, ADR-0036).
--
-- This migration extends the storage surface from WS-13/WS-16 (bucket CRUD
-- + presign + quotas) up to S3 parity:
--
--   * per-bucket versioning toggle (unversioned / enabled / suspended)
--   * per-bucket object-lock policy (default retention mode + days)
--   * per-bucket lifecycle rules (expiration, NVP expiration, abort
--     incomplete multipart, transition)
--
-- Per ADR-0036 the Lahijan side is the source of truth for the policy
-- surface. The SeaweedFS driver pushes the policy via the AWS SDK v2 S3
-- client (PutBucketVersioning / PutBucketLifecycleConfiguration /
-- PutObjectLockConfiguration) on every Set call; the lifecycle evaluator
-- River worker (storage.lifecycle.evaluate) enforces the rules
-- independently so the platform works even when SeaweedFS' native
-- lifecycle support is incomplete (per the WS-29 doc's "Notes" caveat).
--
-- Storage_buckets gets new columns for the toggles + the object-lock
-- default; the lifecycle rules live in their own normalised table so a
-- single rule change does not rewrite the whole policy (matches the
-- per-row pattern used by compute_snapshot_policies from WS-25).

-- -------------------------------------------------------------------------
-- storage_buckets extensions
-- -------------------------------------------------------------------------

ALTER TABLE storage_buckets
    ADD COLUMN versioning_status              TEXT NOT NULL DEFAULT 'unversioned',
    ADD COLUMN object_lock_enabled            BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN object_lock_default_mode       TEXT,
    ADD COLUMN object_lock_default_retention_days INT;

-- Versioning status: mirrors S3's three BucketVersioningStatus values.
-- unversioned: never enabled (default; buckets created before WS-29).
-- enabled    : PUT-creates a new version; DELETE-creates a delete marker.
-- suspended  : no new versions are created; existing versions stay.
ALTER TABLE storage_buckets ADD CONSTRAINT storage_buckets_versioning_status_valid
    CHECK (versioning_status IN ('unversioned', 'enabled', 'suspended'));

-- Object-lock default mode: only set when object_lock_enabled is TRUE.
-- GOVERNANCE: privileged users can bypass (special perm).
-- COMPLIANCE: no one (including root) can bypass — WORM.
ALTER TABLE storage_buckets ADD CONSTRAINT storage_buckets_object_lock_mode_valid
    CHECK (object_lock_default_mode IS NULL
           OR object_lock_default_mode IN ('GOVERNANCE', 'COMPLIANCE'));

-- Object-lock default retention: positive integer days when set; NULL
-- means "no default retention period" (legal hold can still be applied
-- per-object).
ALTER TABLE storage_buckets ADD CONSTRAINT storage_buckets_object_lock_days_valid
    CHECK (object_lock_default_retention_days IS NULL
           OR object_lock_default_retention_days >= 1);

-- If object_lock is enabled, a default mode must be set. Mirrors the
-- AWS S3 constraint that object lock requires a default retention config.
ALTER TABLE storage_buckets ADD CONSTRAINT storage_buckets_object_lock_coherent
    CHECK (
        (object_lock_enabled = FALSE)
        OR (object_lock_enabled = TRUE
            AND object_lock_default_mode IS NOT NULL
            AND object_lock_default_retention_days IS NOT NULL)
    );

COMMENT ON COLUMN storage_buckets.versioning_status              IS 'Bucket versioning state (unversioned | enabled | suspended). Mirrors S3 BucketVersioningStatus.';
COMMENT ON COLUMN storage_buckets.object_lock_enabled            IS 'TRUE when the bucket has been configured with an object-lock policy. Requires object_lock_default_mode + object_lock_default_retention_days.';
COMMENT ON COLUMN storage_buckets.object_lock_default_mode       IS 'S3 object-lock default retention mode (GOVERNANCE | COMPLIANCE). NULL when object_lock_enabled is FALSE.';
COMMENT ON COLUMN storage_buckets.object_lock_default_retention_days IS 'Object-lock default retention period in days. NULL when object_lock_enabled is FALSE.';

-- -------------------------------------------------------------------------
-- storage_lifecycle_rules: one row per rule per bucket
-- -------------------------------------------------------------------------

CREATE TABLE storage_lifecycle_rules (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- bucket_id cascade: dropping a bucket drops its rules so the policy
    -- never outlives the bucket it belongs to.
    bucket_id       UUID        NOT NULL REFERENCES storage_buckets (id) ON DELETE CASCADE,

    -- User-provided rule identifier within the bucket. Mirrors S3's
    -- LifecycleRule.ID field. (bucket_id, rule_id) is unique.
    rule_id         TEXT        NOT NULL,

    -- Status: enabled = the lifecycle evaluator considers this rule;
    -- disabled = the rule is recorded but ignored (matches the S3
    -- LifecycleRule.Status field).
    status          TEXT        NOT NULL DEFAULT 'enabled',

    -- The action this rule performs. The set is fixed by the S3
    -- lifecycle action surface Lahijan supports (per WS-29 scope).
    --   expiration                   : delete the current version after N days.
    --   noncurrent_version_expiration: delete noncurrent versions after N days.
    --   abort_incomplete_multipart   : abort multipart uploads older than N days.
    --   transition                   : move the current version to a lower tier after N days.
    action          TEXT        NOT NULL,

    -- Days after which the action fires. When non-zero, the rule uses
    -- "age in days" semantics; when NULL, date_field is used instead.
    -- Mutually exclusive with date_at (enforced by constraint below).
    days            INT,

    -- Absolute date at which the action fires (alternative to days).
    -- Stored as UTC. NULL when days is set.
    date_at         TIMESTAMPTZ,

    -- For transition rules only: the storage class to transition to.
    -- Free-form text so we can support SeaweedFS tier names + AWS class
    -- names ("STANDARD_IA", "GLACIER", "SeaweedFS-cold", ...).
    -- NULL for non-transition rules.
    storage_class   TEXT,

    -- Prefix filter. NULL or empty = whole bucket. Mirrors
    -- LifecycleRule.Filter.Prefix (Lahijan does not support tag-based
    -- filters in MVP; a future WS can add a tag_filter_json column).
    prefix          TEXT,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Validate the status + action enumerations so a typo cannot strand a
-- row in an unknown state.
ALTER TABLE storage_lifecycle_rules ADD CONSTRAINT storage_lifecycle_rules_status_valid
    CHECK (status IN ('enabled', 'disabled'));
ALTER TABLE storage_lifecycle_rules ADD CONSTRAINT storage_lifecycle_rules_action_valid
    CHECK (action IN (
        'expiration',
        'noncurrent_version_expiration',
        'abort_incomplete_multipart',
        'transition'
    ));

-- days + date_at are mutually exclusive; exactly one must be set so the
-- evaluator has a deterministic trigger.
ALTER TABLE storage_lifecycle_rules ADD CONSTRAINT storage_lifecycle_rules_trigger_coherent
    CHECK (
        (days IS NOT NULL AND date_at IS NULL)
        OR (days IS NULL AND date_at IS NOT NULL)
    );

-- days, when set, must be positive (S3 disallows 0; we mirror that).
ALTER TABLE storage_lifecycle_rules ADD CONSTRAINT storage_lifecycle_rules_days_positive
    CHECK (days IS NULL OR days >= 1);

-- transition rules require a storage_class; the other actions forbid it.
ALTER TABLE storage_lifecycle_rules ADD CONSTRAINT storage_lifecycle_rules_transition_storage_class
    CHECK (
        (action = 'transition' AND storage_class IS NOT NULL)
        OR (action <> 'transition' AND storage_class IS NULL)
    );

-- (bucket_id, rule_id) is unique within a bucket. (tenant_id, rule_id)
-- alone is NOT unique because two tenants can each have a "log-expiry"
-- rule without colliding.
CREATE UNIQUE INDEX uq_storage_lifecycle_rules_bucket_rule
    ON storage_lifecycle_rules (bucket_id, rule_id);

-- Hot lookups for the per-tenant rule scan the lifecycle evaluator
-- performs every tick.
CREATE INDEX idx_storage_lifecycle_rules_tenant
    ON storage_lifecycle_rules (tenant_id)
    WHERE status = 'enabled';
CREATE INDEX idx_storage_lifecycle_rules_bucket
    ON storage_lifecycle_rules (bucket_id);

COMMENT ON TABLE  storage_lifecycle_rules              IS 'Per-bucket S3 lifecycle rules (WS-29, ADR-0036). The lifecycle evaluator River worker scans this table and acts on due rules.';
COMMENT ON COLUMN storage_lifecycle_rules.rule_id      IS 'User-provided rule identifier within the bucket; (bucket_id, rule_id) is unique.';
COMMENT ON COLUMN storage_lifecycle_rules.status      IS 'enabled = evaluator considers it; disabled = recorded but ignored.';
COMMENT ON COLUMN storage_lifecycle_rules.action      IS 'Lifecycle action: expiration | noncurrent_version_expiration | abort_incomplete_multipart | transition.';
COMMENT ON COLUMN storage_lifecycle_rules.days        IS 'Age in days after which the rule fires. Mutually exclusive with date_at.';
COMMENT ON COLUMN storage_lifecycle_rules.date_at     IS 'Absolute date at which the rule fires. Mutually exclusive with days.';
COMMENT ON COLUMN storage_lifecycle_rules.storage_class IS 'Required for transition rules; the storage class to transition to. NULL otherwise.';
COMMENT ON COLUMN storage_lifecycle_rules.prefix      IS 'Prefix filter; NULL or empty = whole bucket.';
