// Package seaweedfs: presign.go generates SigV4-signed URLs for limited-time
// direct access to a single object. Per ADR-0011 the data plane is direct
// from the user's S3 client to SeaweedFS; pre-signed URLs are how a Lahijan
// plugin or the dashboard hands a user a temporary download/upload link
// without proxying the bytes through Lahijan.
//
// The signing itself is delegated to the AWS SDK PresignClient (per
// ADR-0027). The SDK signs against the same admin credentials the driver
// uses for the rest of the S3 surface; in practice Lahijan mints
// per-user credentials and signs pre-signed URLs with those when a user
// requests one, but the SDK + endpoint configuration is the same.
package seaweedfs

import (
	"context"
	"errors"
	"fmt"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// PresignGetObject generates a pre-signed URL for downloading a single
// object. ttl is clamped to the driver default when zero. Returns
// ErrInvalidBucketName when the bucket name fails validation; an empty
// key is allowed (the URL then targets the bucket root).
func (p *Provider) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration) (*PresignResult, error) {
	ctx, span := startSpan(ctx, "presign.get", bucketAttr(bucket))
	defer span.End()

	res, err := p.presignHelper(ctx, bucket, key, ttl, false)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: presign.get: %w", err)
	}
	setStatus(span, nil)
	return res, nil
}

// PresignPutObject generates a pre-signed URL for uploading a single
// object. The caller is responsible for setting any desired object
// metadata (Content-Type, etc.) on the actual PUT request the user
// issues against the URL — Lahijan does not encode that into the
// signature.
func (p *Provider) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration) (*PresignResult, error) {
	ctx, span := startSpan(ctx, "presign.put", bucketAttr(bucket))
	defer span.End()

	res, err := p.presignHelper(ctx, bucket, key, ttl, true)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: presign.put: %w", err)
	}
	setStatus(span, nil)
	return res, nil
}

// presignHelper is the shared implementation. isPut selects between
// PresignPutObject and PresignGetObject on the underlying presign client.
func (p *Provider) presignHelper(ctx context.Context, bucket, key string, ttl time.Duration, isPut bool) (*PresignResult, error) {
	if err := validateCanonicalBucketName(bucket); err != nil {
		return nil, err
	}
	if p.presign == nil {
		return nil, errors.New("seaweedfs: presign client is not configured")
	}
	if ttl <= 0 {
		ttl = p.defaultPresignTTL
	}
	expiresAt := time.Now().Add(ttl).UTC()

	if isPut {
		req, err := p.presign.PresignPutObject(ctx, &awss3.PutObjectInput{
			Bucket: strPtr(bucket),
			Key:    strPtr(key),
		}, func(o *awss3.PresignOptions) {
			o.Expires = ttl
		})
		if err != nil {
			return nil, fmt.Errorf("sdk presign: %w", err)
		}
		return &PresignResult{
			URL:       req.URL,
			Method:    req.Method,
			Bucket:    bucket,
			Key:       key,
			ExpiresAt: expiresAt,
		}, nil
	}
	req, err := p.presign.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: strPtr(bucket),
		Key:    strPtr(key),
	}, func(o *awss3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return nil, fmt.Errorf("sdk presign: %w", err)
	}
	return &PresignResult{
		URL:       req.URL,
		Method:    req.Method,
		Bucket:    bucket,
		Key:       key,
		ExpiresAt: expiresAt,
	}, nil
}

// DefaultPresignTTL exposes the driver's default presign TTL so the
// storage service can surface it in API responses.
func (p *Provider) DefaultPresignTTL() time.Duration {
	return p.defaultPresignTTL
}
