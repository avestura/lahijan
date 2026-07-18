// Package database: webauthn_credentials_repo.go wraps the sqlc-generated
// user_webauthn_credentials queries. The table is global for the same
// reason every other credential table is global: a WebAuthn credential
// belongs to the user, not any tenant, and protects every login.
//
// Unlike the OAuth/OIDC token store and the TOTP secret store, the
// public_key column here is NOT encrypted — the W3C WebAuthn spec
// explicitly states the credential public key is not sensitive (the
// authenticator signs with the matching private key, which never leaves
// the device). The repo therefore treats public_key as a transparent
// []byte.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// WebauthnCredentialsRepository is the persistence boundary for
// user_webauthn_credentials.
type WebauthnCredentialsRepository struct {
	q *gen.Queries
}

// NewWebauthnCredentialsRepository wraps the given sqlc queries.
func NewWebauthnCredentialsRepository(q *gen.Queries) *WebauthnCredentialsRepository {
	return &WebauthnCredentialsRepository{q: q}
}

// CreateWebauthnCredentialParams carries the fields of a new credential row.
// AAGUID, SignCount, Transports, and Name all have sensible defaults;
// callers that want to omit them can pass zero values (Transports == nil
// is coerced to []string{}).
type CreateWebauthnCredentialParams struct {
	UserID       uuid.UUID
	CredentialID string
	PublicKey    []byte
	Aaguid       *uuid.UUID
	SignCount    int64
	Transports   []string
	Name         string
}

// Create inserts a credential row.
func (r *WebauthnCredentialsRepository) Create(
	ctx context.Context,
	arg CreateWebauthnCredentialParams,
) (gen.UserWebauthnCredential, error) {
	transports := arg.Transports
	if transports == nil {
		transports = []string{}
	}
	return r.q.CreateWebauthnCredential(ctx, gen.CreateWebauthnCredentialParams{
		UserID:       arg.UserID,
		CredentialID: arg.CredentialID,
		PublicKey:    arg.PublicKey,
		Aaguid:       arg.Aaguid,
		SignCount:    arg.SignCount,
		Transports:   transports,
		Name:         arg.Name,
	})
}

// Get returns the credential by its row id.
func (r *WebauthnCredentialsRepository) Get(ctx context.Context, id uuid.UUID) (gen.UserWebauthnCredential, error) {
	return r.q.GetWebauthnCredential(ctx, id)
}

// GetByUserAndID returns the credential by (user_id, credential_id) — the
// assertion path lookup. The credential_id is the WebAuthn-format id the
// browser sends.
func (r *WebauthnCredentialsRepository) GetByUserAndID(
	ctx context.Context,
	userID uuid.UUID,
	credentialID string,
) (gen.UserWebauthnCredential, error) {
	return r.q.GetWebauthnCredentialByUserAndID(ctx, gen.GetWebauthnCredentialByUserAndIDParams{
		UserID:       userID,
		CredentialID: credentialID,
	})
}

// ListForUser returns every credential the user has enrolled, newest first.
func (r *WebauthnCredentialsRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]gen.UserWebauthnCredential, error) {
	return r.q.ListWebauthnCredentialsForUser(ctx, userID)
}

// UpdateSignCount sets the new WebAuthn sign counter on the credential.
// Called by the MFA service after every successful assertion; the next
// assertion must carry a count strictly greater than this value.
func (r *WebauthnCredentialsRepository) UpdateSignCount(
	ctx context.Context,
	id, userID uuid.UUID,
	signCount int64,
) error {
	return r.q.UpdateWebauthnSignCount(ctx, gen.UpdateWebauthnSignCountParams{
		ID:        id,
		UserID:    userID,
		SignCount: signCount,
	})
}

// Delete removes a credential (revoke WebAuthn factor).
func (r *WebauthnCredentialsRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return r.q.DeleteWebauthnCredential(ctx, gen.DeleteWebauthnCredentialParams{ID: id, UserID: userID})
}

// CountForUser returns the number of WebAuthn credentials the user has
// enrolled. Used by the MFA policy check ("is this user enrolled?").
func (r *WebauthnCredentialsRepository) CountForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return r.q.CountWebauthnCredentialsForUser(ctx, userID)
}
