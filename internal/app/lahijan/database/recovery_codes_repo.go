// Package database: recovery_codes_repo.go wraps the sqlc-generated
// user_recovery_codes queries. The table is global; a recovery code is a
// fallback authentication credential that lives on the user, not any
// tenant.
//
// The code_hash column carries the SHA-256 hex digest of the raw recovery
// code; this repo never sees the raw code. The MFA service generates the
// raw codes, hands them to the caller once, and stores only the digests
// here.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// UserRecoveryCodeRow is the package-level alias for the sqlc-generated
// type so callers do not need to import the gen package directly. Mirrors
// the existing pattern in users_repo.go (which returns gen.User).
type UserRecoveryCodeRow = gen.UserRecoveryCode

// RecoveryCodesRepository is the persistence boundary for
// user_recovery_codes.
type RecoveryCodesRepository struct {
	q *gen.Queries
}

// NewRecoveryCodesRepository wraps the given sqlc queries.
func NewRecoveryCodesRepository(q *gen.Queries) *RecoveryCodesRepository {
	return &RecoveryCodesRepository{q: q}
}

// Create inserts one recovery code (already SHA-256 hashed by the caller).
// The MFA service calls this N times in a loop when generating a fresh
// batch of 10 codes; the repository does not enforce a per-user cap.
func (r *RecoveryCodesRepository) Create(
	ctx context.Context,
	userID uuid.UUID,
	codeHash string,
) (gen.UserRecoveryCode, error) {
	return r.q.CreateRecoveryCode(ctx, gen.CreateRecoveryCodeParams{
		UserID:   userID,
		CodeHash: codeHash,
	})
}

// LookupUnused returns the user's unconsumed code row whose hash matches.
// Returns pgx.ErrNoRows when the hash does not match or the matching row
// has been used. The caller MUST consume via Consume on success.
func (r *RecoveryCodesRepository) LookupUnused(
	ctx context.Context,
	userID uuid.UUID,
	codeHash string,
) (gen.UserRecoveryCode, error) {
	return r.q.GetRecoveryCodeByUserAndHash(ctx, gen.GetRecoveryCodeByUserAndHashParams{
		UserID:   userID,
		CodeHash: codeHash,
	})
}

// ListForUser returns every recovery code row the user has, including
// consumed ones. Used by the dashboard to show the remaining count.
func (r *RecoveryCodesRepository) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]gen.UserRecoveryCode, error) {
	return r.q.ListRecoveryCodesForUser(ctx, userID)
}

// Consume marks the given code row as used (single-use enforcement).
// Idempotent on a consumed row (no-op), but the caller treats any row that
// was already used as a failed recovery attempt.
func (r *RecoveryCodesRepository) Consume(ctx context.Context, id, userID uuid.UUID) error {
	return r.q.ConsumeRecoveryCode(ctx, gen.ConsumeRecoveryCodeParams{ID: id, UserID: userID})
}

// DeleteAllForUser wipes every code (used and unused) for the user. Called
// by the MFA service right before regenerating a fresh batch — the old
// codes must stop working the moment new ones are issued.
func (r *RecoveryCodesRepository) DeleteAllForUser(ctx context.Context, userID uuid.UUID) error {
	return r.q.DeleteAllRecoveryCodesForUser(ctx, userID)
}

// CountUnusedForUser returns the number of unconsumed codes the user has.
// Used by the dashboard to warn when the supply is low.
func (r *RecoveryCodesRepository) CountUnusedForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return r.q.CountUnusedRecoveryCodesForUser(ctx, userID)
}
