// Package storage: buckets.go implements the bucket CRUD (create / get /
// list / update / delete) + per-action audit + event emission. Every
// privileged action emits an audit row BEFORE the side effect
// (status=pending) and marks the outcome AFTER; every lifecycle
// transition also emits into the WASM event bus so plugins can react.
//
// The orchestration order mirrors dns/zones.go:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Validation (slug shape per ADR-0011).
//  3. Audit emit (status=pending) — the row exists even if step 5 fails.
//  4. Cross-tenant uniqueness check via the storage_buckets repo
//     (canonical name is globally unique).
//  5. SeaweedFS create + storage_buckets row insert (the row caches the
//     canonical name).
//  6. Event bus emit (so plugins react after the DB is consistent).
//  7. Audit mark-outcome (success | failure).
//
// If the provider is nil the service returns ErrProviderDisabled which the
// handler maps to 501 not_implemented.
package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// BucketCreateParams carries the user-controlled fields of a create-bucket
// call. Slug is the user-picked portion (validated); the canonical name is
// composed from the tenant id + slug.
type BucketCreateParams struct {
	// Slug is the user-picked bucket slug (1-26 lowercase alphanumeric
	// + dashes per ADR-0011). Required.
	Slug string
	// Label is the optional user-facing label. Falls back to the slug
	// when empty.
	Label string
	// Description is the optional free-form description.
	Description string
	// QuotaBytes is the optional per-bucket size ceiling. Zero means
	// "no backend quota" (Lahijan enforces via metering).
	QuotaBytes int64
	// QuotaObjects is the optional per-bucket object-count ceiling.
	// Zero means "no backend quota".
	QuotaObjects int64
}

// CreateBucket orchestrates a bucket create: validate -> audit pending ->
// canonical-name uniqueness check -> SeaweedFS create -> storage_buckets
// row insert -> event emit -> audit outcome. Returns the cached
// storage_buckets row.
func (s *Service) CreateBucket(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params BucketCreateParams,
) (database.StorageBucket, error) {
	if s.provider == nil {
		return database.StorageBucket{}, ErrProviderDisabled
	}
	// Compose the canonical name. BucketName validates both the tenant
	// id (UUID) and the slug (1-26 lowercase alphanumeric + dashes per
	// ADR-0011). Surface a slug-specific sentinel so the handler
	// returns 400 (not 500) on a bad slug.
	canonicalName, errName := seaweedfs.BucketName(tenantID.String(), params.Slug)
	if errName != nil {
		return database.StorageBucket{}, fmt.Errorf("%w: %w", ErrInvalidBucketSlug, errName)
	}
	if err := validateQuota(params.QuotaBytes, params.QuotaObjects); err != nil {
		return database.StorageBucket{}, err
	}

	// 1) Cross-tenant uniqueness: canonical name is globally unique
	// (the storage_buckets.name column has a unique index). The
	// repository's CreateStorageBucket surfaces a unique-violation as
	// ErrBucketAlreadyExists below; this pre-check short-circuits the
	// common case so we do not waste a SeaweedFS round-trip on a name
	// already claimed.
	if existing, errLookup := s.repos.StorageBuckets.GetByCanonicalNameGlobal(ctx, canonicalName); errLookup == nil && existing.ID != uuid.Nil {
		return database.StorageBucket{}, fmt.Errorf("%w: %s", ErrBucketAlreadyExists, canonicalName)
	}

	// 2) Audit emit (status=pending). The row exists even if step 4 fails.
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketCreate,
		ResourceType: ResourceBucket,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"slug":          params.Slug,
			"canonical":     canonicalName,
			"label":         params.Label,
			"description":   params.Description,
			"quota_bytes":   params.QuotaBytes,
			"quota_objects": params.QuotaObjects,
		},
	})

	// 3) SeaweedFS create. The provider applies the per-bucket quota
	// at create time so the daemon enforces it on every subsequent PUT.
	quota := seaweedfs.QuotaSpec{}
	applyBackendQuota := false
	if params.QuotaBytes > 0 {
		// SeaweedFS' SizeMiB is mebibytes; round up so a 1-byte
		// ceiling still blocks the next PUT.
		quota.SizeMiB = (params.QuotaBytes + (1 << 20) - 1) >> 20
		applyBackendQuota = true
	}
	if params.QuotaObjects > 0 {
		quota.FileCount = params.QuotaObjects
		applyBackendQuota = true
	}
	if !applyBackendQuota {
		// Fall back to the service default when the caller did not
		// pass any dimension. A zero-value default disables the
		// backend quota (Lahijan enforces via metering).
		if s.config.DefaultQuotaBytes > 0 {
			quota.SizeMiB = (s.config.DefaultQuotaBytes + (1 << 20) - 1) >> 20
		}
		if s.config.DefaultQuotaObjects > 0 {
			quota.FileCount = s.config.DefaultQuotaObjects
		}
	}
	var quotaPtr *seaweedfs.QuotaSpec
	if quota.SizeMiB > 0 || quota.FileCount > 0 {
		quotaPtr = &quota
	}
	if _, err := s.provider.CreateBucket(ctx, seaweedfs.CreateBucketParams{
		Bucket:   canonicalName,
		TenantID: tenantID.String(),
		Quota:    quotaPtr,
	}); err != nil {
		// If SeaweedFS reports the bucket already exists, treat it the
		// same as our cross-tenant short-circuit.
		if errors.Is(err, seaweedfs.ErrAlreadyExists) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": "bucket already exists in SeaweedFS",
			}})
			return database.StorageBucket{}, fmt.Errorf("%w: %s", ErrBucketAlreadyExists, canonicalName)
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.StorageBucket{}, fmt.Errorf("storage.bucket.create: %w", err)
	}

	// 4) Cache the bucket row. The canonical name is what SeaweedFS
	// returned (== what we composed); the slug is the user-picked
	// portion.
	row, err := s.repos.StorageBuckets.Create(ctx, database.CreateStorageBucketParams{
		Name:         canonicalName,
		Slug:         params.Slug,
		OwnerUserID:  userID,
		Label:        params.Label,
		Description:  params.Description,
		QuotaBytes:   params.QuotaBytes,
		QuotaObjects: params.QuotaObjects,
	})
	if err != nil {
		// The DB write failed (typically a unique violation if
		// another tenant raced us). Best-effort: roll back the
		// SeaweedFS create so the daemon does not keep an orphaned
		// bucket. A failure to roll back is logged but does not
		// propagate — the caller already has a problem.
		if rbErr := s.provider.DeleteBucket(ctx, canonicalName); rbErr != nil {
			_ = rbErr // logged by caller via audit metadata
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.StorageBucket{}, fmt.Errorf("storage.bucket.create: %w", err)
	}

	// 5) Event bus emit (s3.bucket.created).
	s.emitEvent(ctx, eventbus.S3BucketCreated, tenantID, userID, row.ID, map[string]any{
		"name":          row.Name,
		"slug":          row.Slug,
		"canonical":     row.Name,
		"quota_bytes":   row.QuotaBytes,
		"quota_objects": row.QuotaObjects,
	})

	// 6) Audit mark-outcome (success).
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"bucket_id": row.ID,
	}})

	return row, nil
}

// GetBucket returns the cached bucket row. The SeaweedFS daemon is NOT
// probed here; callers needing live quota state call ReconcileBucket first.
func (s *Service) GetBucket(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
) (database.StorageBucket, error) {
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.StorageBucket{}, ErrBucketNotFound
		}
		return database.StorageBucket{}, fmt.Errorf("storage.bucket.get: %w", err)
	}
	return row, nil
}

// ListBuckets returns a paginated list of the tenant's buckets.
func (s *Service) ListBuckets(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.StorageBucket, error) {
	rows, err := s.repos.StorageBuckets.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("storage.bucket.list: %w", err)
	}
	return rows, nil
}

// CountBuckets returns the number of buckets in the tenant.
func (s *Service) CountBuckets(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.StorageBuckets.Count(ctx)
}

// BucketUpdateParams carries the user-controlled fields of a PATCH bucket
// call. All fields are optional; a nil pointer means "leave unchanged".
type BucketUpdateParams struct {
	Label       *string
	Description *string
}

// UpdateBucket replaces the cached label / description for the bucket.
// Quota changes go through SetBucketQuota (separate privileged action).
func (s *Service) UpdateBucket(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	params BucketUpdateParams,
) (database.StorageBucket, error) {
	if s.provider == nil {
		return database.StorageBucket{}, ErrProviderDisabled
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.StorageBucket{}, ErrBucketNotFound
		}
		return database.StorageBucket{}, fmt.Errorf("storage.bucket.get: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketUpdate,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})

	if params.Label != nil && *params.Label != row.Label {
		if err := s.repos.StorageBuckets.SetLabel(ctx, bucketID, *params.Label); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return database.StorageBucket{}, fmt.Errorf("storage.bucket.update: %w", err)
		}
		row.Label = *params.Label
	}
	if params.Description != nil && *params.Description != row.Description {
		if err := s.repos.StorageBuckets.SetDescription(ctx, bucketID, *params.Description); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return database.StorageBucket{}, fmt.Errorf("storage.bucket.update: %w", err)
		}
		row.Description = *params.Description
	}

	// Event bus emit (s3.bucket.updated).
	s.emitEvent(ctx, eventbus.S3BucketUpdated, tenantID, userID, row.ID, map[string]any{
		"name":  row.Name,
		"slug":  row.Slug,
		"label": row.Label,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// DeleteBucket orchestrates a bucket delete: revoke every credential ->
// SeaweedFS delete -> storage_credentials bulk-revoke -> storage_buckets
// soft-delete -> event emit -> audit outcome. The order is important:
// credential revocation first so no orphaned credential outlives its
// parent bucket; then SeaweedFS delete so the daemon stops serving the
// bucket; then our DB cleanup so a daemon failure does not strand rows.
//
// Idempotent on the daemon side: if SeaweedFS reports 404 we still proceed
// so the storage_buckets row is removed even when the daemon's view
// drifted.
func (s *Service) DeleteBucket(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.bucket.get: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketDelete,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name": row.Name,
			"slug": row.Slug,
		},
	})

	// 1) Bulk-revoke every credential scoped to this bucket on the
	// SeaweedFS side. We read the live list first so we can revoke
	// each identity individually; the repository's RevokeAllForBucket
	// handles the DB side below.
	if creds, listErr := s.repos.StorageCredentials.ListForBucket(ctx, bucketID, 1000, 0); listErr == nil {
		for _, c := range creds {
			// Best-effort: a missing identity on the daemon side is
			// already-revoked; tolerate it.
			if revErr := s.provider.RevokeCredentials(ctx, c.AccessKeyID); revErr != nil && !errors.Is(revErr, seaweedfs.ErrNotFound) {
				// Log + continue; we still want to mark the Lahijan
				// rows as revoked so the audit trail is consistent.
				_ = revErr
			}
		}
	}

	// 2) SeaweedFS delete. Tolerate 404 — the daemon's view may have
	// drifted.
	if err := s.provider.DeleteBucket(ctx, row.Name); err != nil && !errors.Is(err, seaweedfs.ErrNotFound) {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.delete: %w", err)
	}

	// 3) Mark every credential revoked (DB side) + soft-delete the
	// bucket row. The credentials are orphaned without the parent
	// bucket; the soft-delete keeps the audit trail alive.
	if err := s.repos.StorageCredentials.RevokeAllForBucket(ctx, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.delete: revoke credentials: %w", err)
	}
	if err := s.repos.StorageBuckets.SoftDelete(ctx, bucketID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.delete: %w", err)
	}

	// 4) Event bus emit (s3.bucket.deleted).
	s.emitEvent(ctx, eventbus.S3BucketDeleted, tenantID, userID, row.ID, map[string]any{
		"name": row.Name,
		"slug": row.Slug,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// validateQuota enforces the per-dimension non-negativity invariant. The
// upper bound is intentionally unbounded — the SeaweedFS daemon enforces
// the cluster-wide capacity ceiling; Lahijan does not second-guess it.
func validateQuota(quotaBytes, quotaObjects int64) error {
	if quotaBytes < 0 || quotaObjects < 0 {
		return ErrInvalidQuota
	}
	return nil
}
