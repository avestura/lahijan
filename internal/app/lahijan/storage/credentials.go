// Package storage: credentials.go mints, lists, and revokes per-user S3
// credentials scoped to a single bucket. Per ADR-0011 the plaintext
// secret is returned to the caller EXACTLY ONCE at mint time; the
// Lahijan DB stores only the sha256 fingerprint (secret_hash) plus the
// metadata the dashboard needs to render the credential list.
//
// The credential is scoped to a single bucket via the SeaweedFS IAM
// subsystem; the daemon enforces the scope server-side on every S3
// request. Cross-bucket access is impossible by construction because
// the IAM identity record lists exactly one bucket.
//
// Revocation is two-layer: the SeaweedFS identity is removed (so the
// access key stops signing immediately) and the Lahijan row is
// soft-deleted (revoked_at set, row stays for the audit trail).
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/seaweedfs"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// CredentialAction is the high-level verb a credential permits on the
// scoped bucket. The storage service expands each action into the
// SeaweedFS-style bucket-scoped action string ("Read:<bucket>",
// "Write:<bucket>", ...) at mint time; the high-level form is cached on
// the storage_credentials row for the dashboard.
type CredentialAction string

const (
	// ActionRead permits GetObject + ListBucket on the scoped bucket.
	ActionRead CredentialAction = "Read"
	// ActionWrite permits PutObject + DeleteObject. Implies Read.
	ActionWrite CredentialAction = "Write"
	// ActionList permits ListBucket only (no Get). Useful for
	// inventory plugins.
	ActionList CredentialAction = "List"
	// ActionTagging permits PutObjectTagging + DeleteObjectTagging.
	ActionTagging CredentialAction = "Tagging"
	// ActionAdmin grants every action including bucket-level
	// operations. Reserved for the bucket owner.
	ActionAdmin CredentialAction = "Admin"
)

// supportedActions is the set the mint path accepts. Kept in sync with
// providers/seaweedfs.IAMAction; re-declared here so the storage package
// does not import the driver's internals.
var supportedActions = map[CredentialAction]seaweedfs.IAMAction{
	ActionRead:    seaweedfs.IAMActionRead,
	ActionWrite:   seaweedfs.IAMActionWrite,
	ActionList:    seaweedfs.IAMActionList,
	ActionTagging: seaweedfs.IAMActionTagging,
	ActionAdmin:   seaweedfs.IAMActionAdmin,
}

// CredentialMintParams carries the user-controlled fields of a mint call.
// At least one Action is required; ExpiresAt is optional (nil = no
// expiry). Label is optional but capped at MaxCredentialLabelLen.
type CredentialMintParams struct {
	// Label is the user-facing label so the user can tell credentials
	// apart in the dashboard ("prod-uploader", "ci-runner", ...).
	Label string
	// Actions is the list of high-level actions the credential
	// permits. Each is applied to the bucket. Required: at least one.
	Actions []CredentialAction
	// ExpiresAt is the optional expiry. When set, the WS-17 janitor
	// revokes the credential past this timestamp; the SeaweedFS daemon
	// does NOT enforce S3 expiries today. Nil means "no expiry".
	ExpiresAt *time.Time
}

// MintedCredential is the result of a Mint call. The SecretKey is
// SENSITIVE — the caller (api handler) returns it to the user ONCE at
// mint time and never persists it. The Row field is the cached Lahijan
// DB row (without the secret) so the handler can render the metadata
// alongside the one-time secret.
type MintedCredential struct {
	// Row is the cached Lahijan DB row. Subsequent reads (list / get)
	// return only this half.
	Row database.StorageCredential
	// SecretKey is the plaintext S3 secret key. SENSITIVE — returned
	// to the user ONCE at mint time and then forgotten.
	SecretKey string
}

// MintCredential orchestrates a credential mint: validate -> audit
// pending -> SeaweedFS mint -> storage_credentials row insert -> event
// emit -> audit outcome. Returns the freshly-minted credential
// including the plaintext secret key (caller returns it ONCE).
func (s *Service) MintCredential(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID uuid.UUID,
	params CredentialMintParams,
) (*MintedCredential, error) {
	if s.provider == nil {
		return nil, ErrProviderDisabled
	}
	if err := validateMintParams(params); err != nil {
		return nil, err
	}
	// Look up the bucket so we have the canonical name to scope the
	// identity to. The repository layer enforces tenant scoping so a
	// cross-tenant bucket id surfaces as ErrBucketNotFound.
	bucket, err := s.repos.StorageBuckets.Get(ctx, bucketID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("storage.credential.mint: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditCredentialMint,
		ResourceType: ResourceBucket,
		ResourceID:   &bucket.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_slug": bucket.Slug,
			"label":       params.Label,
			"actions":     params.Actions,
			"has_expires": params.ExpiresAt != nil,
		},
	})

	// Expand the high-level actions into the SeaweedFS-style
	// bucket-scoped form.
	driverActions := make([]seaweedfs.IAMAction, 0, len(params.Actions))
	highLevelActions := make([]string, 0, len(params.Actions))
	for _, a := range params.Actions {
		driverActions = append(driverActions, supportedActions[a])
		highLevelActions = append(highLevelActions, string(a))
	}

	cred, err := s.provider.MintCredentials(ctx, seaweedfs.MintCredentialsParams{
		TenantID:   tenantID.String(),
		Buckets:    []string{bucket.Name},
		Actions:    driverActions,
		NamePrefix: "lah",
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return nil, fmt.Errorf("storage.credential.mint: %w", err)
	}

	row, err := s.repos.StorageCredentials.Create(ctx, database.CreateStorageCredentialParams{
		BucketID:   bucket.ID,
		UserID:     userID,
		AccessKey:  cred.AccessKey,
		SecretHash: hashSecret(cred.SecretKey),
		Label:      params.Label,
		Actions:    highLevelActions,
		ExpiresAt:  params.ExpiresAt,
	})
	if err != nil {
		// Best-effort: revoke the daemon-side identity so the access
		// key stops signing requests. A failure here is logged but
		// does not propagate — the caller already has a problem.
		_ = s.provider.RevokeCredentials(ctx, cred.AccessKey)
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return nil, fmt.Errorf("storage.credential.mint: %w", err)
	}

	// Event bus emit (s3.credential.minted).
	s.emitEvent(ctx, eventbus.S3CredentialMinted, tenantID, userID, row.ID, map[string]any{
		"bucket_id":   bucket.ID,
		"bucket_slug": bucket.Slug,
		"access_key":  cred.AccessKey,
		"actions":     highLevelActions,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"credential_id": row.ID,
		"access_key":    cred.AccessKey,
	}})

	return &MintedCredential{Row: row, SecretKey: cred.SecretKey}, nil
}

// ListCredentials returns a paginated list of the non-revoked
// credentials scoped to the bucket within the tenant in ctx.
func (s *Service) ListCredentials(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
	limit, offset int32,
) ([]database.StorageCredential, error) {
	// Verify the bucket exists + is owned by the tenant. The repository
	// layer enforces tenant scoping so a cross-tenant bucket id
	// surfaces as ErrBucketNotFound.
	if _, err := s.repos.StorageBuckets.Get(ctx, bucketID); err != nil {
		if database.IsNoRows(err) {
			return nil, ErrBucketNotFound
		}
		return nil, fmt.Errorf("storage.credential.list: %w", err)
	}
	rows, err := s.repos.StorageCredentials.ListForBucket(ctx, bucketID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("storage.credential.list: %w", err)
	}
	return rows, nil
}

// CountCredentials returns the number of non-revoked credentials scoped
// to the bucket within the tenant in ctx.
func (s *Service) CountCredentials(
	ctx context.Context,
	_ uuid.UUID,
	bucketID uuid.UUID,
) (int64, error) {
	if _, err := s.repos.StorageBuckets.Get(ctx, bucketID); err != nil {
		if database.IsNoRows(err) {
			return 0, ErrBucketNotFound
		}
		return 0, fmt.Errorf("storage.credential.count: %w", err)
	}
	return s.repos.StorageCredentials.CountForBucket(ctx, bucketID)
}

// RevokeCredential orchestrates a credential revoke: audit pending ->
// SeaweedFS revoke -> storage_credentials soft-delete -> event emit ->
// audit outcome. Idempotent: revoking an already-revoked credential is
// a no-op (the daemon treats a missing identity as success).
func (s *Service) RevokeCredential(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	bucketID, credentialID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	// Verify the bucket is owned by the tenant before touching the
	// credential. This is the tenant-scope check at the service layer;
	// the repository layer also enforces it but the explicit check
	// produces a cleaner error (ErrBucketNotFound vs ErrCredentialNotFound).
	if _, err := s.repos.StorageBuckets.Get(ctx, bucketID); err != nil {
		if database.IsNoRows(err) {
			return ErrBucketNotFound
		}
		return fmt.Errorf("storage.credential.revoke: %w", err)
	}
	row, err := s.repos.StorageCredentials.Get(ctx, credentialID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrCredentialNotFound
		}
		return fmt.Errorf("storage.credential.revoke: %w", err)
	}
	if row.BucketID != bucketID {
		// Credential exists but does not belong to the bucket in the
		// URL. Return not-found so the URL does not leak existence.
		return ErrCredentialNotFound
	}
	if row.RevokedAt != nil {
		// Already revoked; no-op.
		return nil
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditCredentialRevoke,
		ResourceType: ResourceCredential,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"bucket_id":  bucketID,
			"access_key": row.AccessKeyID,
			"label":      row.Label,
		},
	})

	// SeaweedFS revoke. Tolerate a missing identity — already revoked.
	if err := s.provider.RevokeCredentials(ctx, row.AccessKeyID); err != nil && !errors.Is(err, seaweedfs.ErrNotFound) {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.credential.revoke: %w", err)
	}

	if err := s.repos.StorageCredentials.Revoke(ctx, credentialID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("storage.credential.revoke: %w", err)
	}

	s.emitEvent(ctx, eventbus.S3CredentialRevoked, tenantID, userID, row.ID, map[string]any{
		"bucket_id":  bucketID,
		"access_key": row.AccessKeyID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// validateMintParams checks the precondition for a Mint call.
func validateMintParams(params CredentialMintParams) error {
	if len(params.Actions) == 0 {
		return fmt.Errorf("%w: at least one action is required", ErrInvalidAction)
	}
	for i, a := range params.Actions {
		if _, ok := supportedActions[a]; !ok {
			return fmt.Errorf("%w: action at index %d (%q) is not recognised", ErrInvalidAction, i, a)
		}
	}
	if len(params.Label) > MaxCredentialLabelLen {
		return fmt.Errorf("%w: label length %d exceeds max %d", ErrInvalidLabel, len(params.Label), MaxCredentialLabelLen)
	}
	if params.ExpiresAt != nil && params.ExpiresAt.Before(time.Now()) {
		return fmt.Errorf("%w: expires_at must be in the future", ErrInvalidQuota)
	}
	return nil
}
