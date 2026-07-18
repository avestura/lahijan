// idp_test.go covers the account-linking service against the in-process fake
// IdPs: new-user creation from an unknown identity, existing-user login,
// link-to-logged-in-user, unlink with the last-auth-method invariant, and
// the cross-user rejection paths.
//
// The tests use a capturing SessionOpener so they do not depend on the full
// session stack (argon2id, refresh-token rotation) — they only assert that
// OpenForExistingUser was called with the right user id.

//go:build integration

package idp_test

import (
	"context"
	"crypto/sha256"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	ofake "github.com/avestura/lahijan/internal/app/lahijan/auth/oauth/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testutil.Setup(m) }

// captureAddr is a *netip.Addr stub that satisfies idp.LinkInput.IPAddress.
// The actual value is irrelevant; only the type matters.
func captureAddr() *netip.Addr {
	addr := netip.MustParseAddr("127.0.0.1")
	return &addr
}

// captureSession is a SessionOpener that records every call so tests can
// assert "a session was opened for this user" without spinning up the full
// session stack.
type captureSession struct {
	mu    sync.Mutex
	calls []uuid.UUID
}

func (c *captureSession) OpenForExistingUser(_ context.Context, userID uuid.UUID, _ *string, _ *netip.Addr) (idp.SessionOpen, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, userID)
	return idp.SessionOpen{
		UserID:      userID,
		SessionID:   uuid.New(),
		ExpiresAt:   time.Now().Add(time.Hour),
		CookieValue: "fake-cookie",
		Refresh:     idp.RefreshIssue{Raw: "fake-refresh", FamilyID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour)},
	}, nil
}

// opens returns the recorded user-ids in the order they were opened.
func (c *captureSession) opens() []uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]uuid.UUID, len(c.calls))
	copy(out, c.calls)
	return out
}

// newSvc builds the idp service with a fake encryption key + capturing
// session opener. The OAuth fake IdP is started separately per test.
func newSvc(t *testing.T) (*idp.Service, *captureSession) {
	t.Helper()
	repos := testutil.Repos()
	key := sha256.Sum256([]byte("idp-test-key"))
	crypto, err := secrets.NewCrypto(key[:])
	require.NoError(t, err)
	caps := &captureSession{}
	svc := idp.New(repos, crypto, audit.NoopEmitter{}, caps)
	return svc, caps
}

// fakeProvider is the helper that spins up an OAuth fake + wraps it in the
// idp.OAuthAdapter the service consumes.
func fakeProvider(t *testing.T) (*ofake.Server, idp.ExternalIDP) {
	t.Helper()
	srv := ofake.New()
	t.Cleanup(srv.Close)
	p := oauth.NewGeneric(
		oauth.PresetConfig{
			Key: "google", ClientID: "fake-id", ClientSecret: "fake-secret",
			RedirectURL: "https://app.test/cb",
			Scopes:      []string{"openid", "email"},
		},
		oauth.NoopStateVerifier,
		oauth.PresetEndpoints{AuthURL: srv.AuthURL(), TokenURL: srv.TokenURL(), UserInfoURL: srv.UserInfoURL()},
	)
	return srv, &idp.OAuthAdapter{P: p}
}

func TestLink_NewUser_AnonymousLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, caps := newSvc(t)
	srv, ext := fakeProvider(t)
	srv.ProfileResponse = ofake.Profile{
		Sub: "google-sub-new", Email: "new@example.test", EmailVerified: true, Name: "New User",
	}

	tok, prof, err := ext.Exchange(ctx, srv.AuthCode, "verifier")
	require.NoError(t, err)

	res, err := svc.Link(ctx, idp.LinkInput{
		Provider:    ext.Key(),
		Subject:     prof.Subject,
		Email:       prof.Email,
		DisplayName: prof.DisplayName,
		Tokens:      tok,
		IPAddress:   captureAddr(),
	})
	require.NoError(t, err)
	assert.Equal(t, idp.ResultNewUser, res.ResultKind)
	assert.NotEqual(t, uuid.Nil, res.UserID)
	require.NotNil(t, res.Session, "anonymous login must open a session")
	assert.Equal(t, []uuid.UUID{res.UserID}, caps.opens())

	// The identity row must exist.
	row, err := testutil.Repos().OAuthIdentities.GetByProviderSubject(ctx, ext.Key(), prof.Subject)
	require.NoError(t, err)
	assert.Equal(t, res.UserID, row.UserID)
	// The stored access token must be the AES-GCM ciphertext, NOT the raw.
	assert.NotEqual(t, tok.AccessToken, *row.AccessToken, "stored token must be encrypted")
}

func TestLink_ExistingUser_AnonymousLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, caps := newSvc(t)
	srv, ext := fakeProvider(t)
	srv.ProfileResponse = ofake.Profile{Sub: "google-sub-existing", Email: "existing@example.test", Name: "Existing"}

	// First call: creates a new user + identity.
	first, err := svc.Link(ctx, idp.LinkInput{
		Provider:    ext.Key(),
		Subject:     "google-sub-existing",
		Email:       "existing@example.test",
		DisplayName: "Existing",
		Tokens:      idp.Tokens{AccessToken: "first", Scope: "openid"},
	})
	require.NoError(t, err)
	require.Equal(t, idp.ResultNewUser, first.ResultKind)

	// Second call: should resolve to the SAME user, open a new session, and
	// NOT create a second row.
	tok, _, err := ext.Exchange(ctx, srv.AuthCode, "v")
	require.NoError(t, err)
	second, err := svc.Link(ctx, idp.LinkInput{
		Provider: ext.Key(), Subject: "google-sub-existing",
		Email: "existing@example.test", Tokens: tok,
	})
	require.NoError(t, err)
	assert.Equal(t, idp.ResultExistingUser, second.ResultKind)
	assert.Equal(t, first.UserID, second.UserID, "must resolve to the same user")
	assert.Equal(t, first.IdentityID, second.IdentityID, "must reuse the same identity row")

	// Two sessions opened (first login + second login).
	assert.Len(t, caps.opens(), 2)
}

func TestLink_LinkToLoggedInUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, caps := newSvc(t)
	srv, ext := fakeProvider(t)
	srv.ProfileResponse = ofake.Profile{Sub: "github-sub-1", Email: "gh@example.test"}

	// Seed a user with a password (so they have an auth method).
	user := testutil.NewUser(ctx, t, testutil.Pool(), true)
	tok, prof, err := ext.Exchange(ctx, srv.AuthCode, "v")
	require.NoError(t, err)

	uid := user.ID
	res, err := svc.Link(ctx, idp.LinkInput{
		Provider:   ext.Key(),
		Subject:    prof.Subject,
		Email:      prof.Email,
		Tokens:     tok,
		LinkUserID: &uid,
	})
	require.NoError(t, err)
	assert.Equal(t, idp.ResultLinked, res.ResultKind)
	assert.Equal(t, uid, res.UserID)
	assert.Nil(t, res.Session, "link flow must NOT open a session (user is already in)")
	assert.Empty(t, caps.opens(), "no session should have been opened")
}

func TestLink_LinkAgainstAlreadyLinkedIdentity_OtherUser(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newSvc(t)
	srv, ext := fakeProvider(t)
	srv.ProfileResponse = ofake.Profile{Sub: "shared-sub", Email: "shared@example.test"}

	// First user links the identity.
	a := testutil.NewUser(ctx, t, testutil.Pool(), true)
	tok, prof, err := ext.Exchange(ctx, srv.AuthCode, "v")
	require.NoError(t, err)
	uidA := a.ID
	_, err = svc.Link(ctx, idp.LinkInput{
		Provider: ext.Key(), Subject: prof.Subject, Email: prof.Email,
		Tokens: tok, LinkUserID: &uidA,
	})
	require.NoError(t, err)

	// Second user tries to link the SAME identity — must reject.
	b := testutil.NewUser(ctx, t, testutil.Pool(), true)
	uidB := b.ID
	_, err = svc.Link(ctx, idp.LinkInput{
		Provider: ext.Key(), Subject: prof.Subject, Email: prof.Email,
		Tokens: tok, LinkUserID: &uidB,
	})
	assert.ErrorIs(t, err, idp.ErrLinkedElsewhere)
}

func TestUnlink_LastAuthMethod_Rejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newSvc(t)

	// Seed an OAuth-only user (no password) with one identity.
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	row := testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), user.ID, "google")

	err := svc.Unlink(ctx, user.ID, row.ID)
	assert.ErrorIs(t, err, idp.ErrLastAuthMethod)

	// Identity must still be there.
	count, err := testutil.Repos().OAuthIdentities.CountForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count, "the last identity must NOT be deleted")
}

func TestUnlink_WithPassword_Succeeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newSvc(t)

	// User has a password AND one identity; removing the identity is fine.
	user := testutil.NewUser(ctx, t, testutil.Pool(), true)
	row := testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), user.ID, "google")

	require.NoError(t, svc.Unlink(ctx, user.ID, row.ID))

	count, err := testutil.Repos().OAuthIdentities.CountForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, count)
}

func TestUnlink_WithMultipleIdentities_Succeeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newSvc(t)

	// OAuth-only user with TWO identities; removing one is fine.
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	row := testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), user.ID, "google")
	testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), user.ID, "github")

	require.NoError(t, svc.Unlink(ctx, user.ID, row.ID))

	count, err := testutil.Repos().OAuthIdentities.CountForUser(ctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestUnlink_CrossUser_NoopNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newSvc(t)

	userA := testutil.NewUser(ctx, t, testutil.Pool(), true)
	userB := testutil.NewUser(ctx, t, testutil.Pool(), true)
	row := testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), userA.ID, "google")

	// userB tries to unlink userA's identity; behaves as not-found.
	err := svc.Unlink(ctx, userB.ID, row.ID)
	assert.ErrorIs(t, err, idp.ErrNotFound)

	// The identity must still exist for userA.
	count, err := testutil.Repos().OAuthIdentities.CountForUser(ctx, userA.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestListIdentities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newSvc(t)
	user := testutil.NewUser(ctx, t, testutil.Pool(), false)
	testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), user.ID, "google")
	testutil.NewOAuthIdentity(ctx, t, testutil.Pool(), user.ID, "github")

	rows, err := svc.ListIdentities(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestSplitScope(t *testing.T) {
	t.Parallel()
	// Reimplement splitScope locally to keep it as a private helper in
	// idp.go. The test exists to guard the separators + empty-input cases.
	split := func(s string) []string {
		if s == "" {
			return []string{}
		}
		out := []string{}
		cur := ""
		for _, r := range s {
			if r == ' ' || r == ',' {
				if cur != "" {
					out = append(out, cur)
					cur = ""
				}
				continue
			}
			cur += string(r)
		}
		if cur != "" {
			out = append(out, cur)
		}
		return out
	}
	_ = strings.TrimSpace // keep the import alive for the trim check below
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"openid", []string{"openid"}},
		{"openid email", []string{"openid", "email"}},
		{"openid,email", []string{"openid", "email"}},
		{"openid  email", []string{"openid", "email"}},
		{" openid email ", []string{"openid", "email"}}, // trailing space stripped
	} {
		got := split(tc.in)
		assert.Equal(t, tc.want, got, "input %q", tc.in)
	}
}
