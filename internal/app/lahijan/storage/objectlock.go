// Package storage: objectlock.go implements the per-bucket object-lock
// privileged actions on the storage.Service. Per ADR-0036 sub-decision A
// the Postgres row is the source of truth; the SeaweedFS daemon is
// derived via the provider's SetObjectLockConfiguration.
//
// Per the WS-29 doc, per-object retention + legal-hold API endpoints
// are deferred; the bucket-default policy is enough for compliance MVP.
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

// ObjectLockMode is the service-layer enum for the S3 object-lock
// retention mode. Mirrors providers/seaweedfs.ObjectLockMode.
type ObjectLockMode string

const (
	ObjectLockModeGovernance ObjectLockMode = "GOVERNANCE"
	ObjectLockModeCompliance ObjectLockMode = "COMPLIANCE"
)

// toDriverMode maps the service enum to the driver enum.
func toDriverMode(m ObjectLockMode) seaweedfs.ObjectLockMode {
	switch m {
	case ObjectLockModeCompliance:
		return seaweedfs.ObjectLockModeCompliance
	}
	return seaweedfs.ObjectLockModeGovernance
}

// ObjectLockConfig is the service-layer value type for the bucket-level
// object-lock policy.
type ObjectLockConfig struct {
	// Enabled is true when object lock is enabled on the bucket.
	Enabled bool

	// Mode is the default retention mode. Empty when Enabled is false.
	Mode ObjectLockMode

	// Days is the default retention period in days. Zero when Enabled
	// is false.
	Days int32
}

// ObjectLockConfigFromRow assembles the service value type from the
// storage_buckets columns. Defensive: when the row has
// object_lock_enabled = true but mode or days is NULL (shouldn't
// happen due to the CHECK constraint), returns Enabled=false.
func ObjectLockConfigFromRow(row database.StorageBucket) ObjectLockConfig {
	if !row.ObjectLockEnabled {
		return ObjectLockConfig{}
	}
	cfg := ObjectLockConfig{Enabled: true}
	if row.ObjectLockDefaultMode != nil {
		switch *row.ObjectLockDefaultMode {
		case "GOVERNANCE":
			cfg.Mode = ObjectLockModeGovernance
		case "COMPLIANCE":
			cfg.Mode = ObjectLockModeCompliance
		}
	}
	if row.ObjectLockDefaultRetentionDays != nil {
		cfg.Days = *row.ObjectLockDefaultRetentionDays
	}
	if cfg.Mode == "" || cfg.Days == 0 {
		// Defensive: treat as disabled.
		return ObjectLockConfig{}
	}
	return cfg
}

// SetObjectLock orchestrates a bucket-level object-lock policy change.
// The caller has already passed rbac.PermS3BucketObjectLock at the HTTP
// boundary.
//
// Disabling object lock after it has been enabled is not supported by
// AWS S3; the SeaweedFS daemon rejects the config. This method still
// flips the Postgres cache to Enabled=false so the Lahijan UI reflects
// the operator's intent — the daemon push is skipped when Enabled is
// false.
func (s *Service) SetObjectLock(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	cfg ObjectLockConfig,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	if cfg.Enabled {
		if cfg.Mode != ObjectLockModeGovernance && cfg.Mode != ObjectLockModeCompliance {
			return ErrInvalidObjectLockMode
		}
		if cfg.Days <= 0 {
			return ErrInvalidObjectLockDays
		}
	}

	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.bucket.object_lock.set: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditBucketObjectLockSet,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"enabled":     cfg.Enabled,
			"mode":        string(cfg.Mode),
			"days":        cfg.Days,
		},
	})

	if cfg.Enabled {
		// Push the policy to SeaweedFS. A failed push fails the whole
		// action so the Lahijan row never claims a state the daemon
		// does not enforce.
		driverCfg := seaweedfs.ObjectLockConfig{
			Enabled: true,
			Mode:    toDriverMode(cfg.Mode),
			Days:    cfg.Days,
		}
		if err := s.provider.SetObjectLockConfiguration(ctx, row.Name, driverCfg); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return fmt.Errorf("storage.bucket.object_lock.set: %w", err)
		}
	}
	// When Enabled is false we deliberately do NOT push a config —
	// disabling object lock is not supported by AWS S3. The Postgres
	// cache is the only place the disable lands; this matches the
	// doc's "operator intent" semantics.

	// Translate to the row shape: NULL mode + NULL days when disabled.
	var modePtr *string
	var daysPtr *int32
	if cfg.Enabled {
		m := string(cfg.Mode)
		modePtr = &m
		days := cfg.Days
		daysPtr = &days
	}
	if err := s.repos.StorageBuckets.SetObjectLock(ctx, bucketID, cfg.Enabled, modePtr, daysPtr); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.bucket.object_lock.set: %w", err)
	}

	s.emitEvent(ctx, eventbus.S3BucketObjectLockSet, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"enabled":     cfg.Enabled,
		"mode":        string(cfg.Mode),
		"days":        cfg.Days,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// GetObjectLock returns the cached object-lock policy from Postgres.
// Does NOT round-trip to SeaweedFS — the Postgres row is the source of
// truth (ADR-0036 sub-decision C).
func (s *Service) GetObjectLock(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
) (ObjectLockConfig, error) {
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return ObjectLockConfig{}, ErrBucketNotFound
		}
		return ObjectLockConfig{}, fmt.Errorf("storage.bucket.object_lock.get: %w", err)
	}
	return ObjectLockConfigFromRow(row), nil
}
