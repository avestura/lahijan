// Package storage: errors.go holds the sentinel errors the storage service
// surfaces. Handlers translate them to HTTP envelopes via api/errors.go.
//
// Errors are wrapped at boundaries with context (per
// .opencode/skills/backend-foundations/SKILL.md):
//
//	fmt.Errorf("storage.bucket.create: %w", err)
//
// so a caller can errors.Is(err, storage.ErrBucketNotFound) without
// caring which layer raised the error.
package storage

import "errors"

// ErrProviderDisabled is returned when the SeaweedFS provider is not
// configured. The handler maps it to 501 not_implemented.
var ErrProviderDisabled = errors.New("storage: provider is not enabled on this server")

// ErrBucketNotFound is returned when the bucket does not exist within the
// caller's tenant. The handler maps it to 404 not_found.
var ErrBucketNotFound = errors.New("storage: bucket not found")

// ErrCredentialNotFound is returned when the credential does not exist
// within the caller's tenant. The handler maps it to 404 not_found.
var ErrCredentialNotFound = errors.New("storage: credential not found")

// ErrBucketAlreadyExists is returned when a create call uses a canonical
// name that's already owned by another tenant (or has been soft-deleted
// and not yet purged). The handler maps it to 409 conflict.
var ErrBucketAlreadyExists = errors.New("storage: bucket name already claimed")

// ErrInvalidBucketSlug is returned when the slug fails the Lahijan
// naming convention (1-26 lowercase alphanumeric + dashes, must start +
// end alphanumeric, no consecutive dashes).
var ErrInvalidBucketSlug = errors.New("storage: bucket slug must be 1-26 lowercase alphanumeric characters or dashes")

// ErrInvalidQuota is returned when the caller passes a negative quota
// dimension or a quota_bytes that exceeds the per-bucket ceiling.
var ErrInvalidQuota = errors.New("storage: quota dimensions must be non-negative")

// ErrInvalidAction is returned when the credential mint request carries
// an action that is not in the supported set (Read/Write/List/Tagging/Admin).
var ErrInvalidAction = errors.New("storage: action must be one of Read, Write, List, Tagging, Admin")

// ErrInvalidPresignMethod is returned when the presign request specifies a
// method other than GET or PUT.
var ErrInvalidPresignMethod = errors.New("storage: presign method must be GET or PUT")

// ErrInvalidPresignTTL is returned when the presign TTL is outside the
// allowed range (1 second to 24 hours).
var ErrInvalidPresignTTL = errors.New("storage: presign ttl must be between 1 second and 24 hours")

// ErrInvalidLabel is returned when the credential label exceeds the
// 100-character ceiling.
var ErrInvalidLabel = errors.New("storage: credential label must be 100 characters or fewer")

// ErrCredentialExpired is returned when the caller attempts to use a
// credential past its expires_at. SeaweedFS does not enforce S3
// expiries today; Lahijan owns this check at the storage service.
var ErrCredentialExpired = errors.New("storage: credential has expired")

// ErrLifecycleRuleNotFound is returned when the lifecycle rule does not
// exist within the caller's tenant. The handler maps it to 404 not_found.
var ErrLifecycleRuleNotFound = errors.New("storage: lifecycle rule not found")

// ErrInvalidLifecycleRuleID is returned when the caller-supplied rule
// id is empty or longer than 255 characters.
var ErrInvalidLifecycleRuleID = errors.New("storage: lifecycle rule id must be 1-255 characters")

// ErrInvalidLifecycleAction is returned when the caller-supplied action
// is not in the supported set.
var ErrInvalidLifecycleAction = errors.New(
	"storage: lifecycle action must be expiration, noncurrent_version_expiration, " +
		"abort_incomplete_multipart, or transition",
)

// ErrInvalidLifecycleTrigger is returned when exactly one of days or
// date_at is not set, or when days is non-positive.
var ErrInvalidLifecycleTrigger = errors.New("storage: lifecycle rule must set exactly one of days or date_at; days must be positive")

// ErrInvalidLifecycleStorageClass is returned when a transition rule
// does not set storage_class, or when a non-transition rule sets one.
var ErrInvalidLifecycleStorageClass = errors.New("storage: storage_class is required for transition rules and forbidden for other actions")

// ErrInvalidObjectLockMode is returned when the caller-supplied
// object-lock mode is not GOVERNANCE or COMPLIANCE.
var ErrInvalidObjectLockMode = errors.New("storage: object lock mode must be GOVERNANCE or COMPLIANCE")

// ErrInvalidObjectLockDays is returned when the caller-supplied
// object-lock default retention days is non-positive.
var ErrInvalidObjectLockDays = errors.New("storage: object lock retention days must be positive")
