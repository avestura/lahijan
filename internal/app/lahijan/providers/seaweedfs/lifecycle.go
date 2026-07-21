// Package seaweedfs: lifecycle.go wraps the AWS SDK S3 bucket-lifecycle
// operations. Per ADR-0036 the storage service (WS-29) treats Postgres
// as the source of truth for the per-bucket lifecycle rule set and
// pushes the configuration to SeaweedFS on every change via this file.
//
// The lifecycle evaluator River worker (storage.lifecycle.evaluate)
// ALSO enforces the rules independently via the S3 SDK so the platform
// works even on a SeaweedFS build that does not yet honour
// PutBucketLifecycleConfiguration natively (per the WS-29 doc's
// "Notes" caveat).
package seaweedfs

import (
	"context"
	"fmt"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// LifecycleAction is the action a lifecycle rule performs. Mirrors the
// subset of the S3 lifecycle action vocabulary Lahijan exposes; matches
// the storage_lifecycle_rules.action CHECK constraint 1:1.
type LifecycleAction string

const (
	// LifecycleActionExpiration deletes the current version of every
	// object the rule matches after N days (or on a fixed date). On a
	// versioned bucket this creates a delete marker rather than a hard
	// delete; on an unversioned bucket it hard-deletes the object.
	LifecycleActionExpiration LifecycleAction = "expiration"

	// LifecycleActionNoncurrentVersionExpiration deletes noncurrent
	// versions of every object the rule matches after N days. No-op on
	// an unversioned bucket (no noncurrent versions exist).
	LifecycleActionNoncurrentVersionExpiration LifecycleAction = "noncurrent_version_expiration"

	// LifecycleActionAbortIncompleteMultipart aborts multipart uploads
	// that have not been completed within N days of initiation.
	LifecycleActionAbortIncompleteMultipart LifecycleAction = "abort_incomplete_multipart"

	// LifecycleActionTransition moves the current version to a lower-
	// tier storage class after N days (or on a fixed date). SeaweedFS'
	// tier support varies by build; the worker's transition path is a
	// no-op when the destination class is unknown to the daemon.
	LifecycleActionTransition LifecycleAction = "transition"
)

// LifecycleRule is the driver-level value type for a single S3 lifecycle
// rule. The storage service translates between this and the Postgres
// storage_lifecycle_rules row.
type LifecycleRule struct {
	// ID is the user-provided rule identifier within the bucket.
	// Required; matches the S3 LifecycleRule.ID field.
	ID string

	// Enabled is true when the rule should be considered by the
	// evaluator. Mirrors S3's LifecycleRule.Status = "Enabled".
	Enabled bool

	// Action is the lifecycle action this rule performs.
	Action LifecycleAction

	// Days is the age in days after which the rule fires. Mutually
	// exclusive with Date. Zero means "use Date".
	Days int32

	// Date is the absolute date at which the rule fires (RFC 3339
	// calendar date, "yyyy-mm-dd"). Empty means "use Days".
	Date string

	// StorageClass is the destination tier for transition rules. Empty
	// for non-transition rules.
	StorageClass string

	// Prefix is the key-prefix filter. Empty means "whole bucket".
	Prefix string
}

// SetBucketLifecycle pushes the given rule set to SeaweedFS. The
// storage service persists the rules in Postgres + calls this method
// so the daemon honours the policy natively when it can. A nil return
// means the daemon picked up the configuration.
//
// An empty rule set resets the bucket's lifecycle policy entirely
// (DeleteBucketLifecycle).
func (p *Provider) SetBucketLifecycle(ctx context.Context, bucket string, rules []LifecycleRule) error {
	ctx, span := startSpan(ctx, "lifecycle.set", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: lifecycle.set: %w", err)
	}

	// Empty rule set = clear the policy.
	if len(rules) == 0 {
		if _, err := p.s3.DeleteBucketLifecycle(ctx, &awss3.DeleteBucketLifecycleInput{
			Bucket: strPtr(bucket),
		}); err != nil {
			translated := translateS3Err(err)
			setStatus(span, translated)
			return fmt.Errorf("seaweedfs: lifecycle.set: clear: %w", translated)
		}
		setStatus(span, nil)
		return nil
	}

	s3Rules := make([]awss3types.LifecycleRule, 0, len(rules))
	for _, r := range rules {
		s3Rules = append(s3Rules, toS3LifecycleRule(r))
	}

	if _, err := p.s3.PutBucketLifecycleConfiguration(ctx, &awss3.PutBucketLifecycleConfigurationInput{
		Bucket: strPtr(bucket),
		LifecycleConfiguration: &awss3types.BucketLifecycleConfiguration{
			Rules: s3Rules,
		},
	}); err != nil {
		translated := translateS3Err(err)
		setStatus(span, translated)
		return fmt.Errorf("seaweedfs: lifecycle.set: %w", translated)
	}

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.bucket.lifecycle.set",
		ActorType:  "system",
		ResourceID: strPtr(bucket),
		Metadata: asRawJSON(struct {
			Bucket    string `json:"bucket"`
			RuleCount int    `json:"rule_count"`
		}{bucket, len(rules)}),
	})
	setStatus(span, nil)
	return nil
}

// GetBucketLifecycle reads the live lifecycle rule set from SeaweedFS.
// Returns an empty slice when the bucket has no lifecycle configuration
// (the SDK surfaces this as a NoSuchLifecycleConfiguration error which
// we translate to an empty result).
func (p *Provider) GetBucketLifecycle(ctx context.Context, bucket string) ([]LifecycleRule, error) {
	ctx, span := startSpan(ctx, "lifecycle.get", bucketAttr(bucket))
	defer span.End()

	if err := validateCanonicalBucketName(bucket); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: lifecycle.get: %w", err)
	}

	out, err := p.s3.GetBucketLifecycleConfiguration(ctx, &awss3.GetBucketLifecycleConfigurationInput{
		Bucket: strPtr(bucket),
	})
	if err != nil {
		translated := translateS3Err(err)
		// NoSuchLifecycleConfiguration is the SDK's signal that no
		// lifecycle policy exists yet; treat as an empty rule set so
		// the caller's logic stays uniform.
		if isS3ErrorCode(err, "NoSuchLifecycleConfiguration") {
			setStatus(span, nil)
			return []LifecycleRule{}, nil
		}
		setStatus(span, translated)
		return nil, fmt.Errorf("seaweedfs: lifecycle.get: %w", translated)
	}
	if out == nil {
		setStatus(span, nil)
		return []LifecycleRule{}, nil
	}
	rules := make([]LifecycleRule, 0, len(out.Rules))
	for _, r := range out.Rules {
		rules = append(rules, fromS3LifecycleRule(r))
	}
	setStatus(span, nil)
	return rules, nil
}

// toS3LifecycleRule translates the Lahijan value type to the AWS SDK
// LifecycleRule. The mapping is one-to-one for the fields Lahijan
// supports; tag-based filters and multiple actions per rule are out of
// scope for the MVP per the WS-29 doc.
func toS3LifecycleRule(r LifecycleRule) awss3types.LifecycleRule {
	status := awss3types.ExpirationStatusDisabled
	if r.Enabled {
		status = awss3types.ExpirationStatusEnabled
	}
	out := awss3types.LifecycleRule{
		ID:     safeStrPtr(r.ID),
		Status: status,
		Filter: &awss3types.LifecycleRuleFilter{},
	}
	if r.Prefix != "" {
		out.Filter = &awss3types.LifecycleRuleFilter{
			Prefix: safeStrPtr(r.Prefix),
		}
	}
	switch r.Action {
	case LifecycleActionExpiration:
		exp := &awss3types.LifecycleExpiration{}
		if r.Days > 0 {
			exp.Days = safeInt32Ptr(r.Days)
		}
		if r.Date != "" {
			exp.Date = safeTimePtr(r.Date)
		}
		out.Expiration = exp
	case LifecycleActionNoncurrentVersionExpiration:
		nve := &awss3types.NoncurrentVersionExpiration{}
		if r.Days > 0 {
			nve.NoncurrentDays = safeInt32Ptr(r.Days)
		}
		out.NoncurrentVersionExpiration = nve
	case LifecycleActionAbortIncompleteMultipart:
		aim := &awss3types.AbortIncompleteMultipartUpload{}
		if r.Days > 0 {
			aim.DaysAfterInitiation = safeInt32Ptr(r.Days)
		}
		out.AbortIncompleteMultipartUpload = aim
	case LifecycleActionTransition:
		t := &awss3types.Transition{
			StorageClass: awss3types.TransitionStorageClass(r.StorageClass),
		}
		if r.Days > 0 {
			t.Days = safeInt32Ptr(r.Days)
		}
		if r.Date != "" {
			t.Date = safeTimePtr(r.Date)
		}
		out.Transitions = []awss3types.Transition{*t}
	}
	return out
}

// fromS3LifecycleRule translates the AWS SDK LifecycleRule back to the
// Lahijan value type. Only the first Transition is read; the SDK's rule
// shape allows multiple transitions but Lahijan's MVP surface is one
// per rule.
func fromS3LifecycleRule(r awss3types.LifecycleRule) LifecycleRule {
	out := LifecycleRule{
		ID:      safeStrValue(r.ID),
		Enabled: r.Status == awss3types.ExpirationStatusEnabled,
	}
	if r.Filter != nil && r.Filter.Prefix != nil {
		out.Prefix = *r.Filter.Prefix
	}
	if r.Expiration != nil {
		out.Action = LifecycleActionExpiration
		if r.Expiration.Days != nil {
			out.Days = *r.Expiration.Days
		}
		if r.Expiration.Date != nil {
			out.Date = r.Expiration.Date.Format("2006-01-02")
		}
	}
	if r.NoncurrentVersionExpiration != nil {
		out.Action = LifecycleActionNoncurrentVersionExpiration
		if r.NoncurrentVersionExpiration.NoncurrentDays != nil {
			out.Days = *r.NoncurrentVersionExpiration.NoncurrentDays
		}
	}
	if r.AbortIncompleteMultipartUpload != nil {
		out.Action = LifecycleActionAbortIncompleteMultipart
		if r.AbortIncompleteMultipartUpload.DaysAfterInitiation != nil {
			out.Days = *r.AbortIncompleteMultipartUpload.DaysAfterInitiation
		}
	}
	if len(r.Transitions) > 0 {
		t := r.Transitions[0]
		out.Action = LifecycleActionTransition
		out.StorageClass = string(t.StorageClass)
		if t.Days != nil {
			out.Days = *t.Days
		}
		if t.Date != nil {
			out.Date = t.Date.Format("2006-01-02")
		}
	}
	return out
}

// safeStrPtr returns &s when s is non-empty, nil otherwise. Mirrors
// optionalStrPtr but always non-nil for the lifecycle case (the SDK
// treats nil ID as an error in some builds).
func safeStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// safeStrValue dereferences a *string; empty when nil.
func safeStrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// safeInt32Ptr returns &i for the SDK's optional int32 fields.
func safeInt32Ptr(i int32) *int32 {
	return &i
}

// safeTimePtr parses an RFC 3339 calendar date ("yyyy-mm-dd") and
// returns a *time.Time at UTC midnight. Returns nil on parse error.
func safeTimePtr(date string) *time.Time {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil
	}
	return &t
}
