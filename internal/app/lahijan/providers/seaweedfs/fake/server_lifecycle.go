// Package fake: server_lifecycle.go extends the in-memory fake (server.go)
// with the WS-29 S3 surface (versioning / lifecycle / object-lock /
// delete-object / multipart-abort / copy). The new methods satisfy the
// extended s3BucketAPI interface added in WS-29.
//
// State is stored in new fields on Server (server_versioning.go's
// Server.versioning / Server.lifecycle / Server.objectLock /
// Server.multipartUploads). The fields are kept on Server so a single
// fake boot stays coherent across every method.
//
// Per ADR-0027 the test boundary is the operations interface, not the
// wire. The fake does NOT speak S3 XML; it implements the operations
// interface directly so each test is a few hundred nanoseconds per
// assertion.
package fake

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
)

// versioningState is the per-bucket versioning + object-lock state the
// fake tracks. Lives in Server.buckets[bucket]; written by the WS-29
// S3 surface methods.
type versioningState struct {
	// versioning is the bucket's versioning status. Empty = unversioned.
	versioning seaweedfs.VersioningStatus

	// objectLock is the bucket's object-lock policy. zero value when
	// object lock was never enabled.
	objectLock seaweedfs.ObjectLockConfig

	// lifecycle is the bucket's lifecycle rule set.
	lifecycle []seaweedfs.LifecycleRule

	// versions is the per-key list of object versions when versioning
	// is enabled. Each entry is a snapshot of the object's body at the
	// time the PUT happened. The first entry is the latest. Used by
	// ListObjectVersions + RestoreObjectVersion (the latter copies a
	// historical version to the head).
	versions map[string][]objectVersion

	// multipartUploads is the in-flight multipart uploads keyed by
	// uploadID. Used by AbortMultipartUpload + the lifecycle worker's
	// abort-incomplete-multipart action.
	multipartUploads map[string]multipartUpload
}

// objectVersion is a single object version snapshot.
type objectVersion struct {
	versionID      string
	key            string
	body           []byte
	createdAt      time.Time
	isLatest       bool
	isDeleteMarker bool
}

// multipartUpload is a single in-flight multipart upload. The fields
// mirror the SDK's AbortMultipartUpload input shape so tests can assert
// on them; only the upload identifier is consulted at runtime today.
//
//nolint:unused // fields kept for future abort-incomplete-multipart lifecycle coverage
type multipartUpload struct {
	uploadID    string
	initiatedAt time.Time
}

// newVersioningState returns a zero-value versioningState with the maps
// initialised.
func newVersioningState() versioningState {
	return versioningState{
		versions:         make(map[string][]objectVersion),
		multipartUploads: make(map[string]multipartUpload),
	}
}

// vs returns the per-bucket versioningState, allocating it on first
// access. Caller MUST hold s.mu.
func (s *Server) vs(bucket string) *versioningState {
	b, ok := s.buckets[bucket]
	if !ok {
		return nil
	}
	if b.vs == nil {
		state := newVersioningState()
		b.vs = &state
	}
	return b.vs
}

// vsLock is reserved for serialising versioningState mutations
// independently of Server.mu when the WS-29 surface needs to read state
// across multiple buckets. Unused today; kept here so tests can wire it
// without restructuring the package.
//
//nolint:unused // reserved for future cross-bucket versioning operations
var vsLock sync.Mutex

// ---------------------------------------------------------------------------
// Bucket versioning
// ---------------------------------------------------------------------------

// PutBucketVersioning implements seaweedfs.s3BucketAPI.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) PutBucketVersioning(_ context.Context, params *awss3.PutBucketVersioningInput, _ ...func(*awss3.Options)) (*awss3.PutBucketVersioningOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	status := seaweedfs.VersioningStatusUnversioned
	if params.VersioningConfiguration != nil && params.VersioningConfiguration.Status != "" {
		// The SDK passes the raw enum value ("Enabled" / "Suspended").
		// Map back to the Lahijan-normalised form. The empty / nil
		// case is treated as unversioned (matches the SDK's own
		// GetBucketVersioning response when versioning was never set).
		switch string(params.VersioningConfiguration.Status) {
		case "Enabled":
			status = seaweedfs.VersioningStatusEnabled
		case "Suspended":
			status = seaweedfs.VersioningStatusSuspended
		default:
			status = seaweedfs.VersioningStatusUnversioned
		}
	}
	vs.versioning = status
	return &awss3.PutBucketVersioningOutput{}, nil
}

// GetBucketVersioning implements seaweedfs.s3BucketAPI.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) GetBucketVersioning(_ context.Context, params *awss3.GetBucketVersioningInput, _ ...func(*awss3.Options)) (*awss3.GetBucketVersioningOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	out := &awss3.GetBucketVersioningOutput{}
	switch vs.versioning {
	case seaweedfs.VersioningStatusEnabled:
		out.Status = awss3types.BucketVersioningStatusEnabled
	case seaweedfs.VersioningStatusSuspended:
		out.Status = awss3types.BucketVersioningStatusSuspended
	case seaweedfs.VersioningStatusUnversioned:
		// Empty Status == unversioned (matches the SDK's own
		// GetBucketVersioning response when versioning was never set).
	}
	return out, nil
}

// ListObjectVersions implements seaweedfs.s3BucketAPI. Returns a
// deterministically-ordered page of object versions.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) ListObjectVersions(_ context.Context, params *awss3.ListObjectVersionsInput, _ ...func(*awss3.Options)) (*awss3.ListObjectVersionsOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	prefix := ""
	if params.Prefix != nil {
		prefix = *params.Prefix
	}
	out := &awss3.ListObjectVersionsOutput{
		Versions:      []awss3types.ObjectVersion{},
		DeleteMarkers: []awss3types.DeleteMarkerEntry{},
	}
	// Iterate the keys in deterministic order.
	keys := make([]string, 0, len(vs.versions))
	for k := range vs.versions {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	for _, k := range orderedStrings(keys) {
		for _, v := range vs.versions[k] {
			vid := v.versionID
			key := v.key
			created := v.createdAt
			isLatest := v.isLatest
			if v.isDeleteMarker {
				out.DeleteMarkers = append(out.DeleteMarkers, awss3types.DeleteMarkerEntry{
					VersionId:    &vid,
					Key:          &key,
					IsLatest:     &isLatest,
					LastModified: &created,
				})
				continue
			}
			size := int64(len(v.body))
			out.Versions = append(out.Versions, awss3types.ObjectVersion{
				VersionId:    &vid,
				Key:          &key,
				IsLatest:     &isLatest,
				LastModified: &created,
				Size:         &size,
			})
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Bucket lifecycle
// ---------------------------------------------------------------------------

// PutBucketLifecycleConfiguration implements seaweedfs.s3BucketAPI.
//
//nolint:lll,lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) PutBucketLifecycleConfiguration(_ context.Context, params *awss3.PutBucketLifecycleConfigurationInput, _ ...func(*awss3.Options)) (*awss3.PutBucketLifecycleConfigurationOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	rules := []seaweedfs.LifecycleRule{}
	if params.LifecycleConfiguration != nil {
		for _, r := range params.LifecycleConfiguration.Rules {
			rules = append(rules, lifecycleRuleFromS3(r))
		}
	}
	vs.lifecycle = rules
	return &awss3.PutBucketLifecycleConfigurationOutput{}, nil
}

// GetBucketLifecycleConfiguration implements seaweedfs.s3BucketAPI.
// Returns NoSuchLifecycleConfiguration when no rules are configured.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) GetBucketLifecycleConfiguration(_ context.Context, params *awss3.GetBucketLifecycleConfigurationInput, _ ...func(*awss3.Options)) (*awss3.GetBucketLifecycleConfigurationOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	if len(vs.lifecycle) == 0 {
		return nil, newFakeS3Error(404, "NoSuchLifecycleConfiguration", "no lifecycle config on %q", *params.Bucket)
	}
	out := &awss3.GetBucketLifecycleConfigurationOutput{
		Rules: make([]awss3types.LifecycleRule, 0, len(vs.lifecycle)),
	}
	for _, r := range vs.lifecycle {
		out.Rules = append(out.Rules, lifecycleRuleToS3(r))
	}
	return out, nil
}

// DeleteBucketLifecycle implements seaweedfs.s3BucketAPI.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) DeleteBucketLifecycle(_ context.Context, params *awss3.DeleteBucketLifecycleInput, _ ...func(*awss3.Options)) (*awss3.DeleteBucketLifecycleOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	vs.lifecycle = nil
	return &awss3.DeleteBucketLifecycleOutput{}, nil
}

// ---------------------------------------------------------------------------
// Object lock
// ---------------------------------------------------------------------------

// PutObjectLockConfiguration implements seaweedfs.s3BucketAPI.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) PutObjectLockConfiguration(_ context.Context, params *awss3.PutObjectLockConfigurationInput, _ ...func(*awss3.Options)) (*awss3.PutObjectLockConfigurationOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	if params.ObjectLockConfiguration == nil {
		vs.objectLock = seaweedfs.ObjectLockConfig{}
		return &awss3.PutObjectLockConfigurationOutput{}, nil
	}
	cfg := objectLockConfigFromS3(params.ObjectLockConfiguration)
	vs.objectLock = cfg
	return &awss3.PutObjectLockConfigurationOutput{}, nil
}

// GetObjectLockConfiguration implements seaweedfs.s3BucketAPI. Returns
// ObjectLockConfigurationNotFoundError when object lock was never set.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) GetObjectLockConfiguration(_ context.Context, params *awss3.GetObjectLockConfigurationInput, _ ...func(*awss3.Options)) (*awss3.GetObjectLockConfigurationOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	if !vs.objectLock.Enabled {
		return nil, newFakeS3Error(404, "ObjectLockConfigurationNotFoundError", "object lock not configured on %q", *params.Bucket)
	}
	return &awss3.GetObjectLockConfigurationOutput{
		ObjectLockConfiguration: objectLockConfigToS3(vs.objectLock),
	}, nil
}

// ---------------------------------------------------------------------------
// Object operations (delete / delete-batch / abort multipart / copy)
// ---------------------------------------------------------------------------

// DeleteObject implements seaweedfs.s3BucketAPI. On a versioned bucket
// with no versionId, creates a delete marker; with a versionId, hard-
// deletes that version. On an unversioned bucket, hard-deletes the key.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) DeleteObject(_ context.Context, params *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	if params == nil || params.Bucket == nil || params.Key == nil {
		return nil, newFakeS3Error(400, "InvalidRequest", "bucket + key are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := *params.Bucket
	key := *params.Key
	vs := s.vs(bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", bucket)
	}
	vid := ""
	if params.VersionId != nil {
		vid = *params.VersionId
	}
	if vs.versioning == seaweedfs.VersioningStatusEnabled {
		if vid != "" {
			// Hard-delete the specific version.
			versions := vs.versions[key]
			for i, v := range versions {
				if v.versionID == vid {
					vs.versions[key] = append(versions[:i], versions[i+1:]...)
					break
				}
			}
		} else {
			// Add a delete marker on top.
			marker := objectVersion{
				versionID:      fmt.Sprintf("delete-marker-%d", time.Now().UnixNano()),
				key:            key,
				body:           nil,
				createdAt:      time.Now().UTC(),
				isLatest:       true,
				isDeleteMarker: true,
			}
			for i := range vs.versions[key] {
				vs.versions[key][i].isLatest = false
			}
			vs.versions[key] = append([]objectVersion{marker}, vs.versions[key]...)
		}
	} else {
		// Unversioned: hard-delete the key.
		if b, ok := s.buckets[bucket]; ok && b.Objects != nil {
			delete(b.Objects, key)
		}
		delete(vs.versions, key)
	}
	return &awss3.DeleteObjectOutput{}, nil
}

// DeleteObjects implements seaweedfs.s3BucketAPI. Batched DeleteObject.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) DeleteObjects(ctx context.Context, params *awss3.DeleteObjectsInput, opts ...func(*awss3.Options)) (*awss3.DeleteObjectsOutput, error) {
	if params == nil || params.Bucket == nil || params.Delete == nil {
		return nil, newFakeS3Error(400, "InvalidRequest", "bucket + delete are required")
	}
	out := &awss3.DeleteObjectsOutput{}
	for _, ident := range params.Delete.Objects {
		input := &awss3.DeleteObjectInput{
			Bucket: params.Bucket,
			Key:    ident.Key,
		}
		if ident.VersionId != nil {
			input.VersionId = ident.VersionId
		}
		if _, err := s.DeleteObject(ctx, input, opts...); err != nil {
			code := "InternalError"
			msg := err.Error()
			var s3err *fakeS3Error
			if errors.As(err, &s3err) {
				code = s3err.code
				msg = s3err.message
			}
			out.Errors = append(out.Errors, awss3types.Error{
				Key:     ident.Key,
				Code:    &code,
				Message: &msg,
			})
			continue
		}
		deleted := *ident.Key
		out.Deleted = append(out.Deleted, awss3types.DeletedObject{Key: &deleted})
	}
	return out, nil
}

// AbortMultipartUpload implements seaweedfs.s3BucketAPI.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) AbortMultipartUpload(_ context.Context, params *awss3.AbortMultipartUploadInput, _ ...func(*awss3.Options)) (*awss3.AbortMultipartUploadOutput, error) {
	if params == nil || params.Bucket == nil || params.Key == nil || params.UploadId == nil {
		return nil, newFakeS3Error(400, "InvalidRequest", "bucket + key + uploadId are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	vs := s.vs(*params.Bucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	delete(vs.multipartUploads, *params.UploadId)
	return &awss3.AbortMultipartUploadOutput{}, nil
}

// CopyObject implements seaweedfs.s3BucketAPI. Used by the versioning
// RestoreObjectVersion flow (server-side copy from versioned source to
// current).
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) CopyObject(_ context.Context, params *awss3.CopyObjectInput, _ ...func(*awss3.Options)) (*awss3.CopyObjectOutput, error) {
	if params == nil || params.Bucket == nil || params.Key == nil || params.CopySource == nil {
		return nil, newFakeS3Error(400, "InvalidRequest", "bucket + key + copySource are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dstBucket := *params.Bucket
	dstKey := *params.Key
	vs := s.vs(dstBucket)
	if vs == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", dstBucket)
	}
	// Parse CopySource: "<bucket>/<key>?versionId=<vid>".
	src := strings.TrimPrefix(*params.CopySource, "/")
	parts := strings.SplitN(src, "/", 2)
	if len(parts) < 2 {
		return nil, newFakeS3Error(400, "InvalidRequest", "copySource must be <bucket>/<key>")
	}
	srcBucket, srcTail := parts[0], parts[1]
	srcKey := srcTail
	srcVID := ""
	if idx := strings.Index(srcTail, "?versionId="); idx >= 0 {
		srcKey = srcTail[:idx]
		srcVID = srcTail[idx+len("?versionId="):]
	}
	srcVS := s.vs(srcBucket)
	if srcVS == nil {
		return nil, newFakeS3Error(404, "NoSuchBucket", "source bucket %q does not exist", srcBucket)
	}
	srcVersions, ok := srcVS.versions[srcKey]
	if !ok || len(srcVersions) == 0 {
		// Fall back to the unversioned object map.
		if body, ok := s.buckets[srcBucket].Objects[srcKey]; ok {
			s.writeObjectVersion(vs, dstKey, body)
			etag := "\"fake-copy-etag\""
			return &awss3.CopyObjectOutput{CopyObjectResult: &awss3types.CopyObjectResult{ETag: &etag}}, nil
		}
		return nil, newFakeS3Error(404, "NoSuchKey", "key %q does not exist in bucket %q", srcKey, srcBucket)
	}
	// Find the requested version (or latest if versionId is empty).
	picked := srcVersions[0]
	if srcVID != "" {
		for _, v := range srcVersions {
			if v.versionID == srcVID {
				picked = v
				break
			}
		}
	}
	s.writeObjectVersion(vs, dstKey, picked.body)
	etag := "\"fake-copy-etag-" + picked.versionID + "\""
	return &awss3.CopyObjectOutput{CopyObjectResult: &awss3types.CopyObjectResult{ETag: &etag}}, nil
}

// writeObjectVersion writes a new version of an object on a versioned
// bucket, or overwrites on an unversioned bucket. Caller MUST hold s.mu.
func (s *Server) writeObjectVersion(vs *versioningState, key string, body []byte) {
	picked := append([]byte(nil), body...)
	if vs.versioning == seaweedfs.VersioningStatusEnabled {
		for i := range vs.versions[key] {
			vs.versions[key][i].isLatest = false
		}
		vs.versions[key] = append([]objectVersion{{
			versionID: fmt.Sprintf("v-%d", time.Now().UnixNano()),
			key:       key,
			body:      picked,
			createdAt: time.Now().UTC(),
			isLatest:  true,
		}}, vs.versions[key]...)
		return
	}
	// Unversioned: overwrite the first matching bucket's object map.
	// The caller has already validated the bucket name.
	for _, b := range s.buckets {
		if b.Objects != nil {
			b.Objects[key] = picked
			return
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// orderedStrings returns a sorted copy of in.
func orderedStrings(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// lifecycleRuleToS3 translates a driver-level LifecycleRule to the AWS
// SDK type. The fake uses the same translation the production driver
// uses in lifecycle.go.
func lifecycleRuleToS3(r seaweedfs.LifecycleRule) awss3types.LifecycleRule {
	status := awss3types.ExpirationStatusDisabled
	if r.Enabled {
		status = awss3types.ExpirationStatusEnabled
	}
	out := awss3types.LifecycleRule{
		ID:     ptrIfNonEmpty(r.ID),
		Status: status,
		Filter: &awss3types.LifecycleRuleFilter{},
	}
	if r.Prefix != "" {
		out.Filter = &awss3types.LifecycleRuleFilter{Prefix: ptrIfNonEmpty(r.Prefix)}
	}
	return out
}

// lifecycleRuleFromS3 translates the AWS SDK LifecycleRule back to the
// driver-level type. Mirror of lifecycle.go's fromS3LifecycleRule.
func lifecycleRuleFromS3(r awss3types.LifecycleRule) seaweedfs.LifecycleRule {
	out := seaweedfs.LifecycleRule{
		Enabled: r.Status == awss3types.ExpirationStatusEnabled,
	}
	if r.ID != nil {
		out.ID = *r.ID
	}
	if r.Filter != nil && r.Filter.Prefix != nil {
		out.Prefix = *r.Filter.Prefix
	}
	return out
}

// objectLockConfigToS3 translates a driver-level ObjectLockConfig to
// the AWS SDK type.
func objectLockConfigToS3(cfg seaweedfs.ObjectLockConfig) *awss3types.ObjectLockConfiguration {
	if !cfg.Enabled {
		return &awss3types.ObjectLockConfiguration{}
	}
	mode := awss3types.ObjectLockRetentionModeGovernance
	if cfg.Mode == seaweedfs.ObjectLockModeCompliance {
		mode = awss3types.ObjectLockRetentionModeCompliance
	}
	days := cfg.Days
	return &awss3types.ObjectLockConfiguration{
		ObjectLockEnabled: awss3types.ObjectLockEnabledEnabled,
		Rule: &awss3types.ObjectLockRule{
			DefaultRetention: &awss3types.DefaultRetention{
				Mode: mode,
				Days: &days,
			},
		},
	}
}

// objectLockConfigFromS3 translates the AWS SDK ObjectLockConfiguration
// to the driver-level type.
func objectLockConfigFromS3(c *awss3types.ObjectLockConfiguration) seaweedfs.ObjectLockConfig {
	if c == nil || c.Rule == nil || c.Rule.DefaultRetention == nil {
		return seaweedfs.ObjectLockConfig{}
	}
	dr := c.Rule.DefaultRetention
	cfg := seaweedfs.ObjectLockConfig{
		Enabled: c.ObjectLockEnabled == awss3types.ObjectLockEnabledEnabled,
	}
	if dr.Days != nil {
		cfg.Days = *dr.Days
	}
	switch dr.Mode {
	case awss3types.ObjectLockRetentionModeGovernance:
		cfg.Mode = seaweedfs.ObjectLockModeGovernance
	case awss3types.ObjectLockRetentionModeCompliance:
		cfg.Mode = seaweedfs.ObjectLockModeCompliance
	}
	return cfg
}

// ptrIfNonEmpty returns &s when s is non-empty, nil otherwise.
func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
