// Package storage: presign.go generates pre-signed GET / PUT URLs for
// limited-time direct access to a single object in a bucket. Per
// ADR-0011 the data plane is direct from the user's S3 client to
// SeaweedFS; pre-signed URLs are how the dashboard hands a user a
// temporary download/upload link without proxying the bytes through
// Lahijan.
//
// The signing itself is delegated to the SeaweedFS driver
// (PresignGetObject / PresignPutObject). The storage service enforces
// the per-call TTL clamp + emits an audit row so every presign issuance
// is traceable. The actual signature carries the SeaweedFS admin
// credential today; per WS-16 "Open questions" item 3, object-level
// audit relies on SeaweedFS access logs (Lahijan does not proxy bytes).
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// PresignMethod enumerates the HTTP methods the presign path accepts.
type PresignMethod string

const (
	// PresignGet produces a GET URL (download).
	PresignGet PresignMethod = "GET"
	// PresignPut produces a PUT URL (upload).
	PresignPut PresignMethod = "PUT"
)

// PresignParams carries the user-controlled fields of a presign call.
type PresignParams struct {
	// Method is "GET" (download) or "PUT" (upload).
	Method PresignMethod
	// Key is the object key within the bucket. May be empty for a
	// bucket-level presign (e.g. list operations); the daemon treats
	// an empty key as the bucket root.
	Key string
	// ExpiresIn is the URL lifetime. Clamped to [1s, MaxPresignTTL];
	// zero means DefaultPresignTTL.
	ExpiresIn time.Duration
}

// PresignResult is the user-facing shape of a presign call. Mirrors the
// provider's seaweedfs.PresignResult but re-declared here so the API
// handler does not import the driver package.
type PresignResult struct {
	// URL is the SigV4-signed URL the user opens directly against
	// SeaweedFS.
	URL string
	// Method is "GET" or "PUT".
	Method string
	// Bucket is the canonical bucket name the URL targets.
	Bucket string
	// Key is the object key within the bucket.
	Key string
	// ExpiresAt is when the signature stops being valid.
	ExpiresAt time.Time
}

// Presign orchestrates a presign URL generation: validate -> audit
// pending -> driver presign -> event emit -> audit outcome. Returns the
// freshly-signed URL.
//
// The audit row is emitted for every presign call because pre-signed
// URLs grant time-limited data-plane access; the audit trail must be
// able to answer "who issued a URL for this object on this date".
func (s *Service) Presign(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	params PresignParams,
) (*PresignResult, error) {
	if s.provider == nil {
		return nil, ErrProviderDisabled
	}
	if err := validatePresignParams(params); err != nil {
		return nil, err
	}
	row, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("storage.presign: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditPresign,
		ResourceType: ResourceBucket,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": row.Slug,
			"method":      string(params.Method),
			"key":         params.Key,
			"expires_in":  params.ExpiresIn.String(),
		},
	})

	ttl := params.ExpiresIn
	if ttl == 0 {
		ttl = DefaultPresignTTL
	}

	var res *seaweedfs.PresignResult
	switch params.Method {
	case PresignGet:
		res, err = s.provider.PresignGetObject(ctx, row.Name, params.Key, ttl)
	case PresignPut:
		res, err = s.provider.PresignPutObject(ctx, row.Name, params.Key, ttl)
	}
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return nil, fmt.Errorf("storage.presign: %w", err)
	}

	out := &PresignResult{
		URL:       res.URL,
		Method:    res.Method,
		Bucket:    res.Bucket,
		Key:       res.Key,
		ExpiresAt: res.ExpiresAt,
	}

	s.emitEvent(ctx, eventbus.S3PresignIssued, tenantID, userID, row.ID, map[string]any{
		"bucket_slug": row.Slug,
		"method":      out.Method,
		"key":         out.Key,
		"expires_at":  out.ExpiresAt.Format(time.RFC3339),
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"expires_at": out.ExpiresAt.Format(time.RFC3339),
	}})
	return out, nil
}

// validatePresignParams checks the precondition for a Presign call.
func validatePresignParams(params PresignParams) error {
	switch params.Method {
	case PresignGet, PresignPut:
	default:
		return ErrInvalidPresignMethod
	}
	if params.ExpiresIn < 0 {
		return fmt.Errorf("%w: negative ttl", ErrInvalidPresignTTL)
	}
	if params.ExpiresIn > MaxPresignTTL {
		return fmt.Errorf("%w: ttl %s exceeds max %s", ErrInvalidPresignTTL, params.ExpiresIn, MaxPresignTTL)
	}
	return nil
}
