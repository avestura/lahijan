// Package seaweedfs: naming.go enforces the bucket-naming convention mandated
// by ADR-0011. Every Lahijan-minted bucket is named "<tenant-uuid>-<slug>"
// where the tenant-uuid is the canonical UUID string (lowercase, dashed)
// and the slug is 1-50 lowercase alphanumeric characters + dashes (must
// start + end with alphanumeric).
//
// The compound name is what the user sees in the S3 endpoint and what the
// SeaweedFS S3 server uses as the URL path component. Per ADR-0011 the
// bucket ARN is "arn:aws:s3:::<tenant-uuid>-<slug>".
package seaweedfs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// maxBucketNameLength is the S3 protocol ceiling on bucket names. The
// compound "<tenant-uuid>-<slug>" form must fit within this limit.
// 36 (uuid) + 1 (dash) + slug = 37 + len(slug) <= 63, so the slug is
// capped at 26 characters in practice. See validateSlug.
const maxBucketNameLength = 63

// slugMaxLength is the maximum slug length given the compound
// "<tenant-uuid>-<slug>" form must fit within maxBucketNameLength.
// 36 (uuid) + 1 (dash) + slugMaxLength = 63 => slugMaxLength = 26.
const slugMaxLength = maxBucketNameLength - 37

// slugPattern is the regex a Lahijan bucket slug must match.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,24}[a-z0-9]$|^[a-z0-9]$`)

// canonicalTenantID parses the given tenant id string and returns the
// canonical lowercase UUID form. Returns an error when the string is not a
// valid UUID; the caller (storage service in WS-16) surfaces this as a 400
// "invalid tenant context" envelope.
func canonicalTenantID(tenantID string) (string, error) {
	id, err := uuid.Parse(tenantID)
	if err != nil {
		return "", fmt.Errorf("seaweedfs: tenant id %q is not a UUID: %w", tenantID, err)
	}
	return id.String(), nil
}

// validateSlug returns a non-nil error when the slug fails the Lahijan
// bucket-slug rules (1-26 chars, lowercase alphanumeric + dashes, must
// start + end with alphanumeric). The compound-name length check happens
// in BucketName after both parts have been validated independently.
func validateSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("%w: slug is empty", ErrInvalidSlug)
	}
	if len(slug) > slugMaxLength {
		return fmt.Errorf("%w: slug %q length %d exceeds max %d",
			ErrInvalidSlug, slug, len(slug), slugMaxLength)
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("%w: slug %q must be 1-%d chars, lowercase alphanumeric + dashes, "+
			"start + end alphanumeric", ErrInvalidSlug, slug, slugMaxLength)
	}
	if strings.Contains(slug, "--") {
		return fmt.Errorf("%w: slug %q must not contain consecutive dashes",
			ErrInvalidSlug, slug)
	}
	return nil
}

// BucketName composes the canonical bucket name from a tenant UUID and a
// slug. Both inputs are validated; the function returns an error rather
// than a partial name so the caller never accidentally persists a
// malformed name.
func BucketName(tenantID, slug string) (string, error) {
	tid, err := canonicalTenantID(tenantID)
	if err != nil {
		return "", err
	}
	if err := validateSlug(slug); err != nil {
		return "", err
	}
	name := tid + "-" + slug
	if len(name) > maxBucketNameLength {
		// Defence in depth: validateSlug caps the slug so this branch is
		// unreachable in normal flow. Kept so a future caller that
		// bypasses validateSlug still fails closed.
		return "", fmt.Errorf("%w: compound name %q length %d exceeds max %d",
			ErrInvalidBucketName, name, len(name), maxBucketNameLength)
	}
	return name, nil
}

// ParseBucketName splits a canonical bucket name back into its tenant-uuid
// and slug components. Returns an error when the name does not match the
// Lahijan naming convention. Used by the storage service (WS-16) to look
// up the owning tenant from a raw SeaweedFS bucket list.
func ParseBucketName(name string) (tenantID, slug string, err error) {
	if name == "" {
		return "", "", fmt.Errorf("%w: name is empty", ErrInvalidBucketName)
	}
	// UUID is always 36 chars; the separator at index 36 is the dash.
	if len(name) < 38 { // 36 (uuid) + 1 (dash) + 1 (min slug)
		return "", "", fmt.Errorf("%w: name %q too short for tenant-uuid-slug form",
			ErrInvalidBucketName, name)
	}
	if name[36] != '-' {
		return "", "", fmt.Errorf("%w: name %q missing dash separator after tenant uuid",
			ErrInvalidBucketName, name)
	}
	tid := name[:36]
	slug = name[37:]
	if _, parseErr := uuid.Parse(tid); parseErr != nil {
		return "", "", fmt.Errorf("%w: tenant-uuid portion %q is not a UUID: %w",
			ErrInvalidBucketName, tid, parseErr)
	}
	if err := validateSlug(slug); err != nil {
		return "", "", fmt.Errorf("%w: (slug portion) %w", ErrInvalidBucketName, err)
	}
	return tid, slug, nil
}

// looksLikeLahijanBucket is a quick predicate the storage service uses to
// filter SeaweedFS' raw ListBuckets response. The check is structural (no
// UUID parse) so it is cheap; the caller can run the strict ParseBucketName
// on matches.
//
// Currently unused at the driver layer; kept exported so the upcoming
// storage module (WS-16) does not have to re-derive the rule. The
// package-level lint configuration excludes this function from the
// unused check via the standard `//nolint:unused` directive below.
//
//nolint:unused // consumed by WS-16 (storage module).
func looksLikeLahijanBucket(name string) bool {
	if len(name) < 38 || len(name) > maxBucketNameLength {
		return false
	}
	if name[36] != '-' {
		return false
	}
	return slugPattern.MatchString(name[37:])
}
