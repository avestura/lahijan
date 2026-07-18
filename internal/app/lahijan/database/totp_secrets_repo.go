// Package database: totp_secrets_repo.go wraps the sqlc-generated
// user_totp_secrets queries. The table is global (no tenant_id) because
// a TOTP secret belongs to the user, not to any tenant — login is
// platform-wide and the same factor protects every membership.
//
// The `secret` column carries AES-GCM ciphertext produced by
// auth/secrets.Crypto; this repo never inspects or decodes it. The MFA
// service is responsible for encrypting before Create and decrypting
// after Get.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// TOTPSecretsRepository is the persistence boundary for user_totp_secrets.
type TOTPSecretsRepository struct {
	q *gen.Queries
}

// NewTOTPSecretsRepository wraps the given sqlc queries.
func NewTOTPSecretsRepository(q *gen.Queries) *TOTPSecretsRepository {
	return &TOTPSecretsRepository{q: q}
}

// UpsertTOTPSecretParams carries the fields of a TOTP enrollment row. The
// caller MUST pass already-encrypted ciphertext for the secret — never the
// raw base32 secret.
type UpsertTOTPSecretParams struct {
	UserID uuid.UUID
	// SecretCiphertext is the AES-GCM ciphertext (base64) of the base32
	// TOTP secret, produced by auth/secrets.Crypto.Seal.
	SecretCiphertext string
}

// Upsert inserts or replaces the user's TOTP secret row. A re-enrollment
// resets confirmed_at to NULL so the user must verify a fresh 6-digit code
// before the secret counts as an enrolled factor.
func (r *TOTPSecretsRepository) Upsert(
	ctx context.Context,
	arg UpsertTOTPSecretParams,
) (gen.UserTotpSecret, error) {
	return r.q.CreateTOTPSecret(ctx, gen.CreateTOTPSecretParams{
		UserID: arg.UserID,
		Secret: arg.SecretCiphertext,
	})
}

// Get returns the user's TOTP secret row. Returns pgx.ErrNoRows when the
// user has no enrollment.
func (r *TOTPSecretsRepository) Get(ctx context.Context, userID uuid.UUID) (gen.UserTotpSecret, error) {
	return r.q.GetTOTPSecret(ctx, userID)
}

// Confirm marks the user's TOTP secret as verified (the user submitted a
// valid 6-digit code from their authenticator app). Idempotent: a no-op
// when the row was already confirmed.
func (r *TOTPSecretsRepository) Confirm(ctx context.Context, userID uuid.UUID) error {
	return r.q.ConfirmTOTPSecret(ctx, userID)
}

// Delete removes the user's TOTP secret row entirely (disable TOTP).
func (r *TOTPSecretsRepository) Delete(ctx context.Context, userID uuid.UUID) error {
	return r.q.DeleteTOTPSecret(ctx, userID)
}
