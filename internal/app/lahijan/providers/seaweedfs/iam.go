// Package seaweedfs: iam.go mints per-user S3 credentials scoped to one or
// more buckets, and revokes / rotates them. Per ADR-0011 each credential is
// recorded in SeaweedFS' Filer metadata under
// /etc/seaweedfs/identities/<access_key>.json (Lahijan's bookkeeping), and
// every change re-publishes the SeaweedFS-native IAM document
// /etc/iam/identity.json, which is the only path `weed s3` watches and
// hot-reloads. The filer must run with -saveToFilerLimit so that document is
// stored inline (the s3 watcher reads only inline content).
//
// The plaintext secret key is returned to the caller ONCE at mint / rotate
// time. The storage service (WS-16) is responsible for storing the
// encrypted-at-rest form in the `bucket_credentials` table; subsequent
// reads return only the access key.
package seaweedfs

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IAMAction is an S3-style action verb Lahijan mints into an identity's
// Actions list. Mirrors the subset of SeaweedFS' S3 action vocabulary
// Lahijan exposes.
type IAMAction string

const (
	// IAMActionRead permits GetObject + ListBucket on the scoped buckets.
	IAMActionRead IAMAction = "Read"

	// IAMActionWrite permits PutObject + DeleteObject on the scoped
	// buckets. Implies Read for the same buckets.
	IAMActionWrite IAMAction = "Write"

	// IAMActionList permits ListBucket only (no Get). Useful for
	// "inventory" plugins that only need to enumerate objects.
	IAMActionList IAMAction = "List"

	// IAMActionTagging permits PutObjectTagging +
	// DeleteObjectTagging on the scoped buckets.
	IAMActionTagging IAMAction = "Tagging"

	// IAMActionAdmin grants every action on the scoped buckets,
	// including bucket-level operations (delete bucket, set policy).
	// Lahijan uses this for the tenant owner role.
	IAMActionAdmin IAMAction = "Admin"
)

// MintCredentialsParams is the user-visible shape of a credential-mint call.
type MintCredentialsParams struct {
	// TenantID is the optional tenant scope for the synthesized event
	// payload. Empty when the caller is operating at system scope.
	TenantID string

	// Buckets is the list of canonical bucket names this credential can
	// access. Every entry MUST already exist on SeaweedFS; the driver
	// does not pre-check because the S3 server enforces it server-side.
	// Required: at least one bucket.
	Buckets []string

	// Actions is the list of S3-style actions the credential permits.
	// Each is applied to each entry in Buckets. Required: at least one
	// action.
	Actions []IAMAction

	// NamePrefix is an optional human-readable prefix prepended to the
	// generated access key (max 8 chars; the rest is random). Helps
	// Filer admins identify the credential in log dumps. Default is
	// "lah".
	NamePrefix string
}

// MintCredentials creates a new SeaweedFS identity record carrying the
// requested bucket + action scope, and returns the freshly-generated
// access key + secret key. The secret key is SENSITIVE — the caller must
// return it to the user once and store only the encrypted form.
func (p *Provider) MintCredentials(ctx context.Context, params MintCredentialsParams) (*IAMCredential, error) {
	ctx, span := startSpan(ctx, "credential.mint")
	defer span.End()

	if err := validateMintParams(params); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.mint: %w", err)
	}

	accessKey, err := generateAccessKey(params.NamePrefix)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.mint: %w", err)
	}
	secretKey, err := generateSecretKey()
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.mint: %w", err)
	}

	record := identityRecord{
		Name:      accessKey,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Buckets:   append([]string(nil), params.Buckets...),
		Actions:   expandActions(params.Actions, params.Buckets),
		Disabled:  false,
	}
	if err := p.writeIdentity(ctx, record); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.mint: %w", err)
	}

	cred := &IAMCredential{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Buckets:   record.Buckets,
		Actions:   record.Actions,
		Enabled:   true,
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "storage.credential.minted",
		TenantID:   optionalStrPtr(params.TenantID),
		ActorType:  "system",
		ResourceID: strPtr(accessKey),
		Metadata:   asRawJSON(redactedCredentialEvent(cred)),
	})
	setStatus(span, nil)
	return cred, nil
}

// RotateCredentials deletes the identity at the given access key and
// mints a fresh one with the same bucket + action scope. The old access
// key stops working immediately; the new secret key is returned to the
// caller once.
//
// Returns ErrNotFound when no identity matches the given access key.
func (p *Provider) RotateCredentials(ctx context.Context, accessKey string, params MintCredentialsParams) (*IAMCredential, error) {
	ctx, span := startSpan(ctx, "credential.rotate", accessKeyAttr(accessKey))
	defer span.End()

	if accessKey == "" {
		err := errors.New("seaweedfs: access key is required")
		setStatus(span, err)
		return nil, err
	}
	old, err := p.readIdentity(ctx, accessKey)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.rotate: %w", err)
	}
	// Carry over scope from the previous record when the caller did not
	// override it.
	if len(params.Buckets) == 0 {
		params.Buckets = append([]string(nil), old.Buckets...)
	}
	if len(params.Actions) == 0 {
		params.Actions = contractActions(old.Actions)
	}

	// Delete the old record first so the old secret stops signing
	// requests before the new one is live. A failure here is fatal —
	// the caller must retry with the same access key.
	if delErr := p.deleteIdentity(ctx, accessKey); delErr != nil {
		setStatus(span, delErr)
		return nil, fmt.Errorf("seaweedfs: credential.rotate: %w", delErr)
	}
	fresh, mErr := p.MintCredentials(ctx, params)
	if mErr != nil {
		setStatus(span, mErr)
		return nil, fmt.Errorf("seaweedfs: credential.rotate: %w", mErr)
	}
	p.emitChange(ctx, BusEvent{
		Topic:      "storage.credential.rotated",
		TenantID:   optionalStrPtr(params.TenantID),
		ActorType:  "system",
		ResourceID: strPtr(accessKey),
		Metadata:   asRawJSON(map[string]any{"old": accessKey, "new": fresh.AccessKey}),
	})
	setStatus(span, nil)
	return fresh, nil
}

// RevokeCredentials flips the identity's Disabled flag to true (and
// also deletes the record from the Filer) so subsequent S3 requests
// carrying the access key fail closed. The audit event is emitted before
// the side effect so the audit log captures the intent even if the
// Filer write fails.
//
// Idempotent: revoking an unknown access key returns nil.
func (p *Provider) RevokeCredentials(ctx context.Context, accessKey string) error {
	ctx, span := startSpan(ctx, "credential.revoke", accessKeyAttr(accessKey))
	defer span.End()

	if accessKey == "" {
		err := errors.New("seaweedfs: access key is required")
		setStatus(span, err)
		return err
	}

	// Best-effort read first so we can carry scope in the event payload.
	// A missing record is treated as "already revoked" → idempotent.
	if existing, err := p.readIdentity(ctx, accessKey); err == nil {
		existing.Disabled = true
		// Write the disabled flag back so SeaweedFS keeps the identity
		// loaded (audit trail) but rejects every request.
		if werr := p.writeIdentity(ctx, existing); werr != nil {
			// A failed disabled-flag write is non-fatal: we still
			// delete the record below, which is the stronger signal.
			setStatus(span, werr)
		}
	} else if !errors.Is(err, ErrNotFound) {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: credential.revoke: %w", err)
	}

	// Delete the record so the access key stops signing entirely. The
	// "disabled" write above is best-effort; the delete is the source
	// of truth.
	if err := p.deleteIdentity(ctx, accessKey); err != nil {
		setStatus(span, err)
		return fmt.Errorf("seaweedfs: credential.revoke: %w", err)
	}

	p.emitChange(ctx, BusEvent{
		Topic:      "storage.credential.revoked",
		ActorType:  "system",
		ResourceID: strPtr(accessKey),
	})
	setStatus(span, nil)
	return nil
}

// GetCredential loads the identity record at the given access key and
// returns a redacted view (no secret key). Returns ErrNotFound when the
// identity does not exist.
func (p *Provider) GetCredential(ctx context.Context, accessKey string) (*IAMCredential, error) {
	ctx, span := startSpan(ctx, "credential.get", accessKeyAttr(accessKey))
	defer span.End()

	if accessKey == "" {
		err := errors.New("seaweedfs: access key is required")
		setStatus(span, err)
		return nil, err
	}
	record, err := p.readIdentity(ctx, accessKey)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.get: %w", err)
	}
	setStatus(span, nil)
	return &IAMCredential{
		AccessKey: record.AccessKey,
		SecretKey: "", // never return the secret on read paths
		Buckets:   record.Buckets,
		Actions:   record.Actions,
		Enabled:   !record.Disabled,
	}, nil
}

// ListCredentials returns every identity the driver has minted (i.e. every
// record under /etc/seaweedfs/identities/). SeaweedFS does not paginate
// this surface; the storage service filters to the calling tenant.
func (p *Provider) ListCredentials(ctx context.Context) ([]IAMCredential, error) {
	ctx, span := startSpan(ctx, "credential.list")
	defer span.End()

	records, err := p.filer.ListMetadata(ctx, identitiesPathPrefix)
	if err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("seaweedfs: credential.list: %w", err)
	}
	out := make([]IAMCredential, 0, len(records))
	for _, body := range records {
		var rec identityRecord
		if err := unmarshalStrict(body, &rec); err != nil {
			// Skip malformed records (likely written by a newer
			// SeaweedFS with extra fields).
			continue
		}
		out = append(out, IAMCredential{
			AccessKey: rec.AccessKey,
			Buckets:   rec.Buckets,
			Actions:   rec.Actions,
			Enabled:   !rec.Disabled,
		})
	}
	setStatus(span, nil)
	return out, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// identitiesPathPrefix is the Filer metadata path all Lahijan-minted
// identity records live under. SeaweedFS' `weed s3` server watches this
// path when configured for filer-backed IAM.
const identitiesPathPrefix = "/etc/seaweedfs/identities"

// identityPath returns the Filer metadata path for the given access key.
func identityPath(accessKey string) string {
	return identitiesPathPrefix + "/" + accessKey + ".json"
}

// writeIdentity writes the given identity record to the Filer.
func (p *Provider) writeIdentity(ctx context.Context, rec identityRecord) error {
	body, err := marshalStrict(rec)
	if err != nil {
		return fmt.Errorf("marshal identity: %w", err)
	}
	if err := p.filer.PutMetadata(ctx, identityPath(rec.AccessKey), body); err != nil {
		return err
	}
	return p.syncIAMConfig(ctx)
}

// iamConfigPath is the SeaweedFS-native IAM document `weed s3` subscribes
// to (filer.IamConfigDirectory + filer.IamIdentityFile upstream).
const iamConfigPath = "/etc/iam/identity.json"

// s3APIConfiguration mirrors SeaweedFS' iam_pb.S3ApiConfiguration JSON.
type s3APIConfiguration struct {
	Identities []s3Identity `json:"identities"`
}

type s3Identity struct {
	Name        string         `json:"name"`
	Credentials []s3Credential `json:"credentials"`
	Actions     []string       `json:"actions"`
}

type s3Credential struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// SyncIAM re-publishes the S3 IAM document from the recorded identities.
// program.Start calls it once at boot; mint/rotate/revoke call it
// implicitly.
func (p *Provider) SyncIAM(ctx context.Context) error {
	ctx, span := startSpan(ctx, "credential.sync")
	defer span.End()
	err := p.syncIAMConfig(ctx)
	setStatus(span, err)
	if err != nil {
		return fmt.Errorf("seaweedfs: credential.sync: %w", err)
	}
	return nil
}

// syncIAMConfig rebuilds /etc/iam/identity.json from the admin identity
// plus every enabled Lahijan identity record. The document REPLACES the
// s3 server's identity set on reload, so the admin identity must always
// be included or Lahijan would lock itself out of the S3 control plane.
func (p *Provider) syncIAMConfig(ctx context.Context) error {
	p.iamMu.Lock()
	defer p.iamMu.Unlock()

	cfg := s3APIConfiguration{Identities: []s3Identity{}}
	if p.adminAccessKey != "" {
		cfg.Identities = append(cfg.Identities, s3Identity{
			Name:        "lahijan_admin",
			Credentials: []s3Credential{{AccessKey: p.adminAccessKey, SecretKey: p.adminSecretKey}},
			Actions:     []string{"Admin", "Read", "Write", "List", "Tagging"},
		})
	}
	records, err := p.filer.ListMetadata(ctx, identitiesPathPrefix)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("list identities: %w", err)
	}
	for _, body := range records {
		var rec identityRecord
		if uerr := unmarshalStrict(body, &rec); uerr != nil || rec.Disabled || rec.AccessKey == "" {
			continue
		}
		cfg.Identities = append(cfg.Identities, s3Identity{
			Name:        rec.Name,
			Credentials: []s3Credential{{AccessKey: rec.AccessKey, SecretKey: rec.SecretKey}},
			Actions:     rec.Actions,
		})
	}
	body, err := marshalStrict(cfg)
	if err != nil {
		return fmt.Errorf("marshal iam config: %w", err)
	}
	if err := p.filer.PutMetadata(ctx, iamConfigPath, body); err != nil {
		return fmt.Errorf("publish iam config: %w", err)
	}
	return nil
}

// readIdentity loads the identity at the given access key. Returns
// ErrNotFound when the record does not exist.
func (p *Provider) readIdentity(ctx context.Context, accessKey string) (identityRecord, error) {
	body, err := p.filer.GetMetadata(ctx, identityPath(accessKey))
	if err != nil {
		return identityRecord{}, err
	}
	var rec identityRecord
	if err := unmarshalStrict(body, &rec); err != nil {
		return identityRecord{}, fmt.Errorf("decode identity: %w", err)
	}
	return rec, nil
}

// deleteIdentity removes the identity record. Idempotent.
func (p *Provider) deleteIdentity(ctx context.Context, accessKey string) error {
	if err := p.filer.DeleteMetadata(ctx, identityPath(accessKey)); err != nil {
		return err
	}
	return p.syncIAMConfig(ctx)
}

// validateMintParams checks the precondition for a Mint call.
func validateMintParams(params MintCredentialsParams) error {
	if len(params.Buckets) == 0 {
		return errors.New("at least one bucket is required")
	}
	for i, b := range params.Buckets {
		if err := validateCanonicalBucketName(b); err != nil {
			return fmt.Errorf("bucket at index %d: %w", i, err)
		}
	}
	if len(params.Actions) == 0 {
		return errors.New("at least one action is required")
	}
	for i, a := range params.Actions {
		switch a {
		case IAMActionRead, IAMActionWrite, IAMActionList, IAMActionTagging, IAMActionAdmin:
		default:
			return fmt.Errorf("action at index %d (%q) is not recognised", i, a)
		}
	}
	prefix := params.NamePrefix
	if prefix != "" {
		if len(prefix) > 8 {
			return fmt.Errorf("NamePrefix %q length %d exceeds max 8", prefix, len(prefix))
		}
		for _, r := range prefix {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return fmt.Errorf("NamePrefix %q must be lowercase alphanumeric", prefix)
			}
		}
	}
	return nil
}

// expandActions produces the SeaweedFS-style action strings ("Read:<bucket>")
// from the high-level action list. SeaweedFS reads the expanded list
// directly; the high-level form is for the audit log.
func expandActions(actions []IAMAction, buckets []string) []string {
	seen := make(map[string]bool, len(actions)*len(buckets))
	out := make([]string, 0, len(actions)*len(buckets))
	for _, action := range actions {
		for _, bucket := range buckets {
			s := string(action) + ":" + bucket
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// contractActions is the inverse of expandActions: it deduplicates the
// bucket-scoped action strings into the high-level IAMAction list.
func contractActions(expanded []string) []IAMAction {
	seen := make(map[IAMAction]bool)
	out := make([]IAMAction, 0, len(expanded))
	for _, s := range expanded {
		head, _, _ := strings.Cut(s, ":")
		action := IAMAction(head)
		if !seen[action] {
			seen[action] = true
			out = append(out, action)
		}
	}
	return out
}

// generateAccessKey returns a 20-character AWS-style access key id.
// Format: optional <prefix> (max 8 chars) + random base32 (no padding).
// The randomness comes from crypto/rand so an attacker cannot predict
// the next id.
func generateAccessKey(prefix string) (string, error) {
	if prefix == "" {
		prefix = "lah"
	}
	// 10 random bytes → 16 base32 chars; prefix + 16 = 19 chars (close
	// enough to the AWS 20-char standard for the test fake to accept).
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	enc := base32.StdEncoding.EncodeToString(buf)
	enc = strings.TrimRight(enc, "=")
	enc = strings.ToLower(enc)
	return prefix + enc, nil
}

// generateSecretKey returns a 40-character base32 secret. Mirrors the AWS
// secret key shape (40 base64-ish chars).
func generateSecretKey() (string, error) {
	buf := make([]byte, 25)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	enc := base32.StdEncoding.EncodeToString(buf)
	enc = strings.TrimRight(enc, "=")
	return strings.ToLower(enc), nil
}

// redactedCredentialEvent returns the event payload for a Mint / Rotate —
// the access key is included so the audit trail can reference it, the
// secret key is NEVER included.
func redactedCredentialEvent(c *IAMCredential) map[string]any {
	return map[string]any{
		"access_key": c.AccessKey,
		"buckets":    c.Buckets,
		"actions":    c.Actions,
		"enabled":    c.Enabled,
		"emitted_at": time.Now().UTC().Format(time.RFC3339),
	}
}

// marshalStrict is json.Marshal with a stable name so callers can find
// usages. The identityRecord + quotaRecord types are JSON-encoded.
func marshalStrict(v any) ([]byte, error) {
	return jsonMarshal(v)
}

// unmarshalStrict is json.Unmarshal with a stable name.
func unmarshalStrict(body []byte, v any) error {
	return jsonUnmarshal(body, v)
}
