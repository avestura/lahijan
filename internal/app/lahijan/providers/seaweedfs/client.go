// Package seaweedfs: client.go is the entry point every area file calls
// through. It owns the AWS SDK S3 client + presign client (data plane +
// presign), the net/http.Client used by the Filer metadata path (control
// plane), the per-request timeout, the user-agent, the admin credentials,
// and the OTel span bootstrap for every outbound call.
//
// Production wires the client via NewClient from program.Start based on
// conf.providers.seaweedfs.*. Tests wire it via newClientWithDeps against
// in-memory operations mocks (see fake/server.go).
//
// Per ADR-0027 the S3 data + control plane operations go through the AWS
// SDK for Go v2 `service/s3` client (so SigV4 request signing and
// pre-signed URL generation come from the SDK). The SeaweedFS Filer
// metadata path — IAM identities, per-bucket quotas, status probes —
// is handled by a thin net/http.Client wrapper because SeaweedFS' Filer
// API is a daemon-specific REST surface (mirrors the WS-11 / WS-12
// driver shape).
package seaweedfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"go.opentelemetry.io/otel/attribute"
)

// userAgent is sent on every Filer request so SeaweedFS' access log can
// tell Lahijan driver traffic apart from other API clients. The AWS SDK
// client sends its own User-Agent (with the Lahijan driver token below
// appended) so SeaweedFS' S3 access logs can do the same.
const userAgent = "lahijan-seaweedfs-driver/0.1"

// maxFilerResponseBytes caps a single Filer response body so a
// misbehaving Filer cannot drive the driver OOM. 64 MiB covers the
// largest metadata payload Lahijan uses (a bucket listing in a
// 100k-bucket tenant).
const maxFilerResponseBytes = 64 * 1024 * 1024

// Provider implements providers.Provider plus the SeaweedFS-specific
// surface (buckets, IAM, quotas, presign, filer). The struct holds only
// immutable fields after NewClient, except for the lazily-populated
// capabilities cache (guarded by capabilitiesMu). All methods are safe
// for concurrent use.
//
// Per pillar 1 the Name() string is "seaweedfs" (internal-only); end
// users see "object storage".
type Provider struct {
	// s3 is the AWS SDK S3 client used for bucket + object operations
	// (control plane + data plane). nil when this Provider was built
	// with the test-only newClientWithDeps constructor against an
	// in-memory s3BucketAPI fake.
	s3 s3BucketAPI

	// presign is the AWS SDK S3 presign client used for pre-signed URL
	// generation. nil when this Provider was built with an injected
	// presignAPI fake.
	presign presignAPI

	// filer is the thin HTTP wrapper used for Filer metadata + IAM +
	// per-bucket quota configuration. nil when this Provider was built
	// with an injected filerAPI fake.
	filer filerAPI

	// httpClient executes every Filer HTTP request. In production the
	// Transport is a plain http.Transport; in tests it points at
	// httptest when the test exercises the real HTTP path. Owned by
	// the driver; closed via Shutdown.
	httpClient *http.Client

	// s3Endpoint is the configured S3 endpoint URL. Used in presign
	// results so the caller can see what the user will hit.
	s3Endpoint string

	// filerURL is the Filer HTTP origin the driver talks to for
	// metadata + IAM + quota configuration.
	filerURL string

	// region is the AWS region the SDK presents on every request.
	// SeaweedFS ignores it for auth purposes but the SDK requires a
	// non-empty value.
	region string

	// adminAccessKey is the admin access key the SeaweedFS S3 server
	// recognises as the root account. SENSITIVE — never logged.
	adminAccessKey string

	// adminSecretKey is the paired admin secret key. SENSITIVE.
	adminSecretKey string

	// corsOrigins are the browser origins written into each bucket's CORS
	// configuration (Config.CORSAllowedOrigins).
	corsOrigins []string

	// iamMu serialises rebuilds of the SeaweedFS IAM document
	// (/etc/iam/identity.json) so two concurrent mints cannot each
	// publish a listing that misses the other's identity.
	iamMu sync.Mutex

	// timeout is the per-request timeout applied via context.
	timeout time.Duration

	// defaultPresignTTL is the TTL applied when the caller does not
	// override it.
	defaultPresignTTL time.Duration

	// defaultQuota is the quota applied at bucket-create time when the
	// caller does not override it. Zero means "no backend quota".
	defaultQuota QuotaSpec

	// capabilities caches the daemon's feature flags after the first
	// Ping. Lazy-populated; consumers call Capabilities() which calls
	// Ping once if the cache is empty. Safe for concurrent use via
	// capabilitiesMu.
	capabilitiesMu sync.RWMutex
	capabilities   Capabilities
	capabilitiesOK bool

	// bus is the optional WASM event bus the synthesize-events hook
	// fans change events into. Nil when events are disabled.
	bus EventBus
}

// s3BucketAPI is the narrow subset of *s3.Client the driver uses.
// Declared as an interface so unit tests can substitute an in-memory
// fake without implementing the SDK's full client surface. Production
// wiring passes a real *awss3.Client (which satisfies this interface
// structurally).
//
//nolint:interfacebloat // intentionally groups the bucket + object ops.
type s3BucketAPI interface {
	CreateBucket(ctx context.Context, params *awss3.CreateBucketInput, optFns ...func(*awss3.Options)) (*awss3.CreateBucketOutput, error)
	DeleteBucket(ctx context.Context, params *awss3.DeleteBucketInput, optFns ...func(*awss3.Options)) (*awss3.DeleteBucketOutput, error)
	HeadBucket(ctx context.Context, params *awss3.HeadBucketInput, optFns ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error)
	ListBuckets(ctx context.Context, params *awss3.ListBucketsInput, optFns ...func(*awss3.Options)) (*awss3.ListBucketsOutput, error)
	PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	// PutBucketCors lets browsers use presigned URLs from the dashboard
	// origin (see bucket_cors.go).
	PutBucketCors(ctx context.Context, params *awss3.PutBucketCorsInput, optFns ...func(*awss3.Options)) (*awss3.PutBucketCorsOutput, error)
	// WS-29: bucket lifecycle + versioning + object lock. The lifecycle
	// evaluator worker also uses DeleteObject / DeleteObjects /
	// ListObjectVersions / AbortMultipartUpload / CopyObject (restore)
	// to enforce the configured policy server-side.
	PutBucketVersioning(
		ctx context.Context, params *awss3.PutBucketVersioningInput,
		optFns ...func(*awss3.Options),
	) (*awss3.PutBucketVersioningOutput, error)
	GetBucketVersioning(
		ctx context.Context, params *awss3.GetBucketVersioningInput,
		optFns ...func(*awss3.Options),
	) (*awss3.GetBucketVersioningOutput, error)
	ListObjectVersions(
		ctx context.Context, params *awss3.ListObjectVersionsInput,
		optFns ...func(*awss3.Options),
	) (*awss3.ListObjectVersionsOutput, error)
	PutBucketLifecycleConfiguration(
		ctx context.Context, params *awss3.PutBucketLifecycleConfigurationInput,
		optFns ...func(*awss3.Options),
	) (*awss3.PutBucketLifecycleConfigurationOutput, error)
	GetBucketLifecycleConfiguration(
		ctx context.Context, params *awss3.GetBucketLifecycleConfigurationInput,
		optFns ...func(*awss3.Options),
	) (*awss3.GetBucketLifecycleConfigurationOutput, error)
	DeleteBucketLifecycle(
		ctx context.Context, params *awss3.DeleteBucketLifecycleInput,
		optFns ...func(*awss3.Options),
	) (*awss3.DeleteBucketLifecycleOutput, error)
	PutObjectLockConfiguration(
		ctx context.Context, params *awss3.PutObjectLockConfigurationInput,
		optFns ...func(*awss3.Options),
	) (*awss3.PutObjectLockConfigurationOutput, error)
	GetObjectLockConfiguration(
		ctx context.Context, params *awss3.GetObjectLockConfigurationInput,
		optFns ...func(*awss3.Options),
	) (*awss3.GetObjectLockConfigurationOutput, error)
	DeleteObject(
		ctx context.Context, params *awss3.DeleteObjectInput,
		optFns ...func(*awss3.Options),
	) (*awss3.DeleteObjectOutput, error)
	DeleteObjects(
		ctx context.Context, params *awss3.DeleteObjectsInput,
		optFns ...func(*awss3.Options),
	) (*awss3.DeleteObjectsOutput, error)
	AbortMultipartUpload(
		ctx context.Context, params *awss3.AbortMultipartUploadInput,
		optFns ...func(*awss3.Options),
	) (*awss3.AbortMultipartUploadOutput, error)
	CopyObject(
		ctx context.Context, params *awss3.CopyObjectInput,
		optFns ...func(*awss3.Options),
	) (*awss3.CopyObjectOutput, error)
}

// presignAPI is the narrow subset of *awss3.PresignClient the driver
// uses. Production wiring passes a real *awss3.PresignClient (which
// satisfies this interface structurally).
type presignAPI interface {
	PresignGetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.PresignOptions)) (*V4PresignedRequest, error)
	PresignPutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.PresignOptions)) (*V4PresignedRequest, error)
}

// V4PresignedRequest is the value type returned by the AWS SDK presign
// client. We re-declare a structurally-equivalent local type so the
// presignAPI interface can be mocked without callers importing the
// smithy-go v4 package directly.
type V4PresignedRequest struct {
	URL    string
	Method string
}

// filerAPI is the narrow interface the Filer HTTP wrapper exposes.
// Production wiring passes the driver's own *filerHTTPClient; tests
// inject an in-memory fake.
type filerAPI interface {
	// GetStatus probes the Filer root for the version + cluster
	// topology summary. Used by Ping + GetClusterStatus.
	GetStatus(ctx context.Context) (*FilerStatus, error)

	// GetMetadata reads a single metadata record at the given path.
	// Returns ErrNotFound when the path does not exist.
	GetMetadata(ctx context.Context, path string) ([]byte, error)

	// PutMetadata writes a metadata record at the given path.
	// Creates the path if missing; overwrites if present.
	PutMetadata(ctx context.Context, path string, body []byte) error

	// DeleteMetadata removes a metadata record. Idempotent: a missing
	// path returns nil.
	DeleteMetadata(ctx context.Context, path string) error

	// ListMetadata lists every (path, body) tuple under the given
	// prefix. Returns an empty map when no records match.
	ListMetadata(ctx context.Context, prefix string) (map[string][]byte, error)
}

// EventBus is the minimal subset of *eventbus.Bus the driver needs.
// Mirrors the WS-11 (Incus) and WS-12 (PowerDNS) drivers so the same
// bus adapter in program/ works for all three providers.
type EventBus interface {
	// Emit mirrors eventbus.Bus.Emit. The driver calls it from the
	// same goroutine as the change; errors are logged but do not
	// revert the change.
	Emit(ctx context.Context, e BusEvent) error
}

// BusEvent is the minimal event shape the driver emits into the WASM bus.
// Mirrors eventbus.Event; defined here so callers do not need to import
// the eventbus package to use the driver.
type BusEvent struct {
	// Topic is the dotted identifier ("storage.bucket.created").
	Topic string

	// TenantID is the optional tenant scope; nil when the event is
	// system-level.
	TenantID *string

	// ActorType is "user", "system", or "plugin" (mirrors audit).
	ActorType string

	// ResourceID is the optional id of the resource the event is about.
	ResourceID *string

	// Metadata is the free-form JSON blob; the listener decodes per
	// the event's documented schema in wasm/eventbus/events.go.
	Metadata []byte
}

// Capabilities is the per-SeaweedFS feature-flag bundle returned by
// Capabilities(). Mirrors providers.Capabilities but kept as a value
// type (not a pointer) so callers can copy it freely.
type Capabilities struct {
	// ClusterMode is true when SeaweedFS runs in multi-master / multi-
	// volume topology (prod split). False for `weed mini` dev deploys.
	ClusterMode bool

	// RemoteReplication is true when SeaweedFS is configured to sink
	// objects to a remote peer. Reserved for Phase 7 (WS-29).
	RemoteReplication bool

	// QuotasEnforced is true when the SeaweedFS S3 server is configured
	// to honour per-bucket quotas from Filer metadata. True by default
	// in MVP; a future config knob can flip it off for capacity-mode
	// billing.
	QuotasEnforced bool

	// ServerVersion is the SeaweedFS Filer version string (e.g.
	// "3.61-fake").
	ServerVersion string

	// PresignSupported is always true for the S3 surface; kept as a
	// flag so the storage service can stay backend-agnostic if a
	// future provider (e.g. Azure Blob) lands without presign support.
	PresignSupported bool
}

// Config is the bundle passed to NewClient by program.Start.
type Config struct {
	// HTTPClient is the configured *http.Client used by the Filer
	// metadata path. Required.
	HTTPClient *http.Client

	// S3 is the configured AWS SDK S3 client. When nil, NewClient
	// builds one from S3Endpoint + AdminAccessKey + AdminSecretKey +
	// Region. Tests pass a fake to skip the SDK transport entirely.
	S3 s3BucketAPI

	// Presign is the configured AWS SDK presign client. When nil,
	// NewClient builds one from S3 (or from S3Endpoint if S3 is also
	// nil). Tests pass a fake.
	Presign presignAPI

	// Filer is the configured Filer HTTP wrapper. When nil, NewClient
	// builds one from FilerURL + HTTPClient. Tests pass a fake.
	Filer filerAPI

	// S3Endpoint is the S3 endpoint URL the AWS SDK client targets.
	// Required when S3 or Presign is nil.
	S3Endpoint string

	// PublicS3Endpoint is the S3 origin end users reach (e.g.
	// https://s3.example.com). SigV4 signs the Host header, so presigned
	// URLs must be generated against it rather than the internal
	// S3Endpoint (http://seaweed-s3:8333), which no browser can resolve.
	// Empty falls back to S3Endpoint.
	PublicS3Endpoint string

	// FilerURL is the Filer HTTP API origin. Required when Filer is
	// nil.
	FilerURL string

	// Region is the AWS region the SDK presents on every request.
	// Defaults to "us-east-1" when empty.
	Region string

	// AdminAccessKey is the admin access key the SeaweedFS S3 server
	// recognises. SENSITIVE — never logged. Required.
	AdminAccessKey string

	// AdminSecretKey is the paired admin secret key. SENSITIVE.
	// Required.
	AdminSecretKey string

	// RequestTimeout is the per-call timeout. Zero means use the
	// default 30s.
	RequestTimeout time.Duration

	// DefaultPresignTTL is the TTL applied to presigned URLs when the
	// caller does not override it. Zero means use 1 hour.
	DefaultPresignTTL time.Duration

	// DefaultQuota is the per-bucket quota applied at bucket-create
	// time when the caller does not override it. Zero means no backend
	// quota (Lahijan enforces via metering instead).
	DefaultQuota QuotaSpec

	// CORSAllowedOrigins are the browser origins (the dashboard) allowed
	// to call the S3 data plane with presigned URLs. Applied to every
	// bucket at create time and backfilled by EnsureBucketCORS. Empty
	// skips CORS entirely (browser uploads then fail the preflight).
	CORSAllowedOrigins []string

	// Bus is the optional WASM event bus for change-event synthesis.
	// Nil disables event synthesis.
	Bus EventBus
}

// defaultRequestTimeout is used when Config.RequestTimeout is zero.
const defaultRequestTimeout = 30 * time.Second

// defaultPresignTTLDefault is used when Config.DefaultPresignTTL is zero.
const defaultPresignTTLDefault = time.Hour

// defaultRegion is used when Config.Region is empty. SeaweedFS ignores
// the region for auth purposes but the AWS SDK requires a non-empty
// value.
const defaultRegion = "us-east-1"

// NewClient builds a Provider from the given Config. The returned
// provider has not yet contacted the backend; the first call to Ping
// (or the first request) will. Returns an error if HTTPClient,
// FilerURL, or admin credentials are missing.
func NewClient(cfg Config) (*Provider, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("seaweedfs: Config.HTTPClient is required")
	}
	if cfg.AdminAccessKey == "" {
		return nil, errors.New("seaweedfs: Config.AdminAccessKey is required")
	}
	if cfg.AdminSecretKey == "" {
		return nil, errors.New("seaweedfs: Config.AdminSecretKey is required")
	}

	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	presignTTL := cfg.DefaultPresignTTL
	if presignTTL <= 0 {
		presignTTL = defaultPresignTTLDefault
	}
	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}

	// Build the AWS SDK clients when the caller did not inject any. Tests
	// that want to bypass the SDK transport pass all three (S3 + Presign
	// + Filer); production passes none and gets the real clients.
	s3Client := cfg.S3
	if s3Client == nil {
		if cfg.S3Endpoint == "" {
			return nil, errors.New("seaweedfs: Config.S3Endpoint is required when S3 is nil")
		}
		s3Client = buildS3Client(cfg.S3Endpoint, region, cfg.AdminAccessKey, cfg.AdminSecretKey)
	}

	presignClient := cfg.Presign
	if presignClient == nil {
		// If the caller injected an S3 client, derive the presign client
		// from it via the SDK's PresignClient method. Otherwise build a
		// fresh SDK client purely for presign. A public endpoint always
		// gets its own client so the signature covers the public Host.
		if cfg.PublicS3Endpoint != "" {
			presignClient = presignFromS3(buildS3Client(cfg.PublicS3Endpoint, region, cfg.AdminAccessKey, cfg.AdminSecretKey))
		} else if realS3, ok := s3Client.(*awss3.Client); ok {
			presignClient = presignFromS3(realS3)
		} else {
			presignClient = presignFromS3(buildS3Client(cfg.S3Endpoint, region, cfg.AdminAccessKey, cfg.AdminSecretKey))
		}
	}

	filerClient := cfg.Filer
	if filerClient == nil {
		if cfg.FilerURL == "" {
			return nil, errors.New("seaweedfs: Config.FilerURL is required when Filer is nil")
		}
		filerClient = newFilerHTTPClient(cfg.HTTPClient, cfg.FilerURL, timeout)
	}

	return &Provider{
		s3:                s3Client,
		presign:           presignClient,
		filer:             filerClient,
		httpClient:        cfg.HTTPClient,
		s3Endpoint:        strings.TrimRight(cfg.S3Endpoint, "/"),
		filerURL:          strings.TrimRight(cfg.FilerURL, "/"),
		region:            region,
		adminAccessKey:    cfg.AdminAccessKey,
		adminSecretKey:    cfg.AdminSecretKey,
		timeout:           timeout,
		defaultPresignTTL: presignTTL,
		defaultQuota:      cfg.DefaultQuota,
		corsOrigins:       append([]string(nil), cfg.CORSAllowedOrigins...),
		bus:               cfg.Bus,
	}, nil
}

// buildS3Client constructs an AWS SDK S3 client pointed at the given
// SeaweedFS S3 endpoint with static credentials and path-style addressing.
// Path-style is mandatory for SeaweedFS (it does not support
// virtual-hosted-style addressing).
func buildS3Client(endpoint, region, accessKey, secretKey string) *awss3.Client {
	return awss3.New(awss3.Options{
		Region:       region,
		Credentials:  awscredentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
	})
}

// presignFromS3 builds a presignAPI backed by the AWS SDK PresignClient
// for the given *awss3.Client. The wrapper converts the SDK's
// *v4.PresignedHTTPRequest into our local v4PresignedRequest so callers
// do not import smithy-go v4 directly.
func presignFromS3(c *awss3.Client) presignAPI {
	return &sdkPresignAdapter{inner: awss3.NewPresignClient(c)}
}

// sdkPresignAdapter wraps *awss3.PresignClient to satisfy presignAPI.
type sdkPresignAdapter struct {
	inner *awss3.PresignClient
}

// PresignGetObject implements presignAPI.
//
//nolint:lll // signature matches the SDK's; cannot wrap without losing readability.
func (a *sdkPresignAdapter) PresignGetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.PresignOptions)) (*V4PresignedRequest, error) {
	req, err := a.inner.PresignGetObject(ctx, params, optFns...)
	if err != nil {
		return nil, err
	}
	return &V4PresignedRequest{URL: req.URL, Method: req.Method}, nil
}

// PresignPutObject implements presignAPI.
//
//nolint:lll // signature matches the SDK's; cannot wrap without losing readability.
func (a *sdkPresignAdapter) PresignPutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.PresignOptions)) (*V4PresignedRequest, error) {
	req, err := a.inner.PresignPutObject(ctx, params, optFns...)
	if err != nil {
		return nil, err
	}
	return &V4PresignedRequest{URL: req.URL, Method: req.Method}, nil
}

// newFilerHTTPClient builds a filerAPI backed by a net/http.Client that
// speaks the SeaweedFS Filer REST API.
func newFilerHTTPClient(httpClient *http.Client, baseURL string, timeout time.Duration) filerAPI {
	return &filerHTTPClient{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
		timeout:    timeout,
	}
}

// filerHTTPClient implements filerAPI against a real SeaweedFS Filer.
type filerHTTPClient struct {
	httpClient *http.Client
	baseURL    string
	timeout    time.Duration
}

// GetStatus implements filerAPI.
func (c *filerHTTPClient) GetStatus(ctx context.Context) (*FilerStatus, error) {
	var status FilerStatus
	if err := c.do(ctx, http.MethodGet, "/", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// GetMetadata implements filerAPI.
func (c *filerHTTPClient) GetMetadata(ctx context.Context, path string) ([]byte, error) {
	return c.doRaw(ctx, http.MethodGet, normalizeFilerPath(path), nil)
}

// PutMetadata implements filerAPI.
func (c *filerHTTPClient) PutMetadata(ctx context.Context, path string, body []byte) error {
	// PUT, not POST: the Filer stores a PUT body verbatim, while POST
	// requires multipart/form-data and answers 500 to a raw JSON body.
	_, err := c.doRawWithHeader(ctx, http.MethodPut, normalizeFilerPath(path), body, http.Header{
		"Content-Type": []string{"application/json"},
	})
	return err
}

// DeleteMetadata implements filerAPI.
func (c *filerHTTPClient) DeleteMetadata(ctx context.Context, path string) error {
	_, err := c.doRaw(ctx, http.MethodDelete, normalizeFilerPath(path), nil)
	return err
}

// ListMetadata implements filerAPI. SeaweedFS' Filer exposes a directory
// listing as JSON at the prefix path; we flatten the response into a
// (path -> body) map.
func (c *filerHTTPClient) ListMetadata(ctx context.Context, prefix string) (map[string][]byte, error) {
	raw, err := c.doRaw(ctx, http.MethodGet, normalizeFilerPath(prefix), nil)
	if err != nil {
		return nil, err
	}
	// SeaweedFS Filer directory listing returns a JSON object:
	// {"Path":"/...","Entries":[{"FullPath":"/.../sub","Name":"..."}]}
	// We resolve each Entry's FullPath by issuing a follow-up GET.
	var listing struct {
		Entries []struct {
			FullPath string `json:"FullPath"`
		} `json:"Entries"`
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		return nil, fmt.Errorf("seaweedfs: decode filer listing at %q: %w", prefix, err)
	}
	out := make(map[string][]byte, len(listing.Entries))
	for _, e := range listing.Entries {
		if e.FullPath == "" {
			continue
		}
		body, gerr := c.doRaw(ctx, http.MethodGet, e.FullPath, nil)
		if gerr != nil {
			// A missing entry between listing and read is fine; skip it.
			continue
		}
		out[e.FullPath] = body
	}
	return out, nil
}

// do issues a Filer request and decodes the JSON response into out. path
// is the absolute path under the Filer root (with a leading slash).
func (c *filerHTTPClient) do(ctx context.Context, method, path string, body, out any) error {
	ctx, span := startSpan(ctx, "filer.http."+strings.ToLower(method),
		attribute.String("seaweedfs.filer_path", path))
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := c.buildRequest(ctx, method, path, body)
	if err != nil {
		setStatus(span, err)
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: http %s %s: %w", method, path, err)
	}
	if classErr := classifyFiler(resp); classErr != nil {
		setStatus(span, classErr)
		return fmt.Errorf("seaweedfs: http %s %s: %w", method, path, classErr)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFilerResponseBytes))
	if err != nil {
		setStatus(span, err)
		_ = resp.Body.Close()
		return fmt.Errorf("seaweedfs: read filer response: %w", err)
	}
	_ = resp.Body.Close()
	if out == nil || len(raw) == 0 {
		setStatus(span, nil)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: decode filer response %s %s: %w", method, path, err)
	}
	setStatus(span, nil)
	return nil
}

// doRaw is like do but returns the raw response body without JSON decoding.
func (c *filerHTTPClient) doRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	return c.doRawWithHeader(ctx, method, path, body, nil)
}

// doRawWithHeader is the lower-level variant that lets the caller attach
// extra headers (Content-Type for metadata writes).
func (c *filerHTTPClient) doRawWithHeader(ctx context.Context, method, path string, body any, extra http.Header) ([]byte, error) {
	ctx, span := startSpan(ctx, "filer.http."+strings.ToLower(method),
		attribute.String("seaweedfs.filer_path", path))
	defer span.End()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := c.buildRequest(ctx, method, path, body)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		err = wrapCtxErr(err)
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: http %s %s: %w", method, path, err)
	}
	if classErr := classifyFiler(resp); classErr != nil {
		setStatus(span, classErr)
		return nil, fmt.Errorf("seaweedfs: http %s %s: %w", method, path, classErr)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFilerResponseBytes))
	if err != nil {
		setStatus(span, err)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("seaweedfs: read filer response: %w", err)
	}
	_ = resp.Body.Close()
	setStatus(span, nil)
	return raw, nil
}

// buildRequest constructs the *http.Request with the standard headers.
func (c *filerHTTPClient) buildRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			reader = bytes.NewReader(v)
		case string:
			reader = strings.NewReader(v)
		case io.Reader:
			reader = v
		default:
			buf, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("seaweedfs: marshal filer body: %w", err)
			}
			reader = bytes.NewReader(buf)
		}
	}
	fullURL := c.baseURL + path
	if _, err := url.Parse(fullURL); err != nil {
		return nil, fmt.Errorf("seaweedfs: invalid filer url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("seaweedfs: build filer request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// normalizeFilerPath enforces a leading slash on a Filer metadata path
// so the URL concatenation in buildRequest is always well-formed.
func normalizeFilerPath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}
