-- 0034_storage_credentials: per-tenant object storage credentials (WS-16).
--
-- Per ADR-0002 every tenant-scoped table has id + tenant_id + the standard
-- audit columns. storage_credentials is the canonical Lahijan-side record of
-- a per-user S3 credential scoped to one bucket. The credential is minted by
-- providers/seaweedfs.MintCredentials, which writes the identity (with the
-- plaintext secret) into SeaweedFS' Filer at
-- /etc/seaweedfs/identities/<access_key>.json; this table holds ONLY the
-- fingerprint (sha256 of the secret) so the Lahijan side never persists the
-- plaintext. The plaintext secret is returned to the caller exactly once at
-- mint time.
--
-- The row is the source of truth for "which credentials exist on the Lahijan
-- side". The SeaweedFS IAM subsystem enforces per-bucket ACLs at request time
-- using the identity record written by MintCredentials; the actions list is
-- cached here so the admin UI can render scopes without a Filer round-trip.
--
-- bucket_id + tenant_id are both NOT NULL: bucket_id is the FK into
-- storage_buckets (cascades on bucket delete), tenant_id is denormalized so
-- the repository layer can tenant-scope every query. The denormalization is
-- safe because the tenant never changes after creation.
--
-- Revocation is a soft-delete (revoked_at). The Lahijan row stays so the
-- audit trail survives; the SeaweedFS identity is removed at revoke time so
-- the access key stops signing requests immediately.

CREATE TABLE storage_credentials (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id       UUID        NOT NULL REFERENCES storage_buckets (id) ON DELETE CASCADE,
    tenant_id       UUID        NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    -- the user who owns the credential. The credential inherits the user's
    -- tenant; cross-tenant use is rejected at the repository seam.
    user_id         UUID        NOT NULL REFERENCES users (id) ON DELETE NO ACTION,
    -- access_key_id is the S3 access key string the provider minted. Unique
    -- across the platform because SeaweedFS' IAM namespace is cluster-wide.
    access_key_id   TEXT        NOT NULL,
    -- sha256(secret) — forensic fingerprint only; never used to verify
    -- presented secrets (SeaweedFS does that). The plaintext secret is
    -- shown to the caller exactly once at mint time and then forgotten.
    secret_hash     TEXT        NOT NULL,
    -- optional user-supplied label so the user can tell credentials apart
    -- in the dashboard ("prod-uploader", "ci-runner", etc.).
    label           TEXT        NOT NULL DEFAULT '',
    -- cached high-level action list ("Read", "Write", ...). The provider
    -- expands this into per-bucket scope at mint time; the cache is for the
    -- dashboard / list endpoint convenience.
    actions         TEXT[]      NOT NULL DEFAULT '{}',
    last_used_at    TIMESTAMPTZ,
    -- optional expiry. When set, the metering job (WS-17) or a future
    -- janitor revokes the credential past this timestamp. The SeaweedFS
    -- daemon does NOT enforce S3 expiries today; Lahijan owns this.
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at      TIMESTAMPTZ
);

-- access_key_id is globally unique (SeaweedFS' IAM namespace is cluster-wide).
CREATE UNIQUE INDEX uq_storage_credentials_access_key   ON storage_credentials (access_key_id);
-- Hot lookups: per-tenant list, per-bucket list, per-user list.
CREATE INDEX        idx_storage_credentials_tenant_id   ON storage_credentials (tenant_id)
    WHERE revoked_at IS NULL;
CREATE INDEX        idx_storage_credentials_bucket_id   ON storage_credentials (bucket_id)
    WHERE revoked_at IS NULL;
CREATE INDEX        idx_storage_credentials_user_id     ON storage_credentials (user_id)
    WHERE revoked_at IS NULL;

COMMENT ON TABLE  storage_credentials                IS 'Per-tenant object storage credentials. Mirrors SeaweedFS identities; the storage service (WS-16) upserts on every change.';
COMMENT ON COLUMN storage_credentials.access_key_id  IS 'S3 access key string; globally unique across the platform.';
COMMENT ON COLUMN storage_credentials.secret_hash    IS 'sha256(secret) — forensic fingerprint only; plaintext secret is shown once at mint time and never persisted.';
COMMENT ON COLUMN storage_credentials.actions        IS 'Cached high-level action list ("Read", "Write", ...). Expanded by the provider into per-bucket scope at mint time.';
COMMENT ON COLUMN storage_credentials.expires_at     IS 'Optional expiry. Lahijan revokes the credential past this timestamp; SeaweedFS does not enforce S3 expiries today.';
COMMENT ON COLUMN storage_credentials.revoked_at     IS 'Soft-delete timestamp. The SeaweedFS identity is removed at revoke time; the Lahijan row stays for the audit trail.';
