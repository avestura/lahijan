// Package database: storage_buckets_repo.go wraps the sqlc-generated
// storage_buckets queries (WS-16). Every query is tenant-scoped via
// WithTenant at the repository seam; callers cannot pass a tenant id
// directly. The canonical "name" column is the SeaweedFS-assigned bucket
// name (the compound "<tenant-uuid>-<slug>" form mandated by ADR-0011
// and enforced by providers/seaweedfs.BucketName).
//
// The cross-tenant GetByCanonicalNameGlobal (admin-only) path is the one
// exception: it's used by the storage service's "is this canonical name
// already claimed by another tenant?" check at bucket-create time.
// Includes soft-deleted rows so a re-create after delete is rejected with
// a clear "name claimed" error rather than colliding silently.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// StorageBucketsRepository is the persistence boundary for the
// storage_buckets table.
type StorageBucketsRepository struct {
	q *gen.Queries
}

// NewStorageBucketsRepository wraps the given sqlc queries.
func NewStorageBucketsRepository(q *gen.Queries) *StorageBucketsRepository {
	return &StorageBucketsRepository{q: q}
}

// CreateStorageBucketParams carries the user-controlled fields of a new
// storage_buckets row. TenantID is taken from the request context, NOT
// from the caller. Name is the canonical "<tenant-uuid>-<slug>" form
// (already validated + composed by providers/seaweedfs.BucketName); Slug
// is the user-picked portion alone.
type CreateStorageBucketParams struct {
	Name         string
	Slug         string
	OwnerUserID  uuid.UUID
	Label        string
	Description  string
	QuotaBytes   int64
	QuotaObjects int64
}

// Create inserts a new storage_buckets row scoped to the tenant in ctx.
func (r *StorageBucketsRepository) Create(
	ctx context.Context,
	arg CreateStorageBucketParams,
) (gen.StorageBucket, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageBucket{}, err
	}
	return r.q.CreateStorageBucket(ctx, gen.CreateStorageBucketParams{
		TenantID:     tenantID,
		Name:         arg.Name,
		Slug:         arg.Slug,
		OwnerUserID:  arg.OwnerUserID,
		Label:        arg.Label,
		Description:  arg.Description,
		QuotaBytes:   arg.QuotaBytes,
		QuotaObjects: arg.QuotaObjects,
	})
}

// Get returns the non-deleted storage_buckets row with id within the
// tenant in ctx.
func (r *StorageBucketsRepository) Get(
	ctx context.Context,
	id uuid.UUID,
) (gen.StorageBucket, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageBucket{}, err
	}
	return r.q.GetStorageBucketByID(ctx, gen.GetStorageBucketByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetBySlug returns the non-deleted storage_buckets row for slug within
// the tenant in ctx. Used by the storage service on every privileged call
// that takes a slug instead of an id (e.g. the admin CLI).
func (r *StorageBucketsRepository) GetBySlug(
	ctx context.Context,
	slug string,
) (gen.StorageBucket, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageBucket{}, err
	}
	return r.q.GetStorageBucketBySlug(ctx, gen.GetStorageBucketBySlugParams{
		TenantID: tenantID, Slug: slug,
	})
}

// GetByCanonicalName returns the non-deleted storage_buckets row for the
// canonical name within the tenant in ctx. Used by the storage service on
// every privileged call that needs to translate a SeaweedFS bucket name
// into a Lahijan row.
func (r *StorageBucketsRepository) GetByCanonicalName(
	ctx context.Context,
	name string,
) (gen.StorageBucket, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageBucket{}, err
	}
	return r.q.GetStorageBucketByCanonicalName(ctx, gen.GetStorageBucketByCanonicalNameParams{
		TenantID: tenantID, Name: name,
	})
}

// GetByCanonicalNameGlobal is the admin-only cross-tenant lookup.
// Returns the storage_buckets row for the canonical name regardless of
// which tenant owns it. Includes soft-deleted rows so a re-create after
// delete is rejected with a clear "name claimed" error rather than
// colliding silently. Used by the storage service at bucket-create time
// to short-circuit "another tenant already owns this canonical name".
func (r *StorageBucketsRepository) GetByCanonicalNameGlobal(
	ctx context.Context,
	name string,
) (gen.StorageBucket, error) {
	return r.q.GetStorageBucketByCanonicalNameGlobal(ctx, name)
}

// List returns a page of non-deleted storage_buckets rows within the
// tenant in ctx. Ordered by created_at DESC so the newest buckets come
// first (matches the compute + DNS list endpoints' ordering).
func (r *StorageBucketsRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.StorageBucket, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListStorageBuckets(ctx, gen.ListStorageBucketsParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted storage_buckets rows within
// the tenant in ctx.
func (r *StorageBucketsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountStorageBuckets(ctx, tenantID)
}

// SetLabel updates the label of a bucket.
func (r *StorageBucketsRepository) SetLabel(
	ctx context.Context,
	id uuid.UUID,
	label string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.UpdateStorageBucketLabel(ctx, gen.UpdateStorageBucketLabelParams{
		TenantID: tenantID, ID: id, Label: label,
	})
}

// SetDescription updates the description of a bucket.
func (r *StorageBucketsRepository) SetDescription(
	ctx context.Context,
	id uuid.UUID,
	description string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.UpdateStorageBucketDescription(ctx, gen.UpdateStorageBucketDescriptionParams{
		TenantID: tenantID, ID: id, Description: description,
	})
}

// SetQuota updates the cached quota dimensions on the row. The SeaweedFS
// daemon is updated separately by the storage service via the provider's
// SetBucketQuota so the daemon enforces the ceiling server-side.
func (r *StorageBucketsRepository) SetQuota(
	ctx context.Context,
	id uuid.UUID,
	quotaBytes, quotaObjects int64,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetStorageBucketQuota(ctx, gen.SetStorageBucketQuotaParams{
		TenantID:     tenantID,
		ID:           id,
		QuotaBytes:   quotaBytes,
		QuotaObjects: quotaObjects,
	})
}

// SetUsage updates the cached bytes_used / objects_used columns. Called
// by the WS-17 metering job after it polls SeaweedFS for the live bucket
// size. Exported so the metering job (a separate package) can call it
// directly without going through the storage service.
func (r *StorageBucketsRepository) SetUsage(
	ctx context.Context,
	id uuid.UUID,
	bytesUsed, objectsUsed int64,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetStorageBucketUsage(ctx, gen.SetStorageBucketUsageParams{
		TenantID:    tenantID,
		ID:          id,
		BytesUsed:   bytesUsed,
		ObjectsUsed: objectsUsed,
	})
}

// SoftDelete marks the row as deleted. The SeaweedFS bucket is removed
// separately by the storage service via the provider's DeleteBucket; the
// row stays so the audit trail can reference it.
func (r *StorageBucketsRepository) SoftDelete(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteStorageBucket(ctx, gen.SoftDeleteStorageBucketParams{
		TenantID: tenantID, ID: id,
	})
}

// SetVersioning updates the cached versioning_status column (WS-29,
// ADR-0036). The SeaweedFS daemon is updated separately by the storage
// service via the provider's SetBucketVersioning so the daemon enforces
// the version semantics on every subsequent PUT / DELETE.
func (r *StorageBucketsRepository) SetVersioning(
	ctx context.Context,
	id uuid.UUID,
	status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetStorageBucketVersioning(ctx, gen.SetStorageBucketVersioningParams{
		TenantID: tenantID, ID: id, VersioningStatus: status,
	})
}

// SetObjectLock replaces the bucket-level object-lock policy (WS-29,
// ADR-0036). When enabled is FALSE the mode + days columns MUST be nil
// (the storage service enforces this); the table-level CHECK guarantees
// the invariant at the database layer.
//
// The SeaweedFS daemon is updated separately by the storage service via
// the provider's SetObjectLockConfiguration.
func (r *StorageBucketsRepository) SetObjectLock(
	ctx context.Context,
	id uuid.UUID,
	enabled bool,
	mode *string,
	days *int32,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetStorageBucketObjectLock(ctx, gen.SetStorageBucketObjectLockParams{
		TenantID:                       tenantID,
		ID:                             id,
		ObjectLockEnabled:              enabled,
		ObjectLockDefaultMode:          mode,
		ObjectLockDefaultRetentionDays: days,
	})
}
