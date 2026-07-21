// Package seaweedfs: versioning.go wraps the AWS SDK S3 bucket-versioning
// operations. Per ADR-0036 the storage service (WS-29) treats Postgres
// as the source of truth and pushes the configuration to SeaweedFS on
// every change via this file. The driver never carries tenant_id; the
// storage service resolves the canonical bucket name (ADR-0011) and
// hands it to these methods.
//
// All methods open an OTel span and emit a synthesized change event on
// success. Errors are translated to the package sentinel set via
// translateS3Err so callers can errors.Is at the boundary.
package seaweedfs

import (
	"context"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// VersioningStatus is the bucket versioning state. Mirrors the S3
// BucketVersioningStatus enum but uses lowercase Lahijan-normalised
// values so the storage_buckets.versioning_status column matches the
// rest of the codebase.
type VersioningStatus string

const (
	// VersioningStatusUnversioned is the default for new buckets. S3
	// does not have an "unversioned" enum value (it returns an empty
	// string when versioning has never been enabled); we normalise to
	// "unversioned" so the Lahijan-side row + UI have a stable label.
	VersioningStatusUnversioned VersioningStatus = "unversioned"

	// VersioningStatusEnabled matches S3's "Enabled": every PUT creates
	// a new object version; every DELETE creates a delete marker.
	VersioningStatusEnabled VersioningStatus = "enabled"

	// VersioningStatusSuspended matches S3's "Suspended": existing
	// versions stay, no new versions are created, PUTs overwrite the
	// current version in place.
	VersioningStatusSuspended VersioningStatus = "suspended"
)

// toS3VersioningStatus maps the Lahijan-normalised status to the AWS
// SDK enum. The unversioned case maps to nil so SeaweedFS receives no
// Status field (mirrors the SDK's own GetBucketVersioning response
// when versioning was never enabled).
func toS3VersioningStatus(s VersioningStatus) awss3types.BucketVersioningStatus {
	switch s {
	case VersioningStatusEnabled:
		return awss3types.BucketVersioningStatusEnabled
	case VersioningStatusSuspended:
		return awss3types.BucketVersioningStatusSuspended
	}
	return "" // unversioned → empty so the SDK omits the field
}

// fromS3VersioningStatus maps the AWS SDK enum (or empty string) to the
// Lahijan-normalised status. Used by GetBucketVersioning.
func fromS3VersioningStatus(s awss3types.BucketVersioningStatus) VersioningStatus {
	switch s {
	case awss3types.BucketVersioningStatusEnabled:
		return VersioningStatusEnabled
	case awss3types.BucketVersioningStatusSuspended:
		return VersioningStatusSuspended
	}
	return VersioningStatusUnversioned
}

// SetBucketVersioning pushes the versioning status to SeaweedFS. A nil
// return means the daemon picked up the configuration; the storage
// service separately persists the status to Postgres so the next
// GetBucketVersioning returns without a daemon round-trip.
//
// Returns ErrInvalidBucketName when the bucket name fails validation.
// The call does NOT pre-check the bucket's existence — SeaweedFS will
// surface NoSuchBucket on the SDK call.
func (p *Provider) SetBucketVersioning(ctx context.Context, bucket string, status VersioningStatus) error {
	ctx, span := startSpan(ctx, "versioning.set", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: versioning.set: %w", err)
	}

	s3Status := toS3VersioningStatus(status)
	if _, err := p.s3.PutBucketVersioning(ctx, &awss3.PutBucketVersioningInput{
		Bucket: strPtr(bucket),
		VersioningConfiguration: &awss3types.VersioningConfiguration{
			Status: s3Status,
		},
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: versioning.set: %w", translated)
	}

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.versioning.set",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
		Metadata: asRawJSON(struct {
			Bucket string           `json:"bucket"`
			Status VersioningStatus `json:"status"`
		}{bucket, status}),
	})
	setStatus(span, nil)
	return nil
}

// GetBucketVersioning reads the live versioning status from SeaweedFS.
// Returns VersioningStatusUnversioned when the bucket has never been
// versioned (S3 returns an empty Status field in that case).
func (p *Provider) GetBucketVersioning(ctx context.Context, bucket string) (VersioningStatus, error) {
	ctx, span := startSpan(ctx, "versioning.get", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return VersioningStatusUnversioned, fmt.Errorf("seaweedfs: versioning.get: %w", err)
	}

	out, err := p.s3.GetBucketVersioning(ctx, &awss3.GetBucketVersioningInput{
		Bucket: strPtr(bucket),
	})
	if err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return VersioningStatusUnversioned, fmt.Errorf("seaweedfs: versioning.get: %w", translated)
	}
	status := VersioningStatusUnversioned
	if out != nil {
		status = fromS3VersioningStatus(out.Status)
	}
	setStatus(span, nil)
	return status, nil
}

// ListObjectVersions returns a page of object versions in the bucket.
// Used by the storage service's "list versions" endpoint + by the
// lifecycle evaluator worker when applying noncurrent-version rules.
// keyMarker + versionIdMarker are the pagination cursors from the
// previous page; pass empty strings for the first page.
//
// Returns the raw SDK types because the caller (storage service) needs
// the full version metadata (version id, is_latest, delete_marker,
// last_modified). Wrapping the SDK type would just add boilerplate.
func (p *Provider) ListObjectVersions(
	ctx context.Context,
	bucket, prefix, keyMarker, versionIDMarker string,
	maxKeys int32,
) (*awss3.ListObjectVersionsOutput, error) {
	ctx, span := startSpan(ctx, "versioning.list", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: versioning.list: %w", err)
	}
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	input := &awss3.ListObjectVersionsInput{
		Bucket: strPtr(bucket),
		MaxKeys: &maxKeys,
	}
	if prefix != "" {
		input.Prefix = &prefix
	}
	if keyMarker != "" {
		input.KeyMarker = &keyMarker
	}
	if versionIDMarker != "" {
		input.VersionIdMarker = &versionIDMarker
	}

	out, err := p.s3.ListObjectVersions(ctx, input)
	if err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return nil, fmt.Errorf("seaweedfs: versioning.list: %w", translated)
	}
	setStatus(span, nil)
	return out, nil
}

// RestoreObjectVersion copies a noncurrent version of an object to be
// the current version. Implemented as a server-side copy from the same
// bucket+key+version to itself (the S3 idiom for "undelete"). Used by
// the storage service's restore-deleted-version privileged action.
//
// The versionID parameter is REQUIRED when the bucket is versioned; an
// empty versionID restores the latest noncurrent version (best effort,
// not atomic in the S3 sense).
func (p *Provider) RestoreObjectVersion(
	ctx context.Context,
	bucket, key, versionID string,
) error {
	ctx, span := startSpan(ctx, "versioning.restore", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: versioning.restore: %w", err)
	}
	if key == "" {
		err := ErrBadRequest
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: versioning.restore: %w: key is required", err)
	}

	// Server-side copy of the version to itself: the source carries the
	// versionId query param; the destination is the same key without a
	// versionId, which makes it the new current version. This is the S3
	// idiom for "restore from noncurrent version" and works on every S3-
	// compatible daemon.
	source := bucket + "/" + key
	if versionID != "" {
		source += "?versionId=" + versionID
	}
	if _, err := p.s3.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     strPtr(bucket),
		Key:        strPtr(key),
		CopySource: strPtr(source),
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: versioning.restore: %w", translated)
	}

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.object.restored",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
		Metadata: asRawJSON(struct {
			Bucket     string `json:"bucket"`
			Key        string `json:"key"`
			VersionID  string `json:"version_id,omitempty"`
		}{bucket, key, versionID}),
	})
	setStatus(span, nil)
	return nil
}
