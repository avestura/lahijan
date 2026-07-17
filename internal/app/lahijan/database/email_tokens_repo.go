// Package database: email_tokens_repo.go wraps the sqlc-generated email_token
// queries. email_tokens are single-use, expiring tokens for email verification,
// password reset, and email change (WS-06). The raw token travels in a signed
// link emailed to the user; only the SHA-256 hash is stored here.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// EmailTokenKind enumerates the kinds of single-use email tokens Lahijan issues.
const (
	EmailTokenVerifyEmail   = "verify_email"
	EmailTokenPasswordReset = "password_reset"
	EmailTokenEmailChange   = "email_change"
)

// EmailTokensRepository is the persistence boundary for the email_tokens table.
type EmailTokensRepository struct {
	q *gen.Queries
}

// NewEmailTokensRepository wraps the given sqlc queries.
func NewEmailTokensRepository(q *gen.Queries) *EmailTokensRepository {
	return &EmailTokensRepository{q: q}
}

// CreateEmailTokenParams carries the fields of a new email token. NewEmail is
// only set for the email_change kind. The caller supplies the token HASH only.
type CreateEmailTokenParams struct {
	UserID    uuid.UUID
	TokenHash string
	Kind      string
	NewEmail  *string
	ExpiresAt time.Time
}

// Create inserts an email token row.
func (r *EmailTokensRepository) Create(ctx context.Context, arg CreateEmailTokenParams) (gen.EmailToken, error) {
	return r.q.CreateEmailToken(ctx, gen.CreateEmailTokenParams{
		UserID:    arg.UserID,
		TokenHash: arg.TokenHash,
		Kind:      arg.Kind,
		NewEmail:  arg.NewEmail,
		ExpiresAt: arg.ExpiresAt,
	})
}

// GetByHash returns the email token with the given hash.
func (r *EmailTokensRepository) GetByHash(ctx context.Context, tokenHash string) (gen.EmailToken, error) {
	return r.q.GetEmailTokenByHash(ctx, tokenHash)
}

// Consume marks an email token used (single-use). Returns whether the row was
// actually updated: false means the token was already used or did not exist,
// which the caller treats as invalid.
func (r *EmailTokensRepository) Consume(ctx context.Context, tokenHash string) (bool, error) {
	n, err := r.q.ConsumeEmailToken(ctx, tokenHash)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// RevokeForUser invalidates every outstanding email token of a kind for a user
// (e.g. when re-issuing a verification token, revoke the previous one).
func (r *EmailTokensRepository) RevokeForUser(ctx context.Context, userID uuid.UUID, kind string) error {
	return r.q.RevokeEmailTokensForUser(ctx, gen.RevokeEmailTokensForUserParams{UserID: userID, Kind: kind})
}
