// Package seaweedfs: objects.go wraps the AWS SDK S3 per-object operations
// the WS-29 lifecycle evaluator worker needs: DeleteObject (current +
// versioned), AbortMultipartUpload. The driver exposes thin wrappers so
// the storage worker can mock at the operations interface boundary
// (per ADR-0027) without dragging the full SDK type set into the
// worker's tests.
//
// These methods are intentionally minimal — they exist because the
// lifecycle worker needs to act on objects server-side. The user-facing
// data plane (PUT / GET / DELETE) stays direct from the user's S3
// client per ADR-0011.
package seaweedfs

import (
	"context"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// DeleteObject removes the current version of an object on an
// unversioned bucket, or creates a delete marker on a versioned bucket.
// When versionID is non-empty the call hard-deletes that specific
// version (used by the lifecycle evaluator's
// noncurrent_version_expiration action).
func (p *Provider) DeleteObject(ctx context.Context, bucket, key, versionID string) error {
	ctx, span := startSpan(ctx, "object.delete", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: object.delete: %w", err)
	}
	if key == "" {
		err := ErrBadRequest
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: object.delete: %w: key is required", err)
	}

	input := &awss3.DeleteObjectInput{
		Bucket: strPtr(bucket),
		Key:    strPtr(key),
	}
	if versionID != "" {
		input.VersionId = strPtr(versionID)
	}
	if _, err := p.s3.DeleteObject(ctx, input); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: object.delete: %w", translated)
	}
	setStatus(span, nil)
	return nil
}

// AbortMultipartUpload aborts an in-flight multipart upload. Used by
// the lifecycle evaluator's abort_incomplete_multipart action when an
// upload has been open longer than the rule's configured days.
func (p *Provider) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	ctx, span := startSpan(ctx, "multipart.abort", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: multipart.abort: %w", err)
	}
	if key == "" || uploadID == "" {
		err := ErrBadRequest
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: multipart.abort: %w: key + uploadID are required", err)
	}

	if _, err := p.s3.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   strPtr(bucket),
		Key:      strPtr(key),
		UploadId: strPtr(uploadID),
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: multipart.abort: %w", translated)
	}
	setStatus(span, nil)
	return nil
}
