// Package database: tokens_repo.go wraps the sqlc-generated token queries.
// refresh_tokens and personal_access_tokens are global tables. Token issuance,
// hashing, and rotation land in WS-06; this repo only persists and reads rows.
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
type CreateRefreshTokenParams struct {
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UserAgent *string
	IPAddress *netip.Addr
}

// CreateRefreshToken inserts a refresh token row.
func (r *TokensRepository) CreateRefreshToken(ctx context.Context, arg CreateRefreshTokenParams) (gen.RefreshToken, error) {
	return r.q.CreateRefreshToken(ctx, gen.CreateRefreshTokenParams{
		UserID:    arg.UserID,
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

// RevokeAllRefreshTokensForUser revokes every still-valid refresh token for a
// user (used by "log out everywhere").
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
}

// CreatePersonalAccessToken inserts a PAT row.
func (r *TokensRepository) CreatePersonalAccessToken(
	ctx context.Context,
	arg CreatePersonalAccessTokenParams,
) (gen.PersonalAccessToken, error) {
	return r.q.CreatePersonalAccessToken(ctx, gen.CreatePersonalAccessTokenParams{
		UserID:    arg.UserID,
		Name:      arg.Name,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
	})
}

// GetPersonalAccessTokenByHash returns the PAT with the given hash.
func (r *TokensRepository) GetPersonalAccessTokenByHash(
	ctx context.Context,
	tokenHash string,
) (gen.PersonalAccessToken, error) {
	return r.q.GetPersonalAccessTokenByHash(ctx, tokenHash)
}

// TouchPersonalAccessToken updates the last-used timestamp of a PAT.
func (r *TokensRepository) TouchPersonalAccessToken(ctx context.Context, tokenHash string) error {
	return r.q.TouchPersonalAccessToken(ctx, tokenHash)
}

// RevokePersonalAccessToken marks a PAT revoked (idempotent).
func (r *TokensRepository) RevokePersonalAccessToken(ctx context.Context, tokenHash string) error {
	return r.q.RevokePersonalAccessToken(ctx, tokenHash)
}
