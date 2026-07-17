// Package database: sessions_repo.go wraps the sqlc-generated session queries.
// The sessions table is global: a user's login sessions are not tied to a
// tenant. Session issuance, cookie signing, and expiry live in auth/session
// (WS-06); this repo only persists and reads rows.
package database

import (
	"context"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// SessionsRepository is the persistence boundary for the sessions table.
type SessionsRepository struct {
	q *gen.Queries
}

// NewSessionsRepository wraps the given sqlc queries.
func NewSessionsRepository(q *gen.Queries) *SessionsRepository {
	return &SessionsRepository{q: q}
}

// CreateSessionParams carries the fields of a new session. The caller supplies
// the cookie-secret HASH only; the raw secret is never stored.
type CreateSessionParams struct {
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UserAgent *string
	IPAddress *netip.Addr
}

// Create inserts a session row.
func (r *SessionsRepository) Create(ctx context.Context, arg CreateSessionParams) (gen.Session, error) {
	return r.q.CreateSession(ctx, gen.CreateSessionParams{
		UserID:    arg.UserID,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		UserAgent: arg.UserAgent,
		IpAddress: arg.IPAddress,
	})
}

// GetByTokenHash returns the session whose cookie-secret hash matches.
func (r *SessionsRepository) GetByTokenHash(ctx context.Context, tokenHash string) (gen.Session, error) {
	return r.q.GetSessionByTokenHash(ctx, tokenHash)
}

// Get returns the session with the given id.
func (r *SessionsRepository) Get(ctx context.Context, id uuid.UUID) (gen.Session, error) {
	return r.q.GetSession(ctx, id)
}

// Touch updates the session's last_seen_at (called on each authenticated request).
func (r *SessionsRepository) Touch(ctx context.Context, id uuid.UUID) error {
	return r.q.TouchSession(ctx, id)
}

// Revoke marks a session revoked (logout). Idempotent.
func (r *SessionsRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	return r.q.RevokeSession(ctx, id)
}

// RevokeAllForUser revokes every still-valid session for a user.
func (r *SessionsRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	return r.q.RevokeAllSessionsForUser(ctx, userID)
}

// ListForUser returns the user's non-revoked sessions, newest first.
func (r *SessionsRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]gen.Session, error) {
	return r.q.ListSessionsForUser(ctx, userID)
}
