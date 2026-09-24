// Package fake provides an in-memory fake of the SeaweedFS S3 + Filer API
// surface Lahijan's driver uses. The fake implements the driver's
// internal operations interfaces (`s3BucketAPI`, `presignAPI`,
// `filerAPI`) directly so unit tests do not need to speak the S3 wire
// protocol or the Filer REST shape.
//
// Per ADR-0027 the test boundary is the operations interface, not the
// wire. WS-22 (integration test harness) brings up a real SeaweedFS
// container and exercises the real HTTP path end-to-end.
//
// Concurrency: the fake serializes writes behind a sync.Mutex. Reads
// are goroutine-safe.
package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
)

// Server is the in-memory SeaweedFS fake. Construct via NewServer.
//
// The struct intentionally implements all three driver-side interfaces
// (`s3BucketAPI`, `presignAPI`, `filerAPI`) on the same value so a
// single fake boot gives the test a coherent view across buckets +
// IAM + quotas + filer metadata.
type Server struct {
	mu sync.Mutex

	buckets    map[string]*fakeBucket
	identities map[string]*fakeIdentity
	quotaMap   map[string][]byte // path -> body (raw JSON)
	metadata   map[string][]byte // path -> body (raw JSON)

	// emitHook is called by every mutating handler after the change
	// commits. Tests use it to assert the driver's event-synthesis path
	// routed through correctly.
	emitHook func(topic, resourceID string)

	// pingFilerFail, when true, makes the Filer GetStatus return an
	// error (used to test degraded Ping paths).
	pingFilerFail bool

	// pingS3Fail, when true, makes the S3 ListBuckets return an error.
	pingS3Fail bool

	// status overrides for Capabilities assertions.
	filerVersion string
	clusterMode  bool

	// presignCounter counts the number of presign URLs generated since
	// boot. Used by tests that assert the driver exercised the presign
	// path.
	presignCounter int
}

// fakeBucket is the per-bucket in-memory state.
type fakeBucket struct {
	Name      string
	CreatedAt time.Time
	Objects   map[string][]byte // key -> body
	// vs holds the per-bucket versioning + lifecycle + object-lock
	// state added in WS-29 (server_lifecycle.go). Lazily allocated by
	// Server.vs(); nil on the original WS-13 surface so existing tests
	// do not pay the allocation.
	vs *versioningState
	// cors is the last CORS configuration written via PutBucketCors.
	cors *awss3types.CORSConfiguration
}

// fakeIdentity is the per-identity in-memory state. Mirrors the
// identityRecord shape the driver writes to the Filer.
type fakeIdentity struct {
	Name      string
	AccessKey string
	SecretKey string
	IsAdmin   bool
	Buckets   []string
	Actions   []string
	Disabled  bool
}

// NewServer returns a fresh in-memory SeaweedFS fake. t.Cleanup is wired
// so the test does not need to tear the fake down explicitly.
func NewServer(t testing.TB) *Server {
	t.Helper()
	return &Server{
		buckets:      make(map[string]*fakeBucket),
		identities:   make(map[string]*fakeIdentity),
		quotaMap:     make(map[string][]byte),
		metadata:     make(map[string][]byte),
		filerVersion: "3.61-fake",
	}
}

// SetEmitHook registers a callback fired after every successful mutation.
func (s *Server) SetEmitHook(fn func(topic, resourceID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitHook = fn
}

// SetFilerVersion overrides the reported Filer version.
func (s *Server) SetFilerVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filerVersion = v
}

// SetClusterMode toggles the cluster-mode heuristic in the status payload.
func (s *Server) SetClusterMode(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterMode = on
}

// SetPingerFailures forces the next Ping to fail on the Filer, S3, or
// both. Pass true to failFiler or failS3 to fail that half.
func (s *Server) SetPingerFailures(failFiler, failS3 bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pingFilerFail = failFiler
	s.pingS3Fail = failS3
}

// PresignCount returns the number of presign URLs generated since boot.
func (s *Server) PresignCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presignCounter
}

// Buckets returns a snapshot of every bucket currently in the fake.
func (s *Server) Buckets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.buckets))
	for name := range s.buckets {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Identities returns a snapshot of every identity currently in the fake.
func (s *Server) Identities() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.identities))
	for ak := range s.identities {
		out = append(out, ak)
	}
	sort.Strings(out)
	return out
}

// HasIdentity reports whether the given access key currently exists in
// the fake. Convenience for tests that revoke + assert.
func (s *Server) HasIdentity(accessKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.identities[accessKey]
	return ok
}

// IdentityBuckets returns the bucket allowlist currently configured for
// the given access key. Empty list when the identity does not exist.
func (s *Server) IdentityBuckets(accessKey string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.identities[accessKey]
	if !ok {
		return nil
	}
	return append([]string(nil), id.Buckets...)
}

// IdentityActions returns the action list for the given access key.
func (s *Server) IdentityActions(accessKey string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.identities[accessKey]
	if !ok {
		return nil
	}
	return append([]string(nil), id.Actions...)
}

// SetBucketObject writes an object directly into the in-memory state.
// Tests use this to seed a bucket before asserting GetObject behavior.
func (s *Server) SetBucketObject(bucket, key string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[bucket]
	if !ok {
		return fmt.Errorf("fake: bucket %q does not exist", bucket)
	}
	if b.Objects == nil {
		b.Objects = make(map[string][]byte)
	}
	b.Objects[key] = append([]byte(nil), body...)
	return nil
}

// emit fires the registered emit hook (if any). Called by every mutating
// handler after the change commits. Callers MUST NOT hold s.mu when
// calling emit — the hook may itself inspect server state.
func (s *Server) emit(topic, resourceID string) {
	fn := s.emitHookSnapshot()
	if fn != nil {
		fn(topic, resourceID)
	}
}

// emitHookSnapshot returns the current emit hook without holding s.mu
// across the call into the hook.
func (s *Server) emitHookSnapshot() func(topic, resourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emitHook
}

// ---------------------------------------------------------------------------
// s3BucketAPI impl
// ---------------------------------------------------------------------------

// CreateBucket implements seaweedfs.s3BucketAPI.
func (s *Server) CreateBucket(_ context.Context, params *awss3.CreateBucketInput, _ ...func(*awss3.Options)) (*awss3.CreateBucketOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	name := *params.Bucket
	s.mu.Lock()
	if _, exists := s.buckets[name]; exists {
		s.mu.Unlock()
		return nil, newFakeS3Error(409, "BucketAlreadyExists", "bucket %q already exists", name)
	}
	s.buckets[name] = &fakeBucket{
		Name:      name,
		CreatedAt: time.Now().UTC(),
		Objects:   make(map[string][]byte),
	}
	s.mu.Unlock()
	s.emit("storage.bucket.created", name)
	return &awss3.CreateBucketOutput{}, nil
}

// PutBucketCors implements seaweedfs.s3BucketAPI.
func (s *Server) PutBucketCors(_ context.Context, params *awss3.PutBucketCorsInput, _ ...func(*awss3.Options)) (*awss3.PutBucketCorsOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[*params.Bucket]
	if !ok {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", *params.Bucket)
	}
	b.cors = params.CORSConfiguration
	return &awss3.PutBucketCorsOutput{}, nil
}

// BucketCORS returns the CORS configuration last written to bucket (nil
// when none). Test helper.
func (s *Server) BucketCORS(bucket string) *awss3types.CORSConfiguration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.buckets[bucket]; ok {
		return b.cors
	}
	return nil
}

// DeleteBucket implements seaweedfs.s3BucketAPI.
func (s *Server) DeleteBucket(_ context.Context, params *awss3.DeleteBucketInput, _ ...func(*awss3.Options)) (*awss3.DeleteBucketOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	name := *params.Bucket
	s.mu.Lock()
	if _, exists := s.buckets[name]; !exists {
		s.mu.Unlock()
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", name)
	}
	delete(s.buckets, name)
	delete(s.quotaMap, "/etc/seaweedfs/buckets/"+name+"/quota.json")
	s.mu.Unlock()
	s.emit("storage.bucket.deleted", name)
	return &awss3.DeleteBucketOutput{}, nil
}

// HeadBucket implements seaweedfs.s3BucketAPI.
func (s *Server) HeadBucket(_ context.Context, params *awss3.HeadBucketInput, _ ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error) {
	if params == nil || params.Bucket == nil {
		return nil, newFakeS3Error(400, "InvalidBucketName", "bucket is required")
	}
	name := *params.Bucket
	s.mu.Lock()
	_, exists := s.buckets[name]
	s.mu.Unlock()
	if !exists {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", name)
	}
	return &awss3.HeadBucketOutput{}, nil
}

// ListBuckets implements seaweedfs.s3BucketAPI.
func (s *Server) ListBuckets(_ context.Context, _ *awss3.ListBucketsInput, _ ...func(*awss3.Options)) (*awss3.ListBucketsOutput, error) {
	if s.pingS3Fail {
		return nil, newFakeS3Error(503, "InternalError", "fake: listbuckets failed (pingS3Fail set)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := &awss3.ListBucketsOutput{
		Buckets: make([]awss3types.Bucket, 0, len(s.buckets)),
	}
	for name, b := range s.buckets {
		created := b.CreatedAt
		nameCopy := name
		out.Buckets = append(out.Buckets, awss3types.Bucket{
			Name:         &nameCopy,
			CreationDate: &created,
		})
	}
	sort.Slice(out.Buckets, func(i, j int) bool {
		return *out.Buckets[i].Name < *out.Buckets[j].Name
	})
	return out, nil
}

// PutObject implements seaweedfs.s3BucketAPI. Enforces the per-bucket
// quota when one is set in the Filer metadata; rejects with
// QuotaExceeded when the upload would cross the ceiling.
func (s *Server) PutObject(_ context.Context, params *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	if params == nil || params.Bucket == nil || params.Key == nil {
		return nil, newFakeS3Error(400, "InvalidRequest", "bucket + key are required")
	}
	bucket := *params.Bucket
	key := *params.Key
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[bucket]
	if !ok {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", bucket)
	}
	var body []byte
	if params.Body != nil {
		read, err := io.ReadAll(params.Body)
		if err != nil {
			return nil, newFakeS3Error(400, "InvalidRequest", "read body: %v", err)
		}
		body = read
	}
	totalSize := int64(0)
	for _, v := range b.Objects {
		totalSize += int64(len(v))
	}
	if body != nil {
		totalSize += int64(len(body))
	}
	if qBytes, ok := s.quotaMap["/etc/seaweedfs/buckets/"+bucket+"/quota.json"]; ok {
		sizeMiB, fileCount := parseQuotaBytes(qBytes)
		if sizeMiB > 0 && totalSize > sizeMiB*1024*1024 {
			return nil, newFakeS3Error(507, "QuotaExceeded", "size quota exceeded")
		}
		if fileCount > 0 && int64(len(b.Objects))+1 > fileCount {
			return nil, newFakeS3Error(507, "QuotaExceeded", "file count quota exceeded")
		}
	}
	b.Objects[key] = body
	etag := "\"fake-etag-" + key + "\""
	return &awss3.PutObjectOutput{
		ETag: &etag,
	}, nil
}

// GetObject implements seaweedfs.s3BucketAPI.
func (s *Server) GetObject(_ context.Context, params *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	if params == nil || params.Bucket == nil || params.Key == nil {
		return nil, newFakeS3Error(400, "InvalidRequest", "bucket + key are required")
	}
	bucket := *params.Bucket
	key := *params.Key
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.buckets[bucket]
	if !ok {
		return nil, newFakeS3Error(404, "NoSuchBucket", "bucket %q does not exist", bucket)
	}
	body, ok := b.Objects[key]
	if !ok {
		return nil, newFakeS3Error(404, "NoSuchKey", "key %q does not exist in bucket %q", key, bucket)
	}
	contentLength := int64(len(body))
	return &awss3.GetObjectOutput{
		Body:          io.NopCloser(strings.NewReader(string(body))),
		ContentLength: &contentLength,
	}, nil
}

// ---------------------------------------------------------------------------
// presignAPI impl
// ---------------------------------------------------------------------------

// PresignGetObject implements seaweedfs.presignAPI. Produces a
// deterministic SigV4-shaped URL so tests can assert the bucket + key +
// expiry landed.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) PresignGetObject(_ context.Context, params *awss3.GetObjectInput, _ ...func(*awss3.PresignOptions)) (*seaweedfs.V4PresignedRequest, error) {
	if params == nil || params.Bucket == nil || params.Key == nil {
		return nil, errors.New("fake: bucket + key are required")
	}
	s.mu.Lock()
	s.presignCounter++
	s.mu.Unlock()
	url := fmt.Sprintf("https://fake-s3.example/%s/%s?X-Amz-Algorithm=AWS4-HMAC-SHA256",
		*params.Bucket, *params.Key)
	return &seaweedfs.V4PresignedRequest{URL: url, Method: "GET"}, nil
}

// PresignPutObject implements seaweedfs.presignAPI.
//
//nolint:lll // signature mirrors the SDK's; cannot wrap without losing readability.
func (s *Server) PresignPutObject(_ context.Context, params *awss3.PutObjectInput, _ ...func(*awss3.PresignOptions)) (*seaweedfs.V4PresignedRequest, error) {
	if params == nil || params.Bucket == nil || params.Key == nil {
		return nil, errors.New("fake: bucket + key are required")
	}
	s.mu.Lock()
	s.presignCounter++
	s.mu.Unlock()
	url := fmt.Sprintf("https://fake-s3.example/%s/%s?X-Amz-Algorithm=AWS4-HMAC-SHA256",
		*params.Bucket, *params.Key)
	return &seaweedfs.V4PresignedRequest{URL: url, Method: "PUT"}, nil
}

// ---------------------------------------------------------------------------
// filerAPI impl
// ---------------------------------------------------------------------------

// GetStatus implements seaweedfs.filerAPI.
func (s *Server) GetStatus(_ context.Context) (*seaweedfs.FilerStatus, error) {
	if s.pingFilerFail {
		return nil, errors.New("fake: filer status failed (pingFilerFail set)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	volumeCount := 1
	if s.clusterMode {
		volumeCount = 3
	}
	return &seaweedfs.FilerStatus{
		Version: s.filerVersion,
		Topology: &seaweedfs.FilerTopology{
			Free:              1024 * 1024 * 1024 * 100,
			Max:               1024 * 1024 * 1024 * 500,
			VolumeCount:       volumeCount,
			ActiveVolumeCount: volumeCount,
		},
	}, nil
}

// GetMetadata implements seaweedfs.filerAPI.
func (s *Server) GetMetadata(_ context.Context, path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if body, ok := s.quotaMap[path]; ok {
		return append([]byte(nil), body...), nil
	}
	if body, ok := s.metadata[path]; ok {
		return append([]byte(nil), body...), nil
	}
	if strings.HasPrefix(path, "/etc/seaweedfs/identities/") {
		ak := strings.TrimSuffix(strings.TrimPrefix(path, "/etc/seaweedfs/identities/"), ".json")
		if id, ok := s.identities[ak]; ok {
			return marshalIdentityRecord(id), nil
		}
	}
	return nil, seaweedfs.ErrNotFound
}

// PutMetadata implements seaweedfs.filerAPI. Routes to quotaMap when the
// path is a quota record; routes to the identities map for identity
// records so the fake can apply IAM semantics on read.
func (s *Server) PutMetadata(_ context.Context, path string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]byte(nil), body...)
	if strings.HasPrefix(path, "/etc/seaweedfs/buckets/") && strings.HasSuffix(path, "/quota.json") {
		s.quotaMap[path] = cp
		return nil
	}
	if strings.HasPrefix(path, "/etc/seaweedfs/identities/") {
		ak := strings.TrimSuffix(strings.TrimPrefix(path, "/etc/seaweedfs/identities/"), ".json")
		id := unmarshalIdentityRecord(ak, cp)
		s.identities[ak] = id
		return nil
	}
	s.metadata[path] = cp
	return nil
}

// DeleteMetadata implements seaweedfs.filerAPI. Idempotent.
func (s *Server) DeleteMetadata(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.quotaMap, path)
	delete(s.metadata, path)
	if strings.HasPrefix(path, "/etc/seaweedfs/identities/") {
		ak := strings.TrimSuffix(strings.TrimPrefix(path, "/etc/seaweedfs/identities/"), ".json")
		delete(s.identities, ak)
	}
	return nil
}

// ListMetadata implements seaweedfs.filerAPI.
func (s *Server) ListMetadata(_ context.Context, prefix string) (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte)
	for path, body := range s.metadata {
		if strings.HasPrefix(path, prefix) {
			out[path] = append([]byte(nil), body...)
		}
	}
	if strings.HasPrefix(prefix, "/etc/seaweedfs/identities") {
		for ak, id := range s.identities {
			path := "/etc/seaweedfs/identities/" + ak + ".json"
			out[path] = marshalIdentityRecord(id)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newFakeS3Error builds a smithy.APIError-compatible error carrying the
// given HTTP status code + AWS-style error code. The driver's
// translateS3Err reads ErrorCode + ErrorMessage (and the HTTP status
// via the httpresponseError interface) — fakeS3Error implements both
// so the translation works against the fake the same way it works
// against a real SeaweedFS S3 response.
func newFakeS3Error(status int, code, format string, args ...any) error {
	return &fakeS3Error{
		status:  status,
		code:    code,
		message: fmt.Sprintf(format, args...),
	}
}

// fakeS3Error implements smithy.APIError + the httpresponseError
// interface the driver's httpStatusFromErr helper looks for.
type fakeS3Error struct {
	status  int
	code    string
	message string
}

// ErrorCode implements smithy.APIError.
func (e *fakeS3Error) ErrorCode() string { return e.code }

// ErrorMessage implements smithy.APIError.
func (e *fakeS3Error) ErrorMessage() string { return e.message }

// ErrorFault implements smithy.APIError.
func (e *fakeS3Error) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

// Error implements error.
func (e *fakeS3Error) Error() string {
	return fmt.Sprintf("fake s3 error: %s: %s (http %d)", e.code, e.message, e.status)
}

// HTTPStatusCode exposes the HTTP status code for the driver's
// translateS3Err via the httpresponseError interface pattern.
func (e *fakeS3Error) HTTPStatusCode() int { return e.status }

// parseQuotaBytes extracts (SizeMiB, FileCount) from a JSON quota body
// written by the driver's quotas.go. Tolerates malformed bodies.
func parseQuotaBytes(body []byte) (sizeMiB, fileCount int64) {
	var rec struct {
		SizeMiB   int64 `json:"size_mib"`
		FileCount int64 `json:"file_count"`
	}
	_ = json.Unmarshal(body, &rec)
	return rec.SizeMiB, rec.FileCount
}

// marshalIdentityRecord serializes a fakeIdentity back into the JSON
// shape the driver wrote in (and expects to read back). Used by
// GetMetadata + ListMetadata.
func marshalIdentityRecord(id *fakeIdentity) []byte {
	rec := struct {
		Name      string   `json:"name"`
		AccessKey string   `json:"access_key"`
		SecretKey string   `json:"secret_key"`
		IsAdmin   bool     `json:"is_admin,omitempty"`
		Buckets   []string `json:"buckets,omitempty"`
		Actions   []string `json:"actions,omitempty"`
		Disabled  bool     `json:"disabled,omitempty"`
	}{
		Name:      id.Name,
		AccessKey: id.AccessKey,
		SecretKey: id.SecretKey,
		IsAdmin:   id.IsAdmin,
		Buckets:   id.Buckets,
		Actions:   id.Actions,
		Disabled:  id.Disabled,
	}
	body, _ := json.Marshal(rec)
	return body
}

// unmarshalIdentityRecord parses a JSON body the driver wrote into a
// fakeIdentity. Tolerates malformed bodies (returns a record keyed by
// accessKey with whatever fields decoded).
func unmarshalIdentityRecord(accessKey string, body []byte) *fakeIdentity {
	var rec struct {
		Name      string   `json:"name"`
		AccessKey string   `json:"access_key"`
		SecretKey string   `json:"secret_key"`
		IsAdmin   bool     `json:"is_admin"`
		Buckets   []string `json:"buckets"`
		Actions   []string `json:"actions"`
		Disabled  bool     `json:"disabled"`
	}
	_ = json.Unmarshal(body, &rec)
	ak := rec.AccessKey
	if ak == "" {
		ak = accessKey
	}
	return &fakeIdentity{
		Name:      rec.Name,
		AccessKey: ak,
		SecretKey: rec.SecretKey,
		IsAdmin:   rec.IsAdmin,
		Buckets:   rec.Buckets,
		Actions:   rec.Actions,
		Disabled:  rec.Disabled,
	}
}
