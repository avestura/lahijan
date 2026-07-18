// Package seaweedfs: quotas.go enforces per-bucket quotas via the SeaweedFS
// Filer. The S3 server reads /etc/seaweedfs/buckets/<bucket>/quota.json
// on every write and rejects requests that would exceed the limit.
//
// Per WS-13 scope, quotas are enforced in two layers:
//   - **Backend (this file):** SeaweedFS rejects PUTs once the bucket size
//     or object count exceeds the configured ceiling. This is the hard
//     ceiling that protects the cluster from a runaway upload.
//   - **Metering (WS-17):** the billing subsystem tracks per-tenant usage
//     and bills based on consumed capacity. That path is independent of
//     quotas and works whether or not a backend quota is set.
package seaweedfs

import (
	"context"
	"errors"
	"fmt"
)

// SetBucketQuota writes the given quota record for the bucket. A zero-valued
// QuotaSpec clears the existing quota. Returns ErrInvalidBucketName when
// the bucket name fails validation; the call does NOT pre-check the
// bucket's existence — SeaweedFS accepts the Filer write regardless and
// the S3 server picks it up when the bucket is created later.
func (p *Provider) SetBucketQuota(ctx context.Context, bucket string, quota QuotaSpec) error {
	ctx, span := startSpan(ctx, "quota.set", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: quota.set: %w", err)
	}
	if quota.SizeMiB < 0 || quota.FileCount < 0 {
		err := errors.New("seaweedfs: quota dimensions must not be negative")
		setStatus(span, err)
		return err
	}

	rec := quotaRecord(quota)
	body, err := jsonMarshal(rec)
	if err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: quota.set: marshal: %w", err)
	}

	if err := p.filer.PutMetadata(ctx, bucketQuotaPath(bucket), body); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: quota.set: %w", err)
	}

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.quota.set",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
		Metadata:   asRawJSON(rec),
	})
	setStatus(span, nil)
	return nil
}

// GetBucketQuota reads the current quota record for the bucket. Returns
// a zero-valued QuotaSpec (no quota) when the bucket has no record set.
func (p *Provider) GetBucketQuota(ctx context.Context, bucket string) (QuotaSpec, error) {
	ctx, span := startSpan(ctx, "quota.get", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return QuotaSpec{}, fmt.Errorf("seaweedfs: quota.get: %w", err)
	}
	body, err := p.filer.GetMetadata(ctx, bucketQuotaPath(bucket))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// No quota set → unbounded.
			setStatus(span, nil)
			return QuotaSpec{}, nil
		}
		setStatus(span, err)
		return QuotaSpec{}, fmt.Errorf("seaweedfs: quota.get: %w", err)
	}
	var rec quotaRecord
	if err := jsonUnmarshal(body, &rec); err != nil {
		setStatus(span, err)
		return QuotaSpec{}, fmt.Errorf("seaweedfs: quota.get: decode: %w", err)
	}
	setStatus(span, nil)
	return QuotaSpec(rec), nil
}

// ClearBucketQuota removes the quota record for the bucket, effectively
// making it unbounded. Idempotent.
func (p *Provider) ClearBucketQuota(ctx context.Context, bucket string) error {
	ctx, span := startSpan(ctx, "quota.clear", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: quota.clear: %w", err)
	}
	if err := p.filer.DeleteMetadata(ctx, bucketQuotaPath(bucket)); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: quota.clear: %w", err)
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.quota.cleared",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
	})
	setStatus(span, nil)
	return nil
}

// writeBucketQuota is the internal helper used by CreateBucket to apply
// the per-bucket default quota. It does NOT emit an event because the
// parent CreateBucket already emits storage.bucket.created and a second
// storage.bucket.quota.set would be redundant noise.
func (p *Provider) writeBucketQuota(ctx context.Context, bucket string, quota QuotaSpec) error {
	rec := quotaRecord(quota)
	body, err := jsonMarshal(rec)
	if err != nil {
		return fmt.Errorf("marshal quota: %w", err)
	}
	if err := p.filer.PutMetadata(ctx, bucketQuotaPath(bucket), body); err != nil {
		return err
	}
	return nil
}

// bucketQuotaPath is the Filer metadata path for the given bucket's quota
// record. Per-bucket metadata lives under /etc/seaweedfs/buckets/<bucket>/.
func bucketQuotaPath(bucket string) string {
	return "/etc/seaweedfs/buckets/" + bucket + "/quota.json"
}

// jsonMarshal is a stable name for encoding/json.Marshal so call sites
// can be located by name.
func jsonMarshal(v any) ([]byte, error) {
	return jsonMarshalImpl(v)
}

// jsonUnmarshal is a stable name for encoding/json.Unmarshal.
func jsonUnmarshal(body []byte, v any) error {
	return jsonUnmarshalImpl(body, v)
}
