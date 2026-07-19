// Package database: storage_credentials_repo.go wraps the sqlc-generated
// storage_credentials queries (WS-16). Every query is tenant-scoped via
// WithTenant at the repository seam — callers cannot pass a tenant id
// directly. The access_key_id column is the SeaweedFS-minted S3 access key
// string; the secret_hash column carries only the sha256 fingerprint of
// the plaintext secret so the Lahijan side never persists the secret
// itself.
//
// Revocation is a soft-delete (revoked_at). The Lahijan row stays so the
// audit trail survives; the SeaweedFS identity is removed at revoke time
// by the storage service via the provider's RevokeCredentials.
//
// The cross-tenant GetByAccessKeyGlobal (admin-only) path is the one
// exception to tenant scoping: it's used by the WS-17 janitor that
// revokes expired credentials regardless of which tenant owns them.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// StorageCredentialsRepository is the persistence boundary for the
// storage_credentials table.
type StorageCredentialsRepository struct {
	q *gen.Queries
}

// NewStorageCredentialsRepository wraps the given sqlc queries.
func NewStorageCredentialsRepository(q *gen.Queries) *StorageCredentialsRepository {
	return &StorageCredentialsRepository{q: q}
}

// CreateStorageCredentialParams carries the user-controlled fields of a
// new storage_credentials row. TenantID is taken from the request
// context, NOT from the caller.
type CreateStorageCredentialParams struct {
	BucketID   uuid.UUID
	UserID     uuid.UUID
	AccessKey  string
	SecretHash string
	Label      string
	Actions    []string
	ExpiresAt  *time.Time
}

// Create inserts a new storage_credentials row scoped to the tenant in
// ctx. The plaintext secret is NEVER stored; the caller passes the
// sha256 fingerprint via SecretHash.
func (r *StorageCredentialsRepository) Create(
	ctx context.Context,
	arg CreateStorageCredentialParams,
) (gen.StorageCredential, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageCredential{}, err
	}
	return r.q.CreateStorageCredential(ctx, gen.CreateStorageCredentialParams{
		BucketID:    arg.BucketID,
		TenantID:    tenantID,
		UserID:      arg.UserID,
		AccessKeyID: arg.AccessKey,
		SecretHash:  arg.SecretHash,
		Label:       arg.Label,
		Actions:     arg.Actions,
		ExpiresAt:   arg.ExpiresAt,
	})
}

// Get returns the storage_credentials row with id within the tenant in
// ctx. Includes revoked rows so the audit trail can reference them.
func (r *StorageCredentialsRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.StorageCredential, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageCredential{}, err
	}
	return r.q.GetStorageCredentialByID(ctx, gen.GetStorageCredentialByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByAccessKey returns the storage_credentials row for the given
// access key string within the tenant in ctx. Used by the storage
// service on every privileged call to enforce tenant isolation at the
// repository seam — a tenant cannot operate on a credential they do not
// own. Includes revoked rows so the audit trail can reference them.
func (r *StorageCredentialsRepository) GetByAccessKey(
	ctx context.Context,
	accessKey string,
) (gen.StorageCredential, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageCredential{}, err
	}
	return r.q.GetStorageCredentialByAccessKey(ctx, gen.GetStorageCredentialByAccessKeyParams{
		TenantID: tenantID, AccessKeyID: accessKey,
	})
}

// GetByAccessKeyGlobal is the admin-only cross-tenant lookup. Returns
// the storage_credentials row for the access key regardless of which
// tenant owns it. Used by the WS-17 janitor that revokes expired
// credentials regardless of which tenant owns them.
func (r *StorageCredentialsRepository) GetByAccessKeyGlobal(
	ctx context.Context,
	accessKey string,
) (gen.StorageCredential, error) {
	return r.q.GetStorageCredentialByAccessKeyGlobal(ctx, accessKey)
}

// ListForBucket returns a page of non-revoked storage_credentials rows
// scoped to the bucket within the tenant in ctx. Ordered by created_at
// DESC so the newest credentials come first.
func (r *StorageCredentialsRepository) ListForBucket(
	ctx context.Context,
	bucketID uuid.UUID,
	limit, offset int32,
) ([]gen.StorageCredential, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListStorageCredentialsForBucket(ctx, gen.ListStorageCredentialsForBucketParams{
		TenantID: tenantID, BucketID: bucketID, Limit: limit, Offset: offset,
	})
}

// CountForBucket returns the number of non-revoked storage_credentials
// rows scoped to the bucket within the tenant in ctx.
func (r *StorageCredentialsRepository) CountForBucket(
	ctx context.Context,
	bucketID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountStorageCredentialsForBucket(ctx, gen.CountStorageCredentialsForBucketParams{
		TenantID: tenantID, BucketID: bucketID,
	})
}

// ListForUser returns a page of non-revoked storage_credentials rows
// scoped to the user within the tenant in ctx.
func (r *StorageCredentialsRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.StorageCredential, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListStorageCredentialsForUser(ctx, gen.ListStorageCredentialsForUserParams{
		TenantID: tenantID, UserID: userID, Limit: limit, Offset: offset,
	})
}

// Revoke marks the credential as revoked. The SeaweedFS identity is
// removed separately by the storage service via the provider's
// RevokeCredentials so the access key stops signing requests immediately.
// Idempotent: revoking an already-revoked credential is a no-op.
func (r *StorageCredentialsRepository) Revoke(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.RevokeStorageCredential(ctx, gen.RevokeStorageCredentialParams{
		TenantID: tenantID, ID: id,
	})
}

// RevokeAllForBucket bulk-revokes every credential scoped to the bucket
// within the tenant in ctx. Called by the storage service at
// bucket-delete time so no orphaned credentials outlive their parent
// bucket.
func (r *StorageCredentialsRepository) RevokeAllForBucket(
	ctx context.Context,
	bucketID uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.RevokeAllStorageCredentialsForBucket(ctx, gen.RevokeAllStorageCredentialsForBucketParams{
		TenantID: tenantID, BucketID: bucketID,
	})
}

// TouchLastUsed updates the cached last_used_at column. Called by the
// access-log shipping pipeline (Phase 7) when SeaweedFS emits a
// per-identity access event.
func (r *StorageCredentialsRepository) TouchLastUsed(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.TouchStorageCredentialLastUsed(ctx, gen.TouchStorageCredentialLastUsedParams{
		TenantID: tenantID, ID: id,
	})
}
