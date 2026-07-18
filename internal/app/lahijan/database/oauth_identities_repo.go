// Package database: oauth_identities_repo.go wraps the sqlc-generated
// user_oauth_identities queries. The table is global (no tenant_id) because
// an external identity belongs to a user, not a tenant — login is platform-
// wide and a user can be a member of many tenants (ADR-0002, ADR-0004).
//
// Token columns (access_token, refresh_token) hold AES-GCM ciphertext produced
// by auth/secrets.Crypto; this repo never inspects or decodes them. The IdP
// service is responsible for encrypting before Create/Update and decrypting
// after Get/List.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// OAuthIdentitiesRepository is the persistence boundary for
// user_oauth_identities.
type OAuthIdentitiesRepository struct {
	q *gen.Queries
}

// NewOAuthIdentitiesRepository wraps the given sqlc queries.
func NewOAuthIdentitiesRepository(q *gen.Queries) *OAuthIdentitiesRepository {
	return &OAuthIdentitiesRepository{q: q}
}

// CreateOAuthIdentityParams carries the fields of a new external identity link.
// AccessToken, RefreshToken, and ExpiresAt are all optional: an IdP may issue
// only an access_token (GitHub legacy flow), only an id_token (OIDC pure),
// or non-expiring credentials. The caller MUST pass already-encrypted token
// ciphertext — never raw tokens.
type CreateOAuthIdentityParams struct {
	UserID       uuid.UUID
	Provider     string
	Subject      string
	AccessToken  *string
	RefreshToken *string
	Scopes       []string
	ExpiresAt    *time.Time
}

// Create inserts a new external identity link. A duplicate (provider, subject)
// or (user_id, provider) raises a Postgres unique-violation the caller maps to
// a "this identity is already linked" error.
func (r *OAuthIdentitiesRepository) Create(
	ctx context.Context,
	arg CreateOAuthIdentityParams,
) (gen.UserOauthIdentity, error) {
	scopes := arg.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return r.q.CreateOAuthIdentity(ctx, gen.CreateOAuthIdentityParams{
		UserID:       arg.UserID,
		Provider:     arg.Provider,
		Subject:      arg.Subject,
		AccessToken:  arg.AccessToken,
		RefreshToken: arg.RefreshToken,
		Scopes:       scopes,
		ExpiresAt:    arg.ExpiresAt,
	})
}

// Get returns the identity row by id.
func (r *OAuthIdentitiesRepository) Get(ctx context.Context, id uuid.UUID) (gen.UserOauthIdentity, error) {
	return r.q.GetOAuthIdentity(ctx, id)
}

// GetByProviderSubject returns the identity row for the given (provider, subject)
// pair — the callback path lookup after the IdP returns the user's profile.
func (r *OAuthIdentitiesRepository) GetByProviderSubject(
	ctx context.Context,
	provider, subject string,
) (gen.UserOauthIdentity, error) {
	return r.q.GetOAuthIdentityByProviderSubject(ctx, gen.GetOAuthIdentityByProviderSubjectParams{
		Provider: provider,
		Subject:  subject,
	})
}

// GetForUser returns the identity row for the given (userID, provider) pair —
// the link/unlink path lookup.
func (r *OAuthIdentitiesRepository) GetForUser(
	ctx context.Context,
	userID uuid.UUID,
	provider string,
) (gen.UserOauthIdentity, error) {
	return r.q.GetOAuthIdentityForUser(ctx, gen.GetOAuthIdentityForUserParams{
		UserID:   userID,
		Provider: provider,
	})
}

// ListForUser returns every external identity linked to the user, newest first.
func (r *OAuthIdentitiesRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]gen.UserOauthIdentity, error) {
	return r.q.ListOAuthIdentitiesForUser(ctx, userID)
}

// UpdateOAuthIdentityTokensParams rotates the stored tokens + scopes + expiry
// on the given identity row. As with Create, the caller MUST pass already-
// encrypted token ciphertext.
type UpdateOAuthIdentityTokensParams struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	AccessToken  *string
	RefreshToken *string
	Scopes       []string
	ExpiresAt    *time.Time
}

// UpdateTokens rotates the stored tokens (and scopes + expiry).
func (r *OAuthIdentitiesRepository) UpdateTokens(
	ctx context.Context,
	arg UpdateOAuthIdentityTokensParams,
) error {
	scopes := arg.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return r.q.UpdateOAuthIdentityTokens(ctx, gen.UpdateOAuthIdentityTokensParams{
		ID:           arg.ID,
		UserID:       arg.UserID,
		AccessToken:  arg.AccessToken,
		RefreshToken: arg.RefreshToken,
		Scopes:       scopes,
		ExpiresAt:    arg.ExpiresAt,
	})
}

// Delete removes the identity row. The "at least one auth method remaining"
// invariant is enforced in the service layer (which counts password_hash +
// other identities + SAML links first); this repo just deletes.
func (r *OAuthIdentitiesRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return r.q.DeleteOAuthIdentity(ctx, gen.DeleteOAuthIdentityParams{ID: id, UserID: userID})
}

// CountForUser returns the number of OAuth/OIDC identities linked to the user.
// Used by the unlink path to enforce "at least one auth method remaining".
func (r *OAuthIdentitiesRepository) CountForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return r.q.CountOAuthIdentitiesForUser(ctx, userID)
}
