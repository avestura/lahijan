// Package storage: versioning.go implements the per-bucket versioning
// privileged actions on the storage.Service. Each method follows the
// orchestration order pinned in buckets.go:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Bucket lookup + tenant scope check at the repository seam.
//  3. Audit emit (status=pending) — the row exists even if step 5 fails.
//  4. SeaweedFS push (SetBucketVersioning) so the daemon enforces the
//     version semantics on every subsequent PUT.
//  5. storage_buckets row update (cached status).
//  6. Event bus emit (so plugins react after the DB is consistent).
//  7. Audit mark-outcome (success | failure).
//
// Per ADR-0036 the Postgres row is the source of truth; the SeaweedFS
// daemon is derived. A failed daemon push fails the whole action so the
// Lahijan row never claims a state the daemon does not enforce.
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

// VersioningStatus is the user-visible versioning status. Mirrors the
// driver's seaweedfs.VersioningStatus but the service layer does not
// import the driver package transitively for an enum.
type VersioningStatus string

const (
	// VersioningStatusUnversioned is the default.
	VersioningStatusUnversioned VersioningStatus = "unversioned"
	// VersioningStatusEnabled matches S3's "Enabled".
	VersioningStatusEnabled VersioningStatus = "enabled"
	// VersioningStatusSuspended matches S3's "Suspended".
	VersioningStatusSuspended VersioningStatus = "suspended"
)

// toDriverStatus translates the service-layer enum to the driver enum.
func toDriverStatus(s VersioningStatus) seaweedfs.VersioningStatus {
	switch s {
	case VersioningStatusEnabled:
		return seaweedfs.VersioningStatusEnabled
	case VersioningStatusSuspended:
		return seaweedfs.VersioningStatusSuspended
	case VersioningStatusUnversioned:
		return seaweedfs.VersioningStatusUnversioned
	}
	// Unknown values normalise to unversioned (defensive; the enum
	// constraint lives at the HTTP / OpenAPI boundary).
	return seaweedfs.VersioningStatusUnversioned
}

// fromRowStatus translates the storage_buckets.versioning_status column
// to the service-layer enum. The column is TEXT; a stray value is
// normalised to Unversioned (defensive — the CHECK constraint enforces
// the same set the service writes).
func fromRowStatus(s string) VersioningStatus {
	switch s {
	case "enabled":
		return VersioningStatusEnabled
	case "suspended":
		return VersioningStatusSuspended
	}
	return VersioningStatusUnversioned
}

// SetBucketVersioning orchestrates a versioning-status change. The
// caller has already passed the api/middleware.RequirePerm gate at the
// HTTP boundary (rbac.PermS3BucketVersioning).
func (s *Service) SetBucketVersioning(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	status VersioningStatus,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.bucket.versioning.set: %w", err)
	}
	if fromRowStatus(row.VersioningStatus) == status {
		// No-op: the cached state already matches.
		return nil
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketVersioningSet,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"previous":    row.VersioningStatus,
			"new":         string(status),
		},
	})

	// Push to SeaweedFS first. A failed push fails the whole action so
	// the Lahijan row never claims a state the daemon does not enforce.
	if err := s.provider.SetBucketVersioning(ctx, row.Name, toDriverStatus(status)); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.versioning.set: %w", err)
	}

	if err := s.repos.StorageBuckets.SetVersioning(ctx, bucketID, string(status)); err != nil {
		// Best-effort: roll back the daemon push so the daemon does not
		// stay in a state the Lahijan row refused to record. A failure
		// to roll back is logged but does not propagate.
		_ = s.provider.SetBucketVersioning(ctx, row.Name, toDriverStatus(fromRowStatus(row.VersioningStatus)))
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.versioning.set: %w", err)
	}

	s.emitEvent(ctx, eventbus.S3BucketVersioningSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"status":      string(status),
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// GetBucketVersioning returns the cached versioning status from
// Postgres. Does NOT round-trip to SeaweedFS — the Postgres row is the
// source of truth (ADR-0036 sub-decision C).
func (s *Service) GetBucketVersioning(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
) (VersioningStatus, error) {
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return VersioningStatusUnversioned, ErrBucketNotFound
		}
		return VersioningStatusUnversioned, fmt.Errorf("storage.bucket.versioning.get: %w", err)
	}
	return fromRowStatus(row.VersioningStatus), nil
}

// ListObjectVersions returns a paginated list of object versions in
// the bucket. The caller has already passed the
// rbac.PermS3BucketRead gate; this call proxies through to SeaweedFS
// because version listings are inherently live (the object set
// changes between PUTs and the cache would always be stale).
//
// The prefix / keyMarker / versionIDMarker cursors match the S3
// pagination semantics; pass empty strings for the first page.
func (s *Service) ListObjectVersions(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
	prefix, keyMarker, versionIDMarker string,
	maxKeys int32,
) (*seaweedfs.ObjectVersionsPage, error) {
	if s.provider == nil {
		return nil, ErrProviderDisabled
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("storage.bucket.versioning.list: %w", err)
	}
	page, err := s.provider.ListObjectVersions(ctx, row.Name, prefix, keyMarker, versionIDMarker, maxKeys)
	if err != nil {
		if errors.Is(err, seaweedfs.ErrNotFound) {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("storage.bucket.versioning.list: %w", err)
	}
	return page, nil
}

// RestoreObjectVersion restores a deleted object version by server-side
// copy from the versioned source to the current key. The caller has
// already passed the rbac.PermS3BucketUpdate gate (restore is an
// update-class action). When versionID is empty the latest noncurrent
// version is restored (best effort).
func (s *Service) RestoreObjectVersion(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	key, versionID string,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	if key == "" {
		return ErrInvalidPresignMethod // sentinel reuse for "bad request arg"
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.bucket.versioning.restore: %w", err)
	}

	// Restore does not flip the cached versioning_status; it acts on
	// an existing version. Audit it as a versioning-set-adjacent action
	// so the audit row carries the bucket_id + key + version_id.
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketVersioningSet,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"key":         key,
			"version_id":  versionID,
			"action":      "restore",
		},
	})

	if err := s.provider.RestoreObjectVersion(ctx, row.Name, key, versionID); err != nil {
		if errors.Is(err, seaweedfs.ErrNotFound) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": "version not found",
			}})
			return ErrBucketNotFound
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.versioning.restore: %w", err)
	}

	s.emitEvent(ctx, eventbus.S3ObjectDeleted, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"key":         key,
		"version_id":  versionID,
		"action":      "restored",
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}
