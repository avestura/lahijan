// Package database: mfa_pending_sessions_repo.go wraps the sqlc-generated
// mfa_pending_sessions queries. The table is global: a pending MFA session
// pre-dates any tenant selection — the user is mid-login and the
// membership context has not been chosen yet.
//
// The token_hash column carries the SHA-256 hex digest of the raw pending
// token (the raw token is produced by auth/secrets.Signer.Issue and lives
// in the browser's short-term storage until the MFA challenge completes).
package database

import (
	"context"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// MFAPendingSessionsRepository is the persistence boundary for
// mfa_pending_sessions.
type MFAPendingSessionsRepository struct {
	q *gen.Queries
}

// NewMFAPendingSessionsRepository wraps the given sqlc queries.
func NewMFAPendingSessionsRepository(q *gen.Queries) *MFAPendingSessionsRepository {
	return &MFAPendingSessionsRepository{q: q}
}

// CreateMFAPendingSessionParams carries the fields of a new pending row.
type CreateMFAPendingSessionParams struct {
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UserAgent *string
	IPAddress *netip.Addr
}

// Create inserts a new pending session row. The caller MUST pass the SHA-256
// hash of the raw token (never the raw token itself).
func (r *MFAPendingSessionsRepository) Create(
	ctx context.Context,
	arg CreateMFAPendingSessionParams,
) (gen.MfaPendingSession, error) {
	return r.q.CreateMFAPendingSession(ctx, gen.CreateMFAPendingSessionParams{
		UserID:    arg.UserID,
		TokenHash: arg.TokenHash,
		ExpiresAt: arg.ExpiresAt,
		UserAgent: arg.UserAgent,
		IpAddress: arg.IPAddress,
	})
}

// GetByHash returns the pending session row for the given token hash. The
// caller checks ExpiresAt, ConsumedAt, and RevokedAt to decide whether the
// row is still actionable.
func (r *MFAPendingSessionsRepository) GetByHash(
	ctx context.Context,
	tokenHash string,
) (gen.MfaPendingSession, error) {
	return r.q.GetMFAPendingSessionByHash(ctx, tokenHash)
}

// Consume marks the pending session as consumed by a successful MFA
// challenge (single-use). Idempotent: a no-op on an already-consumed row.
// The caller treats a second consumption attempt as invalid.
func (r *MFAPendingSessionsRepository) Consume(ctx context.Context, id uuid.UUID) error {
	return r.q.ConsumeMFAPendingSession(ctx, id)
}

// Revoke marks the pending session as revoked (login cancelled, brute-force
// lockout, or admin force-logout). Subsequent challenges are rejected.
func (r *MFAPendingSessionsRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	return r.q.RevokeMFAPendingSession(ctx, id)
}

// IncFailures atomically bumps the failed_attempts counter. The caller
// fetches the row, checks if the new count crosses the threshold, and
// calls Revoke to lock the user out.
func (r *MFAPendingSessionsRepository) IncFailures(ctx context.Context, id uuid.UUID) error {
	return r.q.IncMFAPendingSessionFailures(ctx, id)
}

// DeleteAllForUser wipes every pending row for the user. Called by the
// MFA service when the user re-enrolls or fully disables MFA: any pending
// challenge against the old factors must stop working immediately.
func (r *MFAPendingSessionsRepository) DeleteAllForUser(ctx context.Context, userID uuid.UUID) error {
	return r.q.DeleteMFAPendingSessionsForUser(ctx, userID)
}
