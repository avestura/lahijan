// Package seaweedfs: types.go holds the JSON request/response types for the
// SeaweedFS Filer + IAM surface this driver needs, plus the domain value
// types the storage service (WS-16) consumes (Bucket, IAMCredential,
// QuotaSpec, PresignResult).
//
// AWS SDK S3 types are NOT re-declared here — the buckets.go file uses the
// SDK types directly (s3.CreateBucketInput, etc.) because we delegate the
// S3 wire format to the SDK. This file is concerned only with the
// Filer-side types and the driver's own domain values.
package seaweedfs

import "time"

// Bucket is the driver-level view of a SeaweedFS S3 bucket. Mirrors the
// subset of the AWS SDK ListBucketsOutput.Buckets entries Lahijan uses.
type Bucket struct {
	// Name is the canonical bucket name ("tenant-uuid-slug").
	Name string

	// CreatedAt is when the bucket was created (best-effort: SeaweedFS
	// reports this via the S3 ListBuckets response). Zero when unknown.
	CreatedAt time.Time
}

// IAMCredential is the result of a Mint or Rotate call. The SecretKey is
// SENSITIVE — the caller (storage service in WS-16) returns it to the
// user once at creation time and stores only the encrypted form in the
// DB; subsequent reads return only the AccessKey.
type IAMCredential struct {
	// AccessKey is the public identifier the user presents to SeaweedFS.
	// 20-character AWS-style identifier. Stored unhashed in the Filer.
	AccessKey string

	// SecretKey is the paired secret the user uses to sign S3 requests.
	// SENSITIVE — never logged. Returned to the user once at mint time.
	SecretKey string

	// Buckets is the list of canonical bucket names this credential has
	// access to. Enforced server-side by SeaweedFS' IAM subsystem.
	Buckets []string

	// Actions is the list of S3-style actions the credential permits
	// ("Read", "Write", "List", "Tagging", "Admin"). The Lahijan
	// storage service translates high-level roles ("viewer", "editor",
	// "owner") into action lists before calling Mint.
	Actions []string

	// Enabled is true when the credential is currently usable. Mint and
	// Rotate always return Enabled=true; Revoke flips it to false
	// (without deleting the record) so the audit trail survives.
	Enabled bool
}

// QuotaSpec is the per-bucket quota configuration Lahijan writes into the
// Filer. SeaweedFS enforces the quota server-side. Zero values mean
// "no limit on that dimension".
type QuotaSpec struct {
	// SizeMiB is the maximum total object size in mebibytes.
	SizeMiB int64

	// FileCount is the maximum number of objects in the bucket.
	FileCount int64
}

// PresignResult is the output of a Presign call. URL is the SigV4-signed
// URL the user's S3 client opens directly against SeaweedFS; Method tells
// the caller whether the URL is for GET or PUT; ExpiresAt is when the
// signature stops being valid.
type PresignResult struct {
	// URL is the pre-signed URL the user opens. Already encodes the
	// access key, signature, expiry, and target object.
	URL string

	// Method is "GET" (download) or "PUT" (upload).
	Method string

	// Bucket is the canonical bucket name the URL targets.
	Bucket string

	// Key is the object key within the bucket. Empty for bucket-level
	// presigns (e.g. list operations).
	Key string

	// ExpiresAt is when the signature stops being valid.
	ExpiresAt time.Time
}

// identityRecord is the JSON payload the driver writes into the Filer at
// /etc/seaweedfs/identities/<access_key>.json. Mirrors the subset of
// SeaweedFS' internal Identity struct Lahijan manages.
//
// The record is the source of truth for the S3 IAM subsystem: when the
// `weed s3` server is configured to load identities from the Filer, it
// watches this path and reloads on change. We deliberately keep the field
// set narrow so future SeaweedFS schema bumps do not silently leak past
// the driver.
type identityRecord struct {
	// Name is a human-readable identifier; Lahijan uses the access key.
	Name string `json:"name"`

	// AccessKey is the public identifier the user presents.
	AccessKey string `json:"access_key"`

	// SecretKey is the paired secret. SENSITIVE — never logged.
	SecretKey string `json:"secret_key"`

	// IsAdmin is always false for Lahijan-minted credentials; only the
	// cluster-level admin credential (configured in compose) has
	// IsAdmin=true.
	IsAdmin bool `json:"is_admin,omitempty"`

	// Buckets is the list of bucket names this credential can access.
	// The SeaweedFS S3 server enforces the scope server-side.
	Buckets []string `json:"buckets,omitempty"`

	// Actions is the list of permitted S3 actions. Wildcards are
	// supported ("Read:*", "Write:*", "*"); Lahijan typically uses
	// bucket-scoped actions ("Read:<bucket>", "Write:<bucket>").
	Actions []string `json:"actions,omitempty"`

	// Disabled flips to true on revoke. SeaweedFS keeps the identity
	// loaded so subsequent access attempts fail closed; the audit trail
	// in the Filer metadata survives.
	Disabled bool `json:"disabled,omitempty"`
}

// quotaRecord is the JSON payload the driver writes into the Filer at
// /etc/seaweedfs/buckets/<bucket>/quota.json. SeaweedFS' `weed s3`
// enforces the quota server-side.
type quotaRecord struct {
	// SizeMiB is the maximum total object size in mebibytes. Zero means
	// "no size limit" (only FileCount is enforced).
	SizeMiB int64 `json:"size_mib,omitempty"`

	// FileCount is the maximum number of objects. Zero means "no count
	// limit" (only SizeMiB is enforced).
	FileCount int64 `json:"file_count,omitempty"`
}

// FilerStatus is the JSON shape returned by GET / on the Filer root. We
// only read the Version + Topology fields; the rest is ignored.
// Exported so the in-memory fake (providers/seaweedfs/fake) can satisfy
// the unexported filerAPI interface from outside this package.
type FilerStatus struct {
	// Version is the SeaweedFS Filer version string.
	Version string `json:"version,omitempty"`

	// Topology carries the cluster-level volume / data nodes summary.
	Topology *FilerTopology `json:"topology,omitempty"`
}

// FilerTopology is the SeaweedFS cluster topology summary embedded in
// the status response. Only the fields the driver cares about are
// decoded. Exported so the fake can construct values.
type FilerTopology struct {
	// Free is the reported free byte count across all volumes.
	Free int64 `json:"free,omitempty"`

	// Max is the reported total byte capacity.
	Max int64 `json:"max,omitempty"`

	// VolumeCount is the number of writable volumes in the cluster.
	VolumeCount int `json:"volume_count,omitempty"`

	// ActiveVolumeCount is the number of volumes currently accepting writes.
	ActiveVolumeCount int `json:"active_volume_count,omitempty"`
}

// FilerVolumeInfo is the simplified volume info returned by the driver's
// GetClusterStatus. The storage service surfaces it in the admin debug page.
type FilerVolumeInfo struct {
	// Version is the SeaweedFS Filer version string.
	Version string

	// FreeBytes is the total free bytes across the cluster.
	FreeBytes int64

	// TotalBytes is the total byte capacity across the cluster.
	TotalBytes int64

	// VolumeCount is the number of writable volumes.
	VolumeCount int

	// ActiveVolumeCount is the number of volumes currently accepting writes.
	ActiveVolumeCount int
}
