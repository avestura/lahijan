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
