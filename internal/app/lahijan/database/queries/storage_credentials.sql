-- Storage credentials: tenant-scoped per-bucket S3 credentials (WS-16).
-- The SeaweedFS driver mints per-user credentials scoped to specific buckets
-- (providers/seaweedfs.MintCredentials); this table caches the Lahijan-side
-- view so the dashboard / list endpoints do not need a Filer round-trip.
-- The plaintext secret is shown to the caller exactly once at mint time;
-- only the sha256 fingerprint is persisted in the secret_hash column.
-- Every query is tenant-scoped via WithTenant (database/tenant.go).

-- name: CreateStorageCredential :one
--: tenant-scoped
INSERT INTO storage_credentials (
    bucket_id, tenant_id, user_id, access_key_id, secret_hash, label,
    actions, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetStorageCredentialByID :one
--: tenant-scoped
SELECT * FROM storage_credentials
WHERE tenant_id = $1 AND id = $2;

-- name: GetStorageCredentialByAccessKey :one
-- Tenant-scoped lookup by access key string. Used by the storage service on
-- every privileged call to enforce tenant isolation at the repository seam
-- (a tenant cannot operate on a credential they do not own).
--: tenant-scoped
SELECT * FROM storage_credentials
WHERE tenant_id = $1 AND access_key_id = $2;

-- name: GetStorageCredentialByAccessKeyGlobal :one
-- Admin-only path: no tenant scoping. Used by the storage service's
-- cross-tenant revoke-by-access-key path (e.g. the WS-17 janitor revoking
-- expired credentials regardless of which tenant owns them).
SELECT * FROM storage_credentials WHERE access_key_id = $1;

-- name: ListStorageCredentialsForBucket :many
--: tenant-scoped
SELECT * FROM storage_credentials
WHERE tenant_id = $1 AND bucket_id = $2 AND revoked_at IS NULL
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountStorageCredentialsForBucket :one
--: tenant-scoped
SELECT count(*) FROM storage_credentials
WHERE tenant_id = $1 AND bucket_id = $2 AND revoked_at IS NULL;

-- name: ListStorageCredentialsForUser :many
--: tenant-scoped
SELECT * FROM storage_credentials
WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: RevokeStorageCredential :exec
--: tenant-scoped
-- Marks the credential as revoked. The SeaweedFS identity is removed
-- separately by the storage service via the provider's RevokeCredentials
-- so the access key stops signing requests immediately.
UPDATE storage_credentials
SET revoked_at = now()
WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL;

-- name: RevokeAllStorageCredentialsForBucket :exec
--: tenant-scoped
-- Bulk-revokes every credential scoped to the bucket. Called by the
-- storage service at bucket-delete time so no orphaned credentials outlive
-- their parent bucket.
UPDATE storage_credentials
SET revoked_at = now()
WHERE tenant_id = $1 AND bucket_id = $2 AND revoked_at IS NULL;

-- name: TouchStorageCredentialLastUsed :exec
--: tenant-scoped
-- Updates the cached last_used_at column. Called by the access-log shipping
-- pipeline (Phase 7) when SeaweedFS emits a per-identity access event.
UPDATE storage_credentials
SET last_used_at = now()
WHERE tenant_id = $1 AND id = $2;
