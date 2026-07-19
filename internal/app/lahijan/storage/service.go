// Package storage implements Lahijan's user-facing object storage module
// (WS-16). It orchestrates calls to the SeaweedFS provider driver (Phase 3,
// WS-13), the tenant-scoped storage_buckets + storage_credentials tables
// (this WS), the RBAC policy + audit emitter (WS-08), and the WASM event
// bus (WS-10b).
//
// Layering:
//
//	api/storage_handlers.go  -> storage.Service -> providers/seaweedfs + database
//	                                       \-> auth/audit
//	                                       \-> wasm/eventbus
//
// Every privileged action calls RequirePerm via the api/middleware gate;
// every state-changing action emits an audit event before the side effect
// (status=pending) and marks the outcome after. Every bucket / credential
// change also emits into the WASM event bus so plugins can react.
//
// Per ADR-0011 the data plane is direct from the user's S3 client to
// SeaweedFS; Lahijan only manages the control plane (bucket CRUD,
// credential minting, quota configuration, presign URL generation).
//
// Per pillar 1, this package is named "storage" — never "seaweedfs". End
// users do not see the word SeaweedFS anywhere in the API or UI.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// Resource type + audit action constants for the storage module. Past-tense
// verbs for actions so the audit row reads "what happened"; i18n keys are
// auto-derived (dots -> underscores).
const (
	ResourceBucket     = audit.ResourceBucket
	ResourceCredential = audit.ResourceCredential

	AuditBucketCreate     = audit.ActionS3BucketCreate
	AuditBucketUpdate     = audit.ActionS3BucketUpdate
	AuditBucketDelete     = audit.ActionS3BucketDelete
	AuditBucketQuotaSet   = audit.ActionS3BucketQuotaSet
	AuditCredentialMint   = audit.ActionS3CredentialMint
	AuditCredentialRevoke = audit.ActionS3CredentialRevoke
	AuditPresign          = audit.ActionS3Presign
)

// DefaultPresignTTL is applied when the caller does not pass one. Mirrors
// the SeaweedFS driver default so the two layers agree on the URL lifetime
// absent caller input.
const DefaultPresignTTL = time.Hour

// MaxPresignTTL caps the lifetime of a single pre-signed URL. 24h matches
// the AWS S3 ceiling so a Lahijan-minted URL does not outlive what the
// SigV4 algorithm itself supports.
const MaxPresignTTL = 24 * time.Hour

// DefaultCredentialTTL is applied when the caller requests expiry=true
// but does not specify a duration. 90 days per WS-16 "Open questions"
// item 2.
const DefaultCredentialTTL = 90 * 24 * time.Hour

// MaxCredentialLabelLen caps the user-supplied label length so a
// misbehaving client cannot drive unbounded storage in the column.
const MaxCredentialLabelLen = 100

// Service is the entrypoint every storage API handler talks to. It owns
// the SeaweedFS provider (Phase 3 driver), the tenant-scoped repositories,
// the audit emitter, the WASM event bus, and the RBAC policy evaluator.
// Every method takes a context carrying the tenant id (set by the tenant
// middleware) and the user id (set by the auth middleware); the service
// enforces RBAC at the api/middleware layer for HTTP requests.
type Service struct {
	provider swProvider
	repos    *database.Repos
	audit    audit.Emitter
	bus      eventBus
	policy   rbac.PolicyEvaluator
	config   Config
}

// swProvider is the narrow seam the service needs from
// providers/seaweedfs.Provider. Defined here so tests can swap a fake
// without dragging the full provider surface into the test file. Split
// into per-area sub-interfaces so the surface stays reviewable; the
// concrete *seaweedfs.Provider satisfies the union.
type swProvider interface {
	swBucketOps
	swCredentialOps
	swQuotaOps
	swPresignOps
}

// swBucketOps covers bucket CRUD + health.
type swBucketOps interface {
	Ping(ctx context.Context) error
	CreateBucket(ctx context.Context, params seaweedfs.CreateBucketParams) (*seaweedfs.Bucket, error)
	HeadBucket(ctx context.Context, bucket string) error
	ListBuckets(ctx context.Context) ([]seaweedfs.Bucket, error)
	DeleteBucket(ctx context.Context, bucket string) error
}

// swCredentialOps covers identity mint + revoke.
type swCredentialOps interface {
	MintCredentials(ctx context.Context, params seaweedfs.MintCredentialsParams) (*seaweedfs.IAMCredential, error)
	RevokeCredentials(ctx context.Context, accessKey string) error
}

// swQuotaOps covers per-bucket quota configuration.
type swQuotaOps interface {
	SetBucketQuota(ctx context.Context, bucket string, quota seaweedfs.QuotaSpec) error
	GetBucketQuota(ctx context.Context, bucket string) (seaweedfs.QuotaSpec, error)
}

// swPresignOps covers pre-signed URL generation.
type swPresignOps interface {
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration) (*seaweedfs.PresignResult, error)
	PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration) (*seaweedfs.PresignResult, error)
}

// eventBus is the narrow seam the service needs from *eventbus.Bus.
type eventBus interface {
	Emit(ctx context.Context, e eventbus.Event) error
}

// Config carries the process-wide knobs the service needs.
type Config struct {
	// DefaultQuotaBytes is the per-bucket size quota applied at create
	// time when the caller does not override it. Zero means "no backend
	// quota" (the bucket is unbounded; Lahijan enforces via metering).
	DefaultQuotaBytes int64

	// DefaultQuotaObjects is the per-bucket object-count quota applied
	// at create time when the caller does not override it. Zero means
	// "no backend quota".
	DefaultQuotaObjects int64
}

// New builds a Service. Every dependency is required except `bus` and
// `policy` (nil disables event emission / non-HTTP RequirePerm).
func New(
	provider swProvider,
	r *database.Repos,
	emitter audit.Emitter,
	bus eventBus,
	policy rbac.PolicyEvaluator,
	cfg Config,
) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	return &Service{
		provider: provider,
		repos:    r,
		audit:    emitter,
		bus:      bus,
		policy:   policy,
		config:   cfg,
	}
}

// hashSecret returns the sha256 hex digest of the secret. Used by the
// credential mint path so the Lahijan DB stores only a forensic
// fingerprint of the plaintext secret; the plaintext is shown to the
// caller exactly once at mint time and then forgotten.
//
// sha256 (not argon2id) is sufficient here because the secret is a
// 40-character random base32 string the provider minted — no rainbow
// table or dictionary attack is realistic. Argon2id is reserved for
// user-chosen passwords (auth/password.Hasher).
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// emitEvent is a thin wrapper that drops the event on the floor when no
// bus is configured (dev / unit tests). Mirrors dns.Service.emitEvent.
func (s *Service) emitEvent(
	ctx context.Context,
	topic string,
	tenantID, userID, resourceID uuid.UUID,
	meta map[string]any,
) {
	if s.bus == nil {
		return
	}
	var raw []byte
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = b
		}
	}
	tid := tenantID
	uid := userID
	rid := resourceID
	_ = s.bus.Emit(ctx, eventbus.Event{
		Topic:      topic,
		TenantID:   &tid,
		ActorType:  audit.ActorUser,
		ActorID:    &uid,
		ResourceID: &rid,
		Metadata:   raw,
		EmittedAt:  time.Now(),
	})
}
