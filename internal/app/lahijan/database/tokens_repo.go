// Package database: tokens_repo.go wraps the sqlc-generated token queries.
// refresh_tokens and personal_access_tokens are global tables. Token issuance,
// hashing, and rotation live in auth/session and auth/pat (WS-06); this repo
// only persists and reads rows.
//
// refresh_tokens belong to a session (session_id) and a rotation family
// (family_id). Reusing a rotated token revokes the whole family — reuse
// detection is orchestrated by auth/session, which calls RevokeRefreshTokenFamily.
package database

import (
	"context"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// TokensRepository is the persistence boundary for refresh_tokens and
// personal_access_tokens.
type TokensRepository struct {
	q *gen.Queries
}

// NewTokensRepository wraps the given sqlc queries.
func NewTokensRepository(q *gen.Queries) *TokensRepository {
	return &TokensRepository{q: q}
}

// CreateRefreshTokenParams carries the fields of a new refresh token. The
// caller supplies the token HASH only; the raw token is never stored.
// SessionID and FamilyID are required: every refresh token belongs to a
// session and a rotation family.
type CreateRefreshTokenParams struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	FamilyID  uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UserAgent *string
	IPAddress *netip.Addr
}

// CreateRefreshToken inserts a refresh token row.
func (r *TokensRepository) CreateRefreshToken(ctx context.Context, arg CreateRefreshTokenParams) (gen.RefreshToken, error) {
	return r.q.CreateRefreshToken(ctx, gen.CreateRefreshTokenParams{
		UserID:    arg.UserID,
		SessionID: arg.SessionID,
		FamilyID:  arg.FamilyID,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		UserAgent: arg.UserAgent,
		IpAddress: arg.IPAddress,
	})
}

// GetRefreshTokenByHash returns the refresh token with the given hash.
func (r *TokensRepository) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (gen.RefreshToken, error) {
	return r.q.GetRefreshTokenByHash(ctx, tokenHash)
}

// RevokeRefreshToken marks a refresh token revoked (idempotent).
func (r *TokensRepository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	return r.q.RevokeRefreshToken(ctx, tokenHash)
}

// RevokeRefreshTokenFamily revokes every still-valid refresh token in a family.
// Used for reuse detection: a reused token means the family is compromised.
func (r *TokensRepository) RevokeRefreshTokenFamily(ctx context.Context, familyID uuid.UUID) error {
	return r.q.RevokeRefreshTokenFamily(ctx, familyID)
}

// RevokeRefreshTokensForSession revokes every still-valid refresh token for a
// session (logout).
func (r *TokensRepository) RevokeRefreshTokensForSession(ctx context.Context, sessionID uuid.UUID) error {
	return r.q.RevokeRefreshTokensForSession(ctx, sessionID)
}

// RevokeAllRefreshTokensForUser revokes every still-valid refresh token for a
// user ("log out everywhere").
func (r *TokensRepository) RevokeAllRefreshTokensForUser(ctx context.Context, userID uuid.UUID) error {
	return r.q.RevokeAllRefreshTokensForUser(ctx, userID)
}

// CreatePersonalAccessTokenParams carries the fields of a new PAT. The caller
// supplies the token HASH only.
type CreatePersonalAccessTokenParams struct {
	UserID    uuid.UUID
	Name      string
	TokenHash string
	ExpiresAt *time.Time
	Scopes    []string
}

// CreatePersonalAccessToken inserts a PAT row.
func (r *TokensRepository) CreatePersonalAccessToken(
	ctx context.Context,
	arg CreatePersonalAccessTokenParams,
) (gen.PersonalAccessToken, error) {
	scopes := arg.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return r.q.CreatePersonalAccessToken(ctx, gen.CreatePersonalAccessTokenParams{
		UserID:    arg.UserID,
		Name:      arg.Name,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		Scopes:    scopes,
	})
}

// GetPersonalAccessTokenByHash returns the PAT with the given hash.
func (r *TokensRepository) GetPersonalAccessTokenByHash(
	ctx context.Context,
	tokenHash string,
) (gen.PersonalAccessToken, error) {
	return r.q.GetPersonalAccessTokenByHash(ctx, tokenHash)
}

// GetPersonalAccessTokenByID returns the PAT with the given id.
func (r *TokensRepository) GetPersonalAccessTokenByID(
	ctx context.Context,
	id uuid.UUID,
) (gen.PersonalAccessToken, error) {
	return r.q.GetPersonalAccessTokenByID(ctx, id)
}

// ListPersonalAccessTokensForUser returns the user's non-revoked PATs, newest first.
func (r *TokensRepository) ListPersonalAccessTokensForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]gen.PersonalAccessToken, error) {
	return r.q.ListPersonalAccessTokensForUser(ctx, userID)
}

// TouchPersonalAccessToken updates the last-used timestamp of a PAT.
func (r *TokensRepository) TouchPersonalAccessToken(ctx context.Context, tokenHash string) error {
	return r.q.TouchPersonalAccessToken(ctx, tokenHash)
}

// RevokePersonalAccessToken marks a PAT revoked (idempotent).
func (r *TokensRepository) RevokePersonalAccessToken(ctx context.Context, tokenHash string) error {
	return r.q.RevokePersonalAccessToken(ctx, tokenHash)
}

// RevokePersonalAccessTokenByID marks a PAT revoked by id, scoped to userID so
// a user cannot revoke another user's PAT.
func (r *TokensRepository) RevokePersonalAccessTokenByID(
	ctx context.Context,
	id, userID uuid.UUID,
) error {
	return r.q.RevokePersonalAccessTokenByID(ctx, gen.RevokePersonalAccessTokenByIDParams{ID: id, UserID: userID})
}
