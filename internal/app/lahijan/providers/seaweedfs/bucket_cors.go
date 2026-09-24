// Package seaweedfs: bucket_cors.go writes each bucket's CORS configuration
// so the dashboard can upload/download objects with presigned URLs straight
// from the browser (ADR-0011: data goes to SeaweedFS directly).
//
// Without a bucket CORS configuration SeaweedFS treats the browser's
// unauthenticated OPTIONS preflight as an ordinary request and answers
// 403 AccessDenied, so every browser PUT fails as a "network error".
package seaweedfs

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// corsMaxAgeSeconds is how long browsers may cache a preflight answer.
const corsMaxAgeSeconds = 3000

// applyBucketCORS writes the dashboard CORS rule onto one bucket. A no-op
// when no origins are configured.
func (p *Provider) applyBucketCORS(ctx context.Context, bucket string) error {
	if len(p.corsOrigins) == 0 {
		return nil
	}
	_, err := p.s3.PutBucketCors(ctx, &awss3.PutBucketCorsInput{
		Bucket: aws.String(bucket),
		CORSConfiguration: &awss3types.CORSConfiguration{
			CORSRules: []awss3types.CORSRule{{
				AllowedOrigins: append([]string(nil), p.corsOrigins...),
				AllowedMethods: []string{"GET", "PUT", "HEAD", "POST", "DELETE"},
				AllowedHeaders: []string{"*"},
				ExposeHeaders:  []string{"ETag", "x-amz-version-id"},
				MaxAgeSeconds:  aws.Int32(corsMaxAgeSeconds),
			}},
		},
	})
	if err != nil {
		return fmt.Errorf("seaweedfs: bucket.cors %q: %w", bucket, translateS3Err(err))
	}
	return nil
}

// EnsureBucketCORS applies the dashboard CORS rule to every existing bucket.
// program.Start calls it once at boot so buckets created before CORS was
// configured (or before the origin changed) become browser-usable.
func (p *Provider) EnsureBucketCORS(ctx context.Context) error {
	if len(p.corsOrigins) == 0 {
		return nil
	}
	ctx, span := startSpan(ctx, "bucket.cors.ensure")
	defer span.End()
	buckets, err := p.ListBuckets(ctx)
	if err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: bucket.cors.ensure: %w", err)
	}
	var errs []error
	for _, b := range buckets {
		if cerr := p.applyBucketCORS(ctx, b.Name); cerr != nil {
			errs = append(errs, cerr)
		}
	}
	err = errors.Join(errs...)
	setStatus(span, err)
	return err
}
