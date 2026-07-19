// Package storage: quotas.go implements per-bucket quota configuration
// + the cached-usage read path. Per WS-16 scope quotas are enforced in
// two layers:
//
//   - **Backend (this file):** SeaweedFS rejects PUTs once the bucket
//     size or object count exceeds the configured ceiling. This is the
//     hard ceiling that protects the cluster from a runaway upload. The
//     storage service pushes the quota to SeaweedFS on every change so
//     the daemon enforces it server-side.
//   - **Metering (WS-17):** the billing subsystem tracks per-tenant
//     usage and bills based on consumed capacity. That path is
//     independent of quotas and works whether or not a backend quota
//     is set. The metering job also refreshes the cached bytes_used /
//     objects_used columns on storage_buckets so the GET /usage
//     endpoint returns current numbers without a daemon round-trip.
//
// Per ADR-0011 the storage service is the ONLY sanctioned path to push
// quota changes to SeaweedFS. Drivers and admin tooling that bypass
// this layer leave the Lahijan DB out of sync with the daemon.
package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// QuotaSpec carries the per-bucket quota dimensions. Zero on either
// dimension means "no limit on that dimension".
type QuotaSpec struct {
	// QuotaBytes is the maximum total object size in bytes. Zero means
	// unlimited.
	QuotaBytes int64
	// QuotaObjects is the maximum number of objects. Zero means
	// unlimited.
	QuotaObjects int64
}

// BucketUsage is the cached-usage read shape returned by GET /usage. The
// Quota fields are the configured ceiling; the Used fields are the
// cached current values refreshed by the WS-17 metering job.
type BucketUsage struct {
	// QuotaBytes is the configured size ceiling; 0 means unlimited.
	QuotaBytes int64 `json:"quota_bytes"`
	// QuotaObjects is the configured object-count ceiling; 0 means unlimited.
	QuotaObjects int64 `json:"quota_objects"`
	// BytesUsed is the cached total object size in bytes.
	BytesUsed int64 `json:"bytes_used"`
	// ObjectsUsed is the cached total object count.
	ObjectsUsed int64 `json:"objects_used"`
}

// SetBucketQuota orchestrates a quota update: validate -> audit pending
// -> SeaweedFS push -> storage_buckets update -> event emit -> audit
// outcome. The SeaweedFS daemon enforces the new ceiling on every
// subsequent PUT.
func (s *Service) SetBucketQuota(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	quota QuotaSpec,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	if err := validateQuota(quota.QuotaBytes, quota.QuotaObjects); err != nil {
		return err
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.bucket.quota.set: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketQuotaSet,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug":   row.Slug,
			"quota_bytes":   quota.QuotaBytes,
			"quota_objects": quota.QuotaObjects,
		},
	})

	// Push the new ceiling to SeaweedFS so the daemon enforces it on
	// every subsequent PUT. SeaweedFS' SizeMiB is mebibytes; round up
	// so a 1-byte ceiling still blocks the next PUT.
	driverQuota := seaweedfs.QuotaSpec{
		SizeMiB:   0,
		FileCount: quota.QuotaObjects,
	}
	if quota.QuotaBytes > 0 {
		driverQuota.SizeMiB = (quota.QuotaBytes + (1 << 20) - 1) >> 20
	}
	if err := s.provider.SetBucketQuota(ctx, row.Name, driverQuota); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.quota.set: %w", err)
	}

	if err := s.repos.StorageBuckets.SetQuota(ctx, bucketID, quota.QuotaBytes, quota.QuotaObjects); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.quota.set: %w", err)
	}

	s.emitEvent(ctx, eventbus.S3BucketQuotaSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug":   row.Slug,
		"quota_bytes":   quota.QuotaBytes,
		"quota_objects": quota.QuotaObjects,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// GetBucketUsage returns the cached usage for the bucket. The cached
// values are refreshed by the WS-17 metering job; the GET /usage
// endpoint reads the cache so it does not need a SeaweedFS round-trip.
//
// A future WS may add a `?live=true` query param that triggers a
// reconcile-on-read against SeaweedFS; for MVP the cache is enough.
func (s *Service) GetBucketUsage(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
) (BucketUsage, error) {
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return BucketUsage{}, ErrBucketNotFound
		}
		return BucketUsage{}, fmt.Errorf("storage.bucket.usage: %w", err)
	}
	return BucketUsage{
		QuotaBytes:   row.QuotaBytes,
		QuotaObjects: row.QuotaObjects,
		BytesUsed:    row.BytesUsed,
		ObjectsUsed:  row.ObjectsUsed,
	}, nil
}
