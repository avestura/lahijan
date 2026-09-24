// Package seaweedfs: buckets.go wraps the AWS SDK S3 bucket operations.
// Every method takes a canonical bucket name (already validated by
// BucketName) and translates to an S3 SDK call. All methods open an OTel
// span; create/delete emit synthesized change events to the configured
// WASM bus.
package seaweedfs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// CreateBucketParams is the user-visible shape of a bucket-create call.
type CreateBucketParams struct {
	// Bucket is the canonical bucket name (output of BucketName). Required.
	Bucket string

	// TenantID is the optional tenant scope (used only for the synthesized
	// event payload). Empty when the caller is operating at the system
	// scope (e.g. an admin repair tool).
	TenantID string

	// Quota is the optional per-bucket quota applied at create time. Zero
	// value means "use the driver's default quota". Set to a non-zero
	// value with SizeMiB=0 to disable quota for this bucket.
	//
	// The presence of a *Quota pointer (rather than a value) lets the
	// caller distinguish "use default" (nil) from "explicitly no quota"
	// (non-nil zero value).
	Quota *QuotaSpec
}

// CreateBucket creates a bucket and returns the freshly-created Bucket
// record. Returns ErrAlreadyExists if the bucket already exists,
// ErrInvalidBucketName if the name violates the Lahijan naming convention,
// ErrBadRequest if the SDK rejects the request (typically a malformed name
// from SeaweedFS' perspective).
func (p *Provider) CreateBucket(ctx context.Context, params CreateBucketParams) (*Bucket, error) {
	ctx, span := startSpan(ctx, "bucket.create", bucketAttr(params.Bucket))
	defer span.End()

	if err := validateCanonicalBucketName(params.Bucket); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: bucket.create: %w", err)
	}

	if _, err := p.s3.CreateBucket(ctx, &awss3.CreateBucketInput{
		Bucket: strPtr(params.Bucket),
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return nil, fmt.Errorf("seaweedfs: bucket.create: %w", translated)
	}

	// Apply the per-bucket quota (if any). The Filer-side quota record is
	// what SeaweedFS' S3 server reads on every write; without it the
	// bucket is unbounded. When params.Quota is nil we use the driver
	// default; when it is an explicit *QuotaSpec with SizeMiB=0 we write
	// nothing (effectively unbounded).
	quota := p.defaultQuota
	if params.Quota != nil {
		quota = *params.Quota
	}
	if quota.SizeMiB > 0 || quota.FileCount > 0 {
		if qerr := p.writeBucketQuota(ctx, params.Bucket, quota); qerr != nil {
			// A failed quota write does NOT roll back the bucket; the
			// caller can retry SetBucketQuota. The bucket is usable but
			// unbounded until the write succeeds.
			setStatus(span, qerr)
		}
	}

	// Allow the dashboard origin to use presigned URLs from the browser.
	// Best effort like the quota: the bucket is usable from S3 clients
	// either way, and EnsureBucketCORS re-applies it at the next boot.
	if cerr := p.applyBucketCORS(ctx, params.Bucket); cerr != nil {
		setStatus(span, cerr)
	}

	created := &Bucket{
		Name:      params.Bucket,
		CreatedAt: time.Now().UTC(),
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.created",
		TenantID:   optionalStrPtr(params.TenantID),
		ActorType:  "system",
		ResourceID: strPtr(params.Bucket),
		Metadata:   asRawJSON(created),
	})
	setStatus(span, nil)
	return created, nil
}

// HeadBucket reports whether the bucket exists. Returns ErrNotFound when
// SeaweedFS responds with 404; any other SDK error is wrapped as-is.
func (p *Provider) HeadBucket(ctx context.Context, bucket string) error {
	ctx, span := startSpan(ctx, "bucket.head", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: bucket.head: %w", err)
	}
	if _, err := p.s3.HeadBucket(ctx, &awss3.HeadBucketInput{
		Bucket: strPtr(bucket),
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: bucket.head: %w", translated)
	}
	setStatus(span, nil)
	return nil
}

// ListBuckets lists every bucket the admin credentials can see. SeaweedFS
// returns ALL buckets in the cluster; the storage service (WS-16) filters
// the list to the calling tenant via looksLikeLahijanBucket + ParseBucketName.
func (p *Provider) ListBuckets(ctx context.Context) ([]Bucket, error) {
	ctx, span := startSpan(ctx, "bucket.list")
	defer span.End()

	out, err := p.s3.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return nil, fmt.Errorf("seaweedfs: bucket.list: %w", translated)
	}
	buckets := make([]Bucket, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		var created time.Time
		if b.CreationDate != nil {
			created = *b.CreationDate
		}
		buckets = append(buckets, Bucket{
			Name:      awsStringToStr(b.Name),
			CreatedAt: created,
		})
	}
	setStatus(span, nil)
	return buckets, nil
}

// DeleteBucket removes a bucket and all of its objects from SeaweedFS.
// SeaweedFS (unlike real AWS S3) accepts DELETE on non-empty buckets; we
// preserve that behaviour and document it as the Lahijan semantic. The
// storage service may pre-check emptiness when the user opts into the
// safer "only if empty" mode (WS-16).
//
// Returns ErrNotFound if the bucket does not exist.
func (p *Provider) DeleteBucket(ctx context.Context, bucket string) error {
	ctx, span := startSpan(ctx, "bucket.delete", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: bucket.delete: %w", err)
	}
	if _, err := p.s3.DeleteBucket(ctx, &awss3.DeleteBucketInput{
		Bucket: strPtr(bucket),
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: bucket.delete: %w", translated)
	}
	// Best-effort: also remove the per-bucket quota record so a future
	// CreateBucket with the same name does not pick up a stale quota.
	// Errors are swallowed because the bucket delete has already
	// committed; a leftover quota record is harmless.
	_ = p.filer.DeleteMetadata(ctx, bucketQuotaPath(bucket))

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.deleted",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
	})
	setStatus(span, nil)
	return nil
}

// BucketARN returns the canonical ARN for a Lahijan bucket (per ADR-0011).
// The bucket name MUST already be valid; this function panics on invalid
// input because it is a pure derivation the caller should have validated
// before calling.
func BucketARN(bucket string) string {
	if err := validateCanonicalBucketName(bucket); err != nil {
		// Pure derivation; the caller has validated by this point.
		panic(fmt.Sprintf("seaweedfs: BucketARN called with invalid name %q: %v", bucket, err))
	}
	// arn:aws:s3:::<bucket>
	return arn.ARN{
		Partition: "aws",
		Service:   "s3",
		Resource:  bucket,
	}.String()
}

// validateCanonicalBucketName is the precondition check every bucket
// method runs before issuing the S3 SDK call. It enforces the Lahijan
// naming convention (ADR-0011) so SeaweedFS never sees a malformed name.
//
// We allow already-formatted "<tenant-uuid>-<slug>" names. Names that
// don't match the Lahijan compound shape are still allowed when they
// pass the AWS rules (3-63 chars, lowercase alphanumeric + '-'); this is
// the escape hatch for an admin repairing a pre-Lahijan bucket.
func validateCanonicalBucketName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: bucket name is empty", ErrInvalidBucketName)
	}
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("%w: bucket name %q length %d out of range [3,63]",
			ErrInvalidBucketName, name, len(name))
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.'
		if !ok {
			return fmt.Errorf("%w: bucket name %q contains illegal char at %d",
				ErrInvalidBucketName, name, i)
		}
	}
	// AWS requires lowercase; we do too.
	if name != lowerASCII(name) {
		return fmt.Errorf("%w: bucket name %q must be lowercase", ErrInvalidBucketName, name)
	}
	if name[0] == '-' || name[0] == '.' || name[len(name)-1] == '-' || name[len(name)-1] == '.' {
		return fmt.Errorf("%w: bucket name %q must start + end with alphanumeric",
			ErrInvalidBucketName, name)
	}
	return nil
}

// lowerASCII returns the ASCII-lowercase form of s without allocating for
// non-ASCII content. Used by validateCanonicalBucketName.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// optionalStrPtr returns a pointer to s when s is non-empty; nil otherwise.
// Used for the optional TenantID field on synthesized events.
func optionalStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// awsStringToStr unwraps an aws.String; empty when nil.
func awsStringToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// translateS3Err converts an AWS SDK S3 error into one of the driver's
// sentinel errors so callers can errors.Is at the boundary. Unknown
// errors are wrapped as ErrOperationFailed.
//
// The AWS SDK v2 surfaces HTTP status codes via the smithy-go generic
// APIError type (ErrorCode / ErrorMessage); the HTTP status code is
// extracted via the optional httpresponseError interface implemented by
// the concrete SDK error types.
func translateS3Err(err error) error {
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		// Not an SDK error — wrap with the ErrOperationFailed sentinel
		// and the original error so callers can errors.Is either.
		return fmt.Errorf("%w: %w", ErrOperationFailed, err)
	}
	switch apiErr.ErrorCode() {
	case "BucketAlreadyExists", "BucketAlreadyOwnedByYou":
		return fmt.Errorf("%w: %s", ErrAlreadyExists, apiErr.ErrorMessage())
	case "NoSuchBucket":
		return fmt.Errorf("%w: %s", ErrNotFound, apiErr.ErrorMessage())
	case "AccessDenied", "Forbidden":
		return fmt.Errorf("%w: %s", ErrForbidden, apiErr.ErrorMessage())
	case "InvalidBucketName":
		return fmt.Errorf("%w: %s", ErrInvalidBucketName, apiErr.ErrorMessage())
	case "QuotaExceeded", "InsufficientStorage":
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, apiErr.ErrorMessage())
	}
	// Fallback to status-code mapping when the response carries one.
	if status := httpStatusFromErr(err); status > 0 {
		switch status {
		case 404:
			return fmt.Errorf("%w: %s", ErrNotFound, apiErr.ErrorMessage())
		case 403:
			return fmt.Errorf("%w: %s", ErrForbidden, apiErr.ErrorMessage())
		case 401:
			return fmt.Errorf("%w: %s", ErrUnauthenticated, apiErr.ErrorMessage())
		case 400:
			return fmt.Errorf("%w: %s", ErrBadRequest, apiErr.ErrorMessage())
		case 409:
			return fmt.Errorf("%w: %s", ErrConflict, apiErr.ErrorMessage())
		case 507:
			return fmt.Errorf("%w: %s", ErrQuotaExceeded, apiErr.ErrorMessage())
		}
		if status >= 400 && status < 500 {
			return fmt.Errorf("%w: %s", ErrOperationFailed, apiErr.ErrorMessage())
		}
		return fmt.Errorf("%w: http %d: %s", ErrOperationFailed, status, apiErr.ErrorMessage())
	}
	return fmt.Errorf("%w: %s", ErrOperationFailed, apiErr.ErrorMessage())
}

// httpStatusFromErr extracts the HTTP status code from a smithy-go error
// via the httpresponseError interface that the SDK attaches to the
// concrete error types. Returns 0 when not present.
func httpStatusFromErr(err error) int {
	type statusCarrier interface {
		HTTPStatusCode() int
	}
	var sc statusCarrier
	if errors.As(err, &sc) {
		return sc.HTTPStatusCode()
	}
	return 0
}

// isS3ErrorCode reports whether err carries the given AWS SDK error
// code. Used by the lifecycle / objectlock getters to translate the
// "not configured yet" SDK responses (NoSuchLifecycleConfiguration,
// ObjectLockConfigurationNotFoundError) into empty results without
// surfacing them as errors.
func isS3ErrorCode(err error, code string) bool {
	if err == nil || code == "" {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorCode() == code
}
