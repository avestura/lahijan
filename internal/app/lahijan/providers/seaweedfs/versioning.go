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
	"time"

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
	case VersioningStatusUnversioned:
		return "" // unversioned → empty so the SDK omits the field
	}
	// Unknown values are normalised to empty (defensive; the enum
	// constraint lives at the storage service boundary).
	return ""
}

// fromS3VersioningStatus maps the AWS SDK enum (or empty string) to the
// Lahijan-normalised status. Used by GetBucketVersioning.
func fromS3VersioningStatus(s awss3types.BucketVersioningStatus) VersioningStatus {
	switch s {
	case awss3types.BucketVersioningStatusEnabled:
		return VersioningStatusEnabled
	case awss3types.BucketVersioningStatusSuspended:
		return VersioningStatusSuspended
	default:
		return VersioningStatusUnversioned
	}
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

// ObjectVersion is the driver-level value type for a single object
// version. The service layer consumes this instead of the raw AWS SDK
// type so it does not need to import the SDK transitively.
type ObjectVersion struct {
	// Key is the object key.
	Key string

	// VersionID is the S3-assigned version identifier. Empty when the
	// bucket is unversioned.
	VersionID string

	// IsLatest is true when this is the current version of the key.
	IsLatest bool

	// IsDeleteMarker is true when this entry represents a delete
	// (logical delete on a versioned bucket) rather than a real object
	// version. The body is nil in that case.
	IsDeleteMarker bool

	// Size is the object size in bytes. Zero for delete markers.
	Size int64

	// LastModified is the UTC timestamp the version was created.
	LastModified time.Time
}

// ObjectVersionsPage is the page returned by ListObjectVersions. The
// service layer uses the cursors when paginating; the worker uses the
// versions to evaluate lifecycle rules.
type ObjectVersionsPage struct {
	// Versions is the list of object versions on this page (does not
	// include delete markers).
	Versions []ObjectVersion

	// DeleteMarkers is the list of delete markers on this page.
	DeleteMarkers []ObjectVersion

	// NextKeyMarker is the pagination cursor for the next page; empty
	// when this is the last page.
	NextKeyMarker string

	// NextVersionIDMarker is the pagination cursor for the next page;
	// empty when this is the last page.
	NextVersionIDMarker string

	// IsTruncated is true when the bucket has more versions to list.
	IsTruncated bool
}

// ListObjectVersions returns a page of object versions in the bucket.
// Used by the storage service's "list versions" endpoint + by the
// lifecycle evaluator worker when applying noncurrent-version rules.
// keyMarker + versionIdMarker are the pagination cursors from the
// previous page; pass empty strings for the first page.
func (p *Provider) ListObjectVersions(
	ctx context.Context,
	bucket, prefix, keyMarker, versionIDMarker string,
	maxKeys int32,
) (*ObjectVersionsPage, error) {
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
		Bucket:  strPtr(bucket),
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
	page := &ObjectVersionsPage{
		Versions:      make([]ObjectVersion, 0, len(out.Versions)),
		DeleteMarkers: make([]ObjectVersion, 0, len(out.DeleteMarkers)),
	}
	for _, v := range out.Versions {
		page.Versions = append(page.Versions, objectVersionFromS3(v))
	}
	for _, d := range out.DeleteMarkers {
		page.DeleteMarkers = append(page.DeleteMarkers, deleteMarkerFromS3(d))
	}
	if out.IsTruncated != nil {
		page.IsTruncated = *out.IsTruncated
	}
	if out.NextKeyMarker != nil {
		page.NextKeyMarker = *out.NextKeyMarker
	}
	if out.NextVersionIdMarker != nil {
		page.NextVersionIDMarker = *out.NextVersionIdMarker
	}
	setStatus(span, nil)
	return page, nil
}

// objectVersionFromS3 translates the SDK ObjectVersion to the driver
// value type.
func objectVersionFromS3(v awss3types.ObjectVersion) ObjectVersion {
	out := ObjectVersion{}
	if v.Key != nil {
		out.Key = *v.Key
	}
	if v.VersionId != nil {
		out.VersionID = *v.VersionId
	}
	if v.IsLatest != nil {
		out.IsLatest = *v.IsLatest
	}
	if v.Size != nil {
		out.Size = *v.Size
	}
	if v.LastModified != nil {
		out.LastModified = *v.LastModified
	}
	return out
}

// deleteMarkerFromS3 translates the SDK DeleteMarkerEntry to the driver
// value type with IsDeleteMarker=true.
func deleteMarkerFromS3(d awss3types.DeleteMarkerEntry) ObjectVersion {
	out := ObjectVersion{IsDeleteMarker: true}
	if d.Key != nil {
		out.Key = *d.Key
	}
	if d.VersionId != nil {
		out.VersionID = *d.VersionId
	}
	if d.IsLatest != nil {
		out.IsLatest = *d.IsLatest
	}
	if d.LastModified != nil {
		out.LastModified = *d.LastModified
	}
	return out
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
			Bucket    string `json:"bucket"`
			Key       string `json:"key"`
			VersionID string `json:"version_id,omitempty"`
		}{bucket, key, versionID}),
	})
	setStatus(span, nil)
	return nil
}
