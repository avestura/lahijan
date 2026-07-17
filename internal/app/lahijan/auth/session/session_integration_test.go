// session_integration_test.go exercises the full session lifecycle against a
// real Postgres via testcontainers: register -> login -> refresh rotation ->
// reuse detection -> logout -> reset-password flow. It is the WS-06 DoD
// "integration tests for register, login, refresh rotation, reuse detection,
// reset flow" requirement.

//go:build integration

package session_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
)

// TestMain starts one testcontainers Postgres for the package, applies all
// migrations, runs the package, then tears down. Per the testing convention,
// each test package owns its own container (no cross-package sharing).
func TestMain(m *testing.M) { testutil.Setup(m) }

// newAuthStack builds the session + email services from the shared repos with
// intentionally weak argon2 params so the tests run fast.
func newAuthStack(t *testing.T) (*session.Service, *email.Service) {
	t.Helper()
	repos := testutil.Repos()
	signer := secrets.NewSigner("test-signing-key")
	hasher := password.NewHasher(4, 1, 1, 8, 16)
	mailer := email.New(
		repos.Users, repos.EmailTokens, hasher, signer,
		notifyemail.NoopSender{}, audit.NoopEmitter{},
		email.Config{
			VerifyTTL:      time.Hour,
			ResetTTL:       time.Hour,
			EmailChangeTTL: time.Hour,
			TokenByteLen:   32,
			AppBaseURL:     "https://app.test",
		},
	)
	svc := session.New(
		repos.Users, repos.Sessions, repos.Tokens, hasher, signer,
		mailer, audit.NoopEmitter{},
		session.Config{
			SessionLifetime: 24 * time.Hour,
			RefreshLifetime: 24 * time.Hour,
			TokenByteLen:    32,
			MinPasswordLen:  12,
		},
	)
	return svc, mailer
}

func TestRegister_Happy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newAuthStack(t)

	addr := "alice+" + uuid8() + "@example.test"
	sess, err := svc.Register(ctx, session.RegisterInput{
		Email:    addr,
		Password: "VeryStrong123!xyz",
		Locale:   "en",
	})
	require.NoError(t, err, "register must succeed with a strong password")
	assert.NotEmpty(t, sess.CookieValue, "register returns a session cookie value")
	assert.NotEmpty(t, sess.Refresh.Raw, "register returns a refresh token")
	assert.NotEqual(t, sess.Refresh.FamilyID, sess.UserID, "family id differs from user id")
}

func TestRegister_WeakPasswordFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newAuthStack(t)

	_, err := svc.Register(ctx, session.RegisterInput{
		Email:    "weak+" + uuid8() + "@example.test",
		Password: "short",
	})
	assert.ErrorIs(t, err, password.ErrPasswordTooWeak)
}

func TestRegister_DuplicateEmailFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newAuthStack(t)
	addr := "dup+" + uuid8() + "@example.test"

	_, err := svc.Register(ctx, session.RegisterInput{
		Email: addr, Password: "VeryStrong123!xyz", Locale: "en",
	})
	require.NoError(t, err)

	_, err = svc.Register(ctx, session.RegisterInput{
		Email: addr, Password: "VeryStrong123!xyz", Locale: "en",
	})
	assert.ErrorIs(t, err, session.ErrEmailTaken)
}

func TestLogin_HappyAndFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newAuthStack(t)
	addr := "login+" + uuid8() + "@example.test"
	pw := "VeryStrong123!xyz"

	_, err := svc.Register(ctx, session.RegisterInput{Email: addr, Password: pw, Locale: "en"})
	require.NoError(t, err)

	sess, err := svc.Login(ctx, session.LoginInput{Email: addr, Password: pw})
	require.NoError(t, err, "login with correct credentials must succeed")
	assert.NotEmpty(t, sess.CookieValue)

	_, err = svc.Login(ctx, session.LoginInput{Email: addr, Password: "WrongPassword99!"})
	assert.ErrorIs(t, err, session.ErrInvalidCredentials)

	_, err = svc.Login(ctx, session.LoginInput{Email: "nobody+" + uuid8() + "@example.test", Password: pw})
	assert.ErrorIs(t, err, session.ErrInvalidCredentials)
}

func TestRefresh_RotationAndReuseDetection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newAuthStack(t)
	addr := "rotate+" + uuid8() + "@example.test"
	pw := "VeryStrong123!xyz"

	reg, err := svc.Register(ctx, session.RegisterInput{Email: addr, Password: pw, Locale: "en"})
	require.NoError(t, err)

	// First refresh: rotates the token within the family.
	r1, err := svc.Refresh(ctx, reg.Refresh.Raw, nil, nil)
	require.NoError(t, err)
	assert.NotEqual(t, reg.Refresh.Raw, r1.Refresh.Raw, "refresh must issue a new token")
	assert.Equal(t, reg.Refresh.FamilyID, r1.Refresh.FamilyID, "rotation keeps the family")

	// The old token must now be considered consumed.
	_, err = svc.Refresh(ctx, reg.Refresh.Raw, nil, nil)
	require.ErrorIs(t, err, session.ErrRefreshReuse, "reusing a rotated token is reuse")

	// The valid rotated token must also fail now (family burned down).
	_, err = svc.Refresh(ctx, r1.Refresh.Raw, nil, nil)
	assert.ErrorIs(t, err, session.ErrRefreshReuse, "the whole family must be revoked on reuse")
}

func TestLogout_RevokesSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newAuthStack(t)
	addr := "logout+" + uuid8() + "@example.test"
	pw := "VeryStrong123!xyz"

	reg, err := svc.Register(ctx, session.RegisterInput{Email: addr, Password: pw, Locale: "en"})
	require.NoError(t, err)

	require.NoError(t, svc.Logout(ctx, reg.Refresh.Raw))
	// After logout, the refresh token must no longer work (revoked).
	_, err = svc.Refresh(ctx, reg.Refresh.Raw, nil, nil)
	assert.ErrorIs(t, err, session.ErrRefreshReuse)
}

func TestPasswordReset_FullFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := testutil.Repos()
	signer := secrets.NewSigner("reset-test-key")
	hasher := password.NewHasher(4, 1, 1, 8, 16)
	capture := &capturingSender{}
	mailer := email.New(
		repos.Users, repos.EmailTokens, hasher, signer,
		capture, audit.NoopEmitter{},
		email.Config{
			ResetTTL:     time.Hour,
			TokenByteLen: 32,
			AppBaseURL:   "https://app.test",
		},
	)

	addr := "reset+" + uuid8() + "@example.test"
	oldPw := "VeryStrong123!xyz"
	newPw := "BrandNewPass456!xyz"

	// Seed a user via the session service (fast path), then drive reset via the
	// email service with the capturing mailer.
	sessSvc, _ := newAuthStack(t)
	_, err := sessSvc.Register(ctx, session.RegisterInput{Email: addr, Password: oldPw, Locale: "en"})
	require.NoError(t, err)

	require.NoError(t, mailer.RequestPasswordReset(ctx, addr), "reset request must not error")
	require.NoError(t, mailer.RequestPasswordReset(ctx, "definitely-missing@example.test"),
		"reset request for unknown email must also succeed (no enumeration)")

	raw := capture.lastToken(t)
	require.NoError(t, mailer.ConfirmPasswordReset(ctx, raw, newPw), "confirm must consume token + set password")

	// Old password must no longer work; new password must.
	_, err = sessSvc.Login(ctx, session.LoginInput{Email: addr, Password: oldPw})
	assert.ErrorIs(t, err, session.ErrInvalidCredentials, "old password must be invalid after reset")

	_, err = sessSvc.Login(ctx, session.LoginInput{Email: addr, Password: newPw})
	require.NoError(t, err, "new password must work after reset")

	// The token must be single-use: a second confirm with the same token fails.
	require.ErrorIs(t, mailer.ConfirmPasswordReset(ctx, raw, "AnotherNew789!xyz"),
		email.ErrInvalidToken, "reset token must be single-use")
}

// uuid8 returns a short unique suffix for disambiguating emails across parallel
// test runs. uuid.NewString never collides.
func uuid8() string { return uuid.NewString()[:8] }

// capturingSender records every email it would have sent, so integration tests
// can extract the raw verify/reset link token without an SMTP roundtrip.
type capturingSender struct {
	emails []notifyemail.Email
}

func (c *capturingSender) Send(_ context.Context, m notifyemail.Email) error {
	c.emails = append(c.emails, m)
	return nil
}

// tokenRe matches a signed token of the form base64url(payload).base64url(sig).
// base64url uses [A-Za-z0-9_-]; the single dot separates the two halves. The
// trailing sentence period after the link is NOT matched because a second dot
// is required to be followed by base64url characters.
var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`)

// lastToken parses the last captured email body for the raw token query value.
func (c *capturingSender) lastToken(t *testing.T) string {
	t.Helper()
	require.NotEmpty(t, c.emails, "expected at least one captured email")
	body := c.emails[len(c.emails)-1].Body
	m := tokenRe.FindStringSubmatch(body)
	require.NotNilf(t, m, "email body must contain a token= parameter; body was: %s", body)
	return m[1]
}
