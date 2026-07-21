// Package seaweedfs: objectlock.go wraps the AWS SDK S3 bucket-object-lock
// operations. Per ADR-0036 the storage service (WS-29) treats Postgres
// as the source of truth for the bucket-level object-lock policy
// (default mode + default retention days) and pushes the configuration
// to SeaweedFS on every change via this file.
//
// Object lock at the bucket level is a one-shot enable on AWS S3 (only
// allowed at bucket-create time). SeaweedFS' S3-compatible surface
// accepts PutObjectLockConfiguration at any time, so we expose a Set/Get
// pair and let the storage layer decide whether a transition is a no-op
// (no change in mode + days) or a real config update. Per the WS-29
// doc, per-object retention + legal-hold API endpoints are deferred;
// the bucket-default policy is enough for compliance MVP.
package seaweedfs

import (
	"context"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ObjectLockMode is the S3 object-lock retention mode. Mirrors the S3
// ObjectLockRetentionMode enum.
type ObjectLockMode string

const (
	// ObjectLockModeGovernance allows privileged users (with
	// s3:BypassGovernanceRetention) to override or remove the retention
	// setting on an object.
	ObjectLockModeGovernance ObjectLockMode = "GOVERNANCE"

	// ObjectLockModeCompliance enforces WORM: no one (including the
	// root account) can shorten or remove the retention period for the
	// duration of the lock.
	ObjectLockModeCompliance ObjectLockMode = "COMPLIANCE"
)

// ObjectLockConfig is the bucket-level object-lock policy. Mode + Days
// are the AWS DefaultRetention fields; Lahijan stores them on
// storage_buckets so the policy survives a daemon round-trip.
type ObjectLockConfig struct {
	// Enabled is true when object lock is enabled on the bucket. When
	// false the Mode + Days fields are ignored.
	Enabled bool

	// Mode is the default retention mode applied to every new object.
	// Empty when Enabled is false.
	Mode ObjectLockMode

	// Days is the default retention period in days. Zero when Enabled
	// is false.
	Days int32
}

// SetObjectLockConfiguration pushes the bucket-level object-lock policy
// to SeaweedFS. A nil return means the daemon picked up the policy.
//
// Disabling object lock after it has been enabled is not supported by
// AWS S3; the storage service treats a disable request as a no-op on
// the daemon side and only updates the Postgres cache. When Enabled is
// true the call writes the DefaultRetention block; when false the call
// is a no-op (we do not push an "Enabled = false" config because the
// daemon would reject it).
func (p *Provider) SetObjectLockConfiguration(ctx context.Context, bucket string, cfg ObjectLockConfig) error {
	ctx, span := startSpan(ctx, "object_lock.set", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: object_lock.set: %w", err)
	}
	if cfg.Enabled {
		if cfg.Mode != ObjectLockModeGovernance && cfg.Mode != ObjectLockModeCompliance {
			err := ErrBadRequest
			setStatus(span, err)
			return fmt.Errorf("seaweedfs: object_lock.set: %w: invalid mode %q", err, cfg.Mode)
		}
		if cfg.Days <= 0 {
			err := ErrBadRequest
			setStatus(span, err)
			return fmt.Errorf("seaweedfs: object_lock.set: %w: days must be > 0", err)
		}
		mode := awss3types.ObjectLockRetentionModeGovernance
		if cfg.Mode == ObjectLockModeCompliance {
			mode = awss3types.ObjectLockRetentionModeCompliance
		}
		if _, err := p.s3.PutObjectLockConfiguration(ctx, &awss3.PutObjectLockConfigurationInput{
			Bucket: strPtr(bucket),
			ObjectLockConfiguration: &awss3types.ObjectLockConfiguration{
				ObjectLockEnabled: awss3types.ObjectLockEnabledEnabled,
				Rule: &awss3types.ObjectLockRule{
					DefaultRetention: &awss3types.DefaultRetention{
						Mode: mode,
						Days: safeInt32Ptr(cfg.Days),
					},
				},
			},
		}); err != nil {
			translated := translateS3Err(err)
			setStatus(span, translated)
			return fmt.Errorf("seaweedfs: object_lock.set: %w", translated)
		}
	}
	// When Enabled is false we deliberately do NOT push a config —
	// disabling object lock is not supported by AWS S3 and SeaweedFS
	// rejects it. The Postgres cache update is enough; the Lahijan-
	// authored policy is the source of truth.

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.object_lock.set",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
		Metadata: asRawJSON(struct {
			Bucket  string         `json:"bucket"`
			Enabled bool           `json:"enabled"`
			Mode    ObjectLockMode `json:"mode,omitempty"`
			Days    int32          `json:"days,omitempty"`
		}{bucket, cfg.Enabled, cfg.Mode, cfg.Days}),
	})
	setStatus(span, nil)
	return nil
}

// GetObjectLockConfiguration reads the live bucket-level object-lock
// policy from SeaweedFS. Returns an empty (Enabled=false) config when
// the bucket has no object-lock configuration set (the SDK surfaces
// this as an ObjectLockConfigurationNotFoundError which we translate
// to an empty result).
func (p *Provider) GetObjectLockConfiguration(ctx context.Context, bucket string) (ObjectLockConfig, error) {
	ctx, span := startSpan(ctx, "object_lock.get", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return ObjectLockConfig{}, fmt.Errorf("seaweedfs: object_lock.get: %w", err)
	}

	out, err := p.s3.GetObjectLockConfiguration(ctx, &awss3.GetObjectLockConfigurationInput{
		Bucket: strPtr(bucket),
	})
	if err != nil {
		translated := translateS3Err(err)
		// ObjectLockConfigurationNotFoundError is the SDK's signal
		// that object lock was never enabled; treat as an empty
		// config so the caller's logic stays uniform.
		if isS3ErrorCode(err, "ObjectLockConfigurationNotFoundError") {
			setStatus(span, nil)
			return ObjectLockConfig{}, nil
		}
		setStatus(span, translated)
		return ObjectLockConfig{}, fmt.Errorf("seaweedfs: object_lock.get: %w", translated)
	}
	if out == nil || out.ObjectLockConfiguration == nil {
		setStatus(span, nil)
		return ObjectLockConfig{}, nil
	}
	cfg := configFromS3(out.ObjectLockConfiguration)
	setStatus(span, nil)
	return cfg, nil
}

// configFromS3 translates the AWS SDK ObjectLockConfiguration to the
// Lahijan value type. Returns Enabled=false when the configuration is
// missing the Rule.DefaultRetention block (matches the SDK's lenient
// interpretation).
func configFromS3(c *awss3types.ObjectLockConfiguration) ObjectLockConfig {
	if c == nil || c.Rule == nil || c.Rule.DefaultRetention == nil {
		return ObjectLockConfig{}
	}
	dr := c.Rule.DefaultRetention
	cfg := ObjectLockConfig{
		Enabled: c.ObjectLockEnabled == awss3types.ObjectLockEnabledEnabled,
		Days:    safeInt32Value(dr.Days),
	}
	switch dr.Mode {
	case awss3types.ObjectLockRetentionModeGovernance:
		cfg.Mode = ObjectLockModeGovernance
	case awss3types.ObjectLockRetentionModeCompliance:
		cfg.Mode = ObjectLockModeCompliance
	}
	return cfg
}

// safeInt32Value dereferences a *int32; zero when nil.
func safeInt32Value(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
