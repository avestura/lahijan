// oauth_identities_integration_test.go verifies the user_oauth_identities
// persistence layer end to end against a real Postgres via testcontainers.
// Covers the WS-07a DoD "OAuth/OIDC tokens encrypted at rest" row-level
// contract (the table holds opaque ciphertext columns; the AES-GCM envelope
// itself is exercised in auth/idp tests).

//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

// fakeEncToken is a placeholder for the AES-GCM ciphertext the real auth/idp
// service produces. The repo treats the column as opaque TEXT; what matters
// for these tests is that the value round-trips unchanged.
const fakeEncToken = "enc::fake-ciphertext::v1"

func TestOAuthIdentities_CreateAndGet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, false)

	row := testutil.NewOAuthIdentity(ctx, t, pool, user.ID, "google")

	// Get by id.
	got, err := repos.OAuthIdentities.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)
	assert.Equal(t, "google", got.Provider)
	assert.Equal(t, user.ID, got.UserID)

	// Get by (provider, subject).
	got2, err := repos.OAuthIdentities.GetByProviderSubject(ctx, row.Provider, row.Subject)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got2.ID)

	// Get for user.
	got3, err := repos.OAuthIdentities.GetForUser(ctx, user.ID, "google")
	require.NoError(t, err)
	assert.Equal(t, row.ID, got3.ID)
}

func TestOAuthIdentities_UniqueProviderSubject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	userA := testutil.NewUser(ctx, t, pool, false)
	userB := testutil.NewUser(ctx, t, pool, false)

	// Link userA to github:sub-xyz.
	_, err := repos.OAuthIdentities.Create(ctx, oauthCreateParams(userA.ID, "github", "sub-xyz"))
	require.NoError(t, err)

	// Re-linking userB to the same (github, sub-xyz) must fail with a unique
	// violation (the subject is already bound to userA).
	_, err = repos.OAuthIdentities.Create(ctx, oauthCreateParams(userB.ID, "github", "sub-xyz"))
	require.Error(t, err, "(provider, subject) must be globally unique")
}

func TestOAuthIdentities_UniqueUserProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, false)

	// First google link for this user.
	_, err := repos.OAuthIdentities.Create(ctx, oauthCreateParams(user.ID, "google", "sub-a"))
	require.NoError(t, err)

	// A second google link for the same user must fail (one per provider).
	_, err = repos.OAuthIdentities.Create(ctx, oauthCreateParams(user.ID, "google", "sub-b"))
	require.Error(t, err, "one identity per (user, provider)")
}

func TestOAuthIdentities_ListForUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, false)

	testutil.NewOAuthIdentity(ctx, t, pool, user.ID, "google")
	testutil.NewOAuthIdentity(ctx, t, pool, user.ID, "github")

	rows, err := repos.OAuthIdentities.ListForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, rows, 2, "user should have two identities")
}

func TestOAuthIdentities_UpdateTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, false)
	row := testutil.NewOAuthIdentity(ctx, t, pool, user.ID, "google")

	newTok := "enc::refreshed-access::v2"
	newRefresh := "enc::refreshed-refresh::v2"
	require.NoError(t, repos.OAuthIdentities.UpdateTokens(ctx, database.UpdateOAuthIdentityTokensParams{
		ID:           row.ID,
		UserID:       user.ID,
		AccessToken:  &newTok,
		RefreshToken: &newRefresh,
		Scopes:       []string{"openid", "email"},
	}))

	got, err := repos.OAuthIdentities.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, newTok, *got.AccessToken)
	assert.Equal(t, newRefresh, *got.RefreshToken)
}

func TestOAuthIdentities_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	user := testutil.NewUser(ctx, t, pool, false)
	row := testutil.NewOAuthIdentity(ctx, t, pool, user.ID, "google")

	require.NoError(t, repos.OAuthIdentities.Delete(ctx, row.ID, user.ID))

	count, err := repos.OAuthIdentities.CountForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, count)
}

func TestOAuthIdentities_Delete_OtherUserIsNoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := testutil.Pool()
	repos := testutil.Repos()
	userA := testutil.NewUser(ctx, t, pool, false)
	userB := testutil.NewUser(ctx, t, pool, false)
	row := testutil.NewOAuthIdentity(ctx, t, pool, userA.ID, "google")

	// userB cannot delete userA's identity; the (id, user_id) WHERE clause
	// makes the delete a no-op without erroring.
	require.NoError(t, repos.OAuthIdentities.Delete(ctx, row.ID, userB.ID))

	count, err := repos.OAuthIdentities.CountForUser(ctx, userA.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count, "cross-user delete must be a no-op")
}

// oauthCreateParams builds the typed params for Create with a fake encrypted
// token + openid/email scopes. The userID+provider+subject are the only
// fields that vary per test; the rest are stable defaults.
func oauthCreateParams(userID uuid.UUID, provider, subject string) database.CreateOAuthIdentityParams {
	tok := fakeEncToken
	return database.CreateOAuthIdentityParams{
		UserID:       userID,
		Provider:     provider,
		Subject:      subject,
		AccessToken:  &tok,
		RefreshToken: &tok,
		Scopes:       []string{"openid", "email"},
	}
}
