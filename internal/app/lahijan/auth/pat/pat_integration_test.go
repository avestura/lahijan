// pat_integration_test.go exercises the PAT service end to end against a real
// Postgres via testcontainers: create -> authenticate -> revoke, plus expiry
// enforcement. This is the WS-06 DoD "PAT issuance + use + revocation" item.

//go:build integration

package pat_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
)

// TestMain starts one testcontainers Postgres for the package. Per the testing
// convention each test package owns its own container.
func TestMain(m *testing.M) { testutil.Setup(m) }

func newPATService(t *testing.T) *pat.Service {
	t.Helper()
	repos := testutil.Repos()
	signer := secrets.NewSigner("pat-test-key")
	return pat.New(
		repos.Tokens, signer, audit.NoopEmitter{},
		pat.Config{Prefix: "lah_pat_", ByteLen: 32, MaxExpiry: 0},
	)
}

func TestPAT_CreateAuthenticateListRevoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newPATService(t)
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	created, err := svc.Create(ctx, pat.CreateInput{
		UserID: user.ID,
		Name:   "ci-bot",
		Scopes: []string{"compute.instance.read", "dns.zone.read"},
	})
	require.NoError(t, err, "create must succeed")
	assert.NotEmpty(t, created.Raw, "raw PAT is shown once")
	assert.Contains(t, created.Raw, "lah_pat_", "raw PAT carries the configured prefix")
	assert.Equal(t, []string{"compute.instance.read", "dns.zone.read"}, created.Scopes)

	// Authenticate with the raw token.
	principal, err := svc.Authenticate(ctx, created.Raw)
	require.NoError(t, err, "authenticate must succeed with a valid PAT")
	assert.Equal(t, user.ID, principal.UserID)
	assert.Equal(t, created.Scopes, principal.Scopes)

	// The PAT shows up in the user's list.
	list, err := svc.List(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "ci-bot", list[0].Name)

	// Revoke it; Authenticate must now reject it.
	require.NoError(t, svc.Revoke(ctx, created.ID, user.ID))
	_, err = svc.Authenticate(ctx, created.Raw)
	assert.ErrorIs(t, err, pat.ErrNotFound, "revoked PAT must not authenticate")
}

func TestPAT_AuthenticateRejectsExpiredAndUnknown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newPATService(t)
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	// An expired PAT.
	past := time.Now().Add(-time.Hour)
	expired, err := svc.Create(ctx, pat.CreateInput{
		UserID: user.ID, Name: "old", Scopes: nil, ExpiresAt: &past,
	})
	require.NoError(t, err)
	_, err = svc.Authenticate(ctx, expired.Raw)
	assert.ErrorIs(t, err, pat.ErrNotFound, "expired PAT must not authenticate")

	// An unknown token (signed by a different key).
	other := secrets.NewSigner("a-different-key")
	raw, _, _ := other.Issue(32)
	_, err = svc.Authenticate(ctx, "lah_pat_"+raw)
	assert.ErrorIs(t, err, pat.ErrNotFound, "token from another signer must not authenticate")

	// Garbage.
	_, err = svc.Authenticate(ctx, "lah_pat_garbage")
	assert.ErrorIs(t, err, pat.ErrNotFound)
}

func TestPAT_RevokeRejectsOtherUsersPAT(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newPATService(t)
	owner := testutil.NewUser(ctx, t, testutil.Pool(), false)
	other := testutil.NewUser(ctx, t, testutil.Pool(), false)

	created, err := svc.Create(ctx, pat.CreateInput{UserID: owner.ID, Name: "mine"})
	require.NoError(t, err)

	// The other user cannot revoke the owner's PAT.
	err = svc.Revoke(ctx, created.ID, other.ID)
	assert.ErrorIs(t, err, pat.ErrNotFound, "revoking another user's PAT must fail closed")
}

func TestPAT_ValidateRejectsBadScopes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newPATService(t)
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)

	// A scope without the scope.action dot is rejected.
	_, err := svc.Create(ctx, pat.CreateInput{
		UserID: user.ID, Name: "bad", Scopes: []string{"computeread"},
	})
	assert.ErrorIs(t, err, pat.ErrInvalidScope)

	// Empty name is rejected.
	_, err = svc.Create(ctx, pat.CreateInput{UserID: user.ID, Name: "  "})
	assert.ErrorIs(t, err, pat.ErrNameRequired)
}
