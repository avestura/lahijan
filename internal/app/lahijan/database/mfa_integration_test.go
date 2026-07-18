// mfa_*_integration_test.go verifies the WS-07c MFA persistence layer end
// to end against a real Postgres via testcontainers. Covers:
//
//   - TOTP secret upsert/get/confirm/delete round-trips
//   - WebAuthn credential create/list/update-sign-count/delete
//   - Recovery code create/lookup/consume/delete-wipe
//   - MFA pending session create/get/consume/revoke/inc-failures
//   - Tenant mfa_required policy drives AnyTenantRequiresMFAForUser
//
// The persistence contract is what every DoD row in WS-07c that involves
// "works end-to-end" ultimately rests on: the service layer cannot be
// correct unless these round-trips are.

//go:build integration

package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

// ---- TOTP secrets --------------------------------------------------------

func TestTOTPSecrets_UpsertGetConfirmDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, true)

	// Upsert (initial insert path).
	const enc1 = "enc::fake-ciphertext::v1"
	row, err := repos.TOTPSecrets.Upsert(ctx, database.UpsertTOTPSecretParams{
		UserID:           user.ID,
		SecretCiphertext: enc1,
	})
	require.NoError(t, err)
	assert.Nil(t, row.ConfirmedAt, "fresh enrollment must be unconfirmed")

	// Confirm.
	require.NoError(t, repos.TOTPSecrets.Confirm(ctx, user.ID))
	got, err := repos.TOTPSecrets.Get(ctx, user.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.ConfirmedAt)
	assert.Equal(t, enc1, got.Secret)

	// Upsert again (re-enroll) — confirmed_at must reset to NULL.
	row, err = repos.TOTPSecrets.Upsert(ctx, database.UpsertTOTPSecretParams{
		UserID:           user.ID,
		SecretCiphertext: "enc::fake-ciphertext::v2",
	})
	require.NoError(t, err)
	assert.Nil(t, row.ConfirmedAt, "re-enrollment must reset confirmed_at")

	// Delete.
	require.NoError(t, repos.TOTPSecrets.Delete(ctx, user.ID))
	_, err = repos.TOTPSecrets.Get(ctx, user.ID)
	require.Error(t, err, "Get after Delete must error")
}

// ---- WebAuthn credentials -----------------------------------------------

func TestWebauthnCredentials_CreateListCountDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, true)

	const credID = "cred-abc-123"
	const pubKey = "\x04\x01\x02\x03"
	row, err := repos.WebauthnCreds.Create(ctx, database.CreateWebauthnCredentialParams{
		UserID:       user.ID,
		CredentialID: credID,
		PublicKey:    []byte(pubKey),
		SignCount:    0,
		Transports:   []string{"usb", "internal"},
		Name:         "Touch ID",
	})
	require.NoError(t, err)

	// List + count.
	rows, err := repos.WebauthnCreds.ListForUser(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	count, err := repos.WebauthnCreds.CountForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)

	// Lookup by (user, credential_id).
	got, err := repos.WebauthnCreds.GetByUserAndID(ctx, user.ID, credID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	// Bump sign counter.
	require.NoError(t, repos.WebauthnCreds.UpdateSignCount(ctx, row.ID, user.ID, 42))
	got, err = repos.WebauthnCreds.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 42, got.SignCount)

	// Cross-user delete is a no-op.
	other := testutil.NewUser(ctx, t, pool, true)
	require.NoError(t, repos.WebauthnCreds.Delete(ctx, row.ID, other.ID))
	count, _ = repos.WebauthnCreds.CountForUser(ctx, user.ID)
	assert.EqualValues(t, 1, count, "cross-user delete must not affect the row")

	// Self delete.
	require.NoError(t, repos.WebauthnCreds.Delete(ctx, row.ID, user.ID))
	count, _ = repos.WebauthnCreds.CountForUser(ctx, user.ID)
	assert.EqualValues(t, 0, count)
}

// ---- Recovery codes ------------------------------------------------------

func TestRecoveryCodes_CreateLookupConsumeWipe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, true)

	// Issue 3 codes; 2 unused, 1 consumed.
	hashes := []string{"h1", "h2", "h3"}
	for _, h := range hashes {
		_, err := repos.RecoveryCodes.Create(ctx, user.ID, h)
		require.NoError(t, err)
	}
	// Lookup unused.
	row, err := repos.RecoveryCodes.LookupUnused(ctx, user.ID, "h2")
	require.NoError(t, err)
	// Consume.
	require.NoError(t, repos.RecoveryCodes.Consume(ctx, row.ID, user.ID))
	// Lookup again — must NOT find it.
	_, err = repos.RecoveryCodes.LookupUnused(ctx, user.ID, "h2")
	require.Error(t, err, "consumed code must not be returned by LookupUnused")

	// Counters: total 3, remaining 2.
	rows, err := repos.RecoveryCodes.ListForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	remaining, err := repos.RecoveryCodes.CountUnusedForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, remaining)

	// Wipe and re-count.
	require.NoError(t, repos.RecoveryCodes.DeleteAllForUser(ctx, user.ID))
	remaining, _ = repos.RecoveryCodes.CountUnusedForUser(ctx, user.ID)
	assert.EqualValues(t, 0, remaining)
}

// ---- MFA pending sessions -----------------------------------------------

func TestMFAPendingSessions_CreateGetConsumeRevokeFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, true)

	row, err := repos.MFAPending.Create(ctx, database.CreateMFAPendingSessionParams{
		UserID:    user.ID,
		TokenHash: "h-" + uuid.NewString(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})
	require.NoError(t, err)
	assert.EqualValues(t, 0, row.FailedAttempts)

	// Get by hash.
	got, err := repos.MFAPending.GetByHash(ctx, row.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	// IncFailures twice.
	require.NoError(t, repos.MFAPending.IncFailures(ctx, row.ID))
	require.NoError(t, repos.MFAPending.IncFailures(ctx, row.ID))
	got, _ = repos.MFAPending.GetByHash(ctx, row.TokenHash)
	assert.EqualValues(t, 2, got.FailedAttempts)

	// Revoke + Consume — the consume on an already-revoked row is a no-op
	// (idempotent) at the repo layer; the service layer treats it as
	// invalid via the in-memory check.
	require.NoError(t, repos.MFAPending.Revoke(ctx, row.ID))
	require.NoError(t, repos.MFAPending.Consume(ctx, row.ID))
}

// ---- Tenant policy -------------------------------------------------------

func TestAnyTenantRequiresMFA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()

	tenantReq := testutil.NewTenant(ctx, t, pool)
	tenantNot := testutil.NewTenant(ctx, t, pool)
	// Flip tenantReq.mfa_required to TRUE.
	_, err := pool.Exec(ctx, "UPDATE tenants SET mfa_required = TRUE WHERE id = $1", tenantReq.ID)
	require.NoError(t, err)

	role := testutil.NewRole(ctx, t, pool, "r-"+uuid.NewString()[:8], "role")
	userIn := testutil.NewUser(ctx, t, pool, true)
	userOut := testutil.NewUser(ctx, t, pool, true)
	testutil.NewMembership(ctx, t, pool, tenantReq.ID, userIn.ID, &role.ID)
	testutil.NewMembership(ctx, t, pool, tenantNot.ID, userOut.ID, &role.ID)

	required, err := repos.Memberships.AnyTenantRequiresMFA(ctx, userIn.ID)
	require.NoError(t, err)
	assert.True(t, required, "user in mfa_required tenant must report true")

	required, err = repos.Memberships.AnyTenantRequiresMFA(ctx, userOut.ID)
	require.NoError(t, err)
	assert.False(t, required, "user only in non-required tenant must report false")

	// A user with no memberships at all also reports false.
	required, err = repos.Memberships.AnyTenantRequiresMFA(ctx, uuid.New())
	require.NoError(t, err)
	assert.False(t, required)
}
