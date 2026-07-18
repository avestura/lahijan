// Package seaweedfs_test: naming_test.go covers the bucket-naming
// convention enforced by ADR-0011 (compound "<tenant-uuid>-<slug>"
// form) and the S3-compatibility rules every bucket name must satisfy.
package seaweedfs_test

import (
	"strings"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketName_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		tenantID string
		slug     string
		want     string
	}{
		{
			name:     "minimal-slug",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "a",
			want:     "11111111-2222-3333-4444-555555555555-a",
		},
		{
			name:     "two-char-slug",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "ab",
			want:     "11111111-2222-3333-4444-555555555555-ab",
		},
		{
			name:     "human-readable",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "avatars",
			want:     "11111111-2222-3333-4444-555555555555-avatars",
		},
		{
			name:     "dashes-in-slug",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "user-uploads",
			want:     "11111111-2222-3333-4444-555555555555-user-uploads",
		},
		{
			name:     "uppercase-tenant-id-normalised",
			tenantID: "11111111-2222-3333-4444-55555555555A",
			slug:     "x",
			want:     "11111111-2222-3333-4444-55555555555a-x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := seaweedfs.BucketName(tc.tenantID, tc.slug)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqual(t, len(got), 63, "compound name must fit S3 ceiling")
		})
	}
}

func TestBucketName_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		tenantID string
		slug     string
		errIs    error
	}{
		{
			name:     "empty-slug",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "",
			errIs:    seaweedfs.ErrInvalidSlug,
		},
		{
			name:     "bad-tenant-id",
			tenantID: "not-a-uuid",
			slug:     "x",
			// canonicalTenantID returns the raw uuid parse error, not
			// the bucket-name sentinel — the caller wraps the error
			// with the storage-service HTTP envelope.
			errIs: nil,
		},
		{
			name:     "uppercase-slug",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "UPPER",
			errIs:    seaweedfs.ErrInvalidSlug,
		},
		{
			name:     "consecutive-dashes",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "a--b",
			errIs:    seaweedfs.ErrInvalidSlug,
		},
		{
			name:     "leading-dash",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "-lead",
			errIs:    seaweedfs.ErrInvalidSlug,
		},
		{
			name:     "trailing-dash",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "trail-",
			errIs:    seaweedfs.ErrInvalidSlug,
		},
		{
			name:     "illegal-char-underscore",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     "under_score",
			errIs:    seaweedfs.ErrInvalidSlug,
		},
		{
			name:     "too-long",
			tenantID: "11111111-2222-3333-4444-555555555555",
			slug:     strings.Repeat("a", 27),
			errIs:    seaweedfs.ErrInvalidSlug,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := seaweedfs.BucketName(tc.tenantID, tc.slug)
			require.Error(t, err)
			if tc.errIs != nil {
				assert.ErrorIs(t, err, tc.errIs)
			}
		})
	}
}

func TestParseBucketName_RoundTrip(t *testing.T) {
	t.Parallel()
	const tenant = "11111111-2222-3333-4444-555555555555"
	const slug = "uploads"
	name, err := seaweedfs.BucketName(tenant, slug)
	require.NoError(t, err)

	gotTenant, gotSlug, err := seaweedfs.ParseBucketName(name)
	require.NoError(t, err)
	assert.Equal(t, tenant, gotTenant)
	assert.Equal(t, slug, gotSlug)
}

func TestParseBucketName_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"too-short", "a"},
		{"no-dash-after-uuid", "11111111-2222-3333-4444-555555555555X"},
		{"bad-uuid", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz-slug"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := seaweedfs.ParseBucketName(tc.in)
			require.Error(t, err)
			assert.ErrorIs(t, err, seaweedfs.ErrInvalidBucketName)
		})
	}
}

func TestBucketARN_Canonical(t *testing.T) {
	t.Parallel()
	const tenant = "11111111-2222-3333-4444-555555555555"
	name, err := seaweedfs.BucketName(tenant, "x")
	require.NoError(t, err)
	arn := seaweedfs.BucketARN(name)
	assert.Equal(t, "arn:aws:s3:::"+name, arn)
}

func TestBucketARN_PanicsOnInvalid(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { _ = seaweedfs.BucketARN("") })
	})
	t.Run("uppercase", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { _ = seaweedfs.BucketARN("UPPERCASE-Bad") })
	})
}
