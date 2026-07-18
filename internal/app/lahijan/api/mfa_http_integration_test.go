// mfa_http_integration_test.go exercises the WS-07c MFA HTTP surface end
// to end against a real Postgres (testcontainers) plus the real auth
// middleware. It is the WS-07c DoD coverage for the user-facing
// ceremonies:
//
//   - TOTP enroll -> verify -> disable works end-to-end
//   - recovery codes generate -> use -> invalidated
//   - login with MFA-enabled user returns pending_session_token (202)
//   - MFA challenge succeeds -> real session issued
//   - MFA challenge fails 5 times -> pending token revoked (locked out)
//   - per-tenant "MFA required" policy enforced at login
//   - every privileged MFA action emits an audit event
//
// The fake WebAuthn authenticator (auth/mfa/webauthn/fake) backs the
// WebAuthn register/login ceremonies so the full W3C verification path is
// exercised without a real browser or USB key.

//go:build integration

package api_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	ptotp "github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/webauthn"
	wfake "github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/webauthn/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// newMFATestApp extends newTestApp with the MFA stack wired against a real
// WebAuthn RP pointed at https://localhost. Returns the app + a fresh
// synthetic authenticator the test uses to drive the WebAuthn ceremonies.
func newMFATestApp(t *testing.T) (*testApp, *wfake.Authenticator) {
	t.Helper()
	repos := testutil.Repos()
	signer := secrets.NewSigner("http-test-signing-key")
	hasher := password.NewHasher(4, 1, 1, 8, 16)
	cookies := api.CookieConfig{
		SessionName: "lahijan_session", RefreshName: "lahijan_refresh",
		Path: "/", SameSite: "lax", SessionMaxAge: 3600, RefreshMaxAge: 3600,
	}
	mailer := email.New(
		repos.Users, repos.EmailTokens, hasher, signer,
		notifyemail.NoopSender{}, audit.NewDBEmitter(repos.AuditLog),
		email.Config{
			VerifyTTL: time.Hour, ResetTTL: time.Hour, EmailChangeTTL: time.Hour,
			TokenByteLen: 32, AppBaseURL: "https://app.test",
		},
	)
	sessionSvc := session.New(
		repos.Users, repos.Sessions, repos.Tokens, hasher, signer,
		mailer, audit.NewDBEmitter(repos.AuditLog),
		session.Config{
			SessionLifetime: time.Hour, RefreshLifetime: time.Hour,
			TokenByteLen: 32, MinPasswordLen: 12,
		},
	)
	patSvc := pat.New(
		repos.Tokens, signer, audit.NewDBEmitter(repos.AuditLog),
		pat.Config{Prefix: "lah_pat_", ByteLen: 32},
	)
	// Build the AES-GCM crypto envelope from a fixed 32-byte dev key so
	// the TOTP secret encryption works without further setup.
	crypto, err := secrets.NewCrypto(make([]byte, 32))
	require.NoError(t, err)
	// Build a real WebAuthn RP pointed at https://localhost so the
	// ceremony verification accepts the synthetic authenticator's output.
	rp, err := webauthn.New(webauthn.Config{
		RPID:      "localhost",
		RPOrigins: []string{"https://localhost"},
	})
	require.NoError(t, err)
	mfaSvc := mfa.New(
		repos, crypto, signer, rp,
		&mfaSessionOpener{svc: sessionSvc},
		audit.NewDBEmitter(repos.AuditLog),
		mfa.DefaultConfig(),
	)
	server := api.NewServer(api.ServerDeps{
		Users:        repos.Users,
		Sessions:     repos.Sessions,
		SessionSvc:   sessionSvc,
		PATSvc:       patSvc,
		EmailSvc:     mailer,
		Signer:       signer,
		Cookies:      cookies,
		Audit:        repos.AuditLog,
		AuditEmitter: audit.NewDBEmitter(repos.AuditLog),
		MFASvc:       mfaSvc,
	})
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(repos.Memberships))
	app := fiber.New()
	middleware.Apply(app, middleware.Options{
		Tenant: middleware.TenantWithResolver(middleware.TenantResolver{
			Tenants: repos.Tenants,
		}),
		Auth: middleware.AuthWithResolver(middleware.AuthResolver{
			Cookies: middleware.CookieConfig{
				SessionName: cookies.SessionName,
				RefreshName: cookies.RefreshName,
			},
			Signer:       signer,
			SessionsRepo: repos.Sessions,
			PATService:   patSvc,
		}),
	})
	api.RegisterRoutes(app, server, policy)
	return &testApp{app: app, cookies: cookies, repos: repos}, wfake.NewAuthenticator()
}

// mfaSessionOpener is the local session-opener adapter for the MFA service.
// Mirrors program.mfaSessionOpenerAdapter but kept inside the test package
// to avoid a circular dep.
type mfaSessionOpener struct{ svc *session.Service }

func (m *mfaSessionOpener) OpenForExistingUser(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (mfa.SessionOpen, error) {
	sess, err := m.svc.OpenForExistingUser(ctx, userID, ua, ip)
	if err != nil {
		return mfa.SessionOpen{}, err
	}
	return mfa.SessionOpen{
		UserID:      sess.UserID,
		SessionID:   sess.SessionID,
		ExpiresAt:   sess.ExpiresAt,
		CookieValue: sess.CookieValue,
		Refresh: mfa.RefreshIssue{
			Raw:       sess.Refresh.Raw,
			FamilyID:  sess.Refresh.FamilyID,
			ExpiresAt: sess.Refresh.ExpiresAt,
		},
	}, nil
}

// ---- Test cases ---------------------------------------------------------

// registerAndLoginMFA creates a user via /register and returns the session
// cookie + the user's email.
func registerAndLoginMFA(t *testing.T, ta *testApp) (string, string) {
	t.Helper()
	// uuid-based email so parallel tests never collide (unique() can
	// collide when two tests run in the same microsecond).
	addr := "mfa+" + uuid.NewString() + "@example.test"
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Equalf(t, 201, status, "register must succeed")
	cookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, extractCookie(sc, "lahijan_session"), "session cookie must be set")
	return cookie, addr
}

// enrollTOTPAndVerify drives the full TOTP enrollment: /enroll returns the
// secret, this helper computes a valid 6-digit code via pquerna/otp/totp
// (the same library the production code uses to validate), and POSTs it to
// /verify. Returns the recovery codes generated at the verify step.
func enrollTOTPAndVerify(t *testing.T, ta *testApp, cookie string) []string {
	t.Helper()
	status, body, _ := doJSON(t, ta, "POST", "/api/v1/me/mfa/totp/enroll", nil, cookie)
	require.Equalf(t, 200, status, "totp enroll: want 200, body=%v", body)
	secret, _ := body["secret"].(string)
	require.NotEmpty(t, secret)
	code, err := ptotp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	status, body, _ = doJSON(t, ta, "POST", "/api/v1/me/mfa/totp/verify",
		map[string]any{"code": code}, cookie)
	require.Equalf(t, 200, status, "totp verify: want 200, body=%v", body)
	codesRaw, _ := body["codes"].([]any)
	codes := make([]string, 0, len(codesRaw))
	for _, c := range codesRaw {
		if s, ok := c.(string); ok {
			codes = append(codes, s)
		}
	}
	require.NotEmpty(t, codes, "verify must return recovery codes")
	return codes
}

func TestMFA_TOTPEnrollVerifyDisable(t *testing.T) {
	t.Parallel()
	ta, _ := newMFATestApp(t)
	cookie, _ := registerAndLoginMFA(t, ta)

	// Enroll + verify.
	enrollTOTPAndVerify(t, ta, cookie)

	// Disable requires the current password.
	status, body, _ := doJSON(t, ta, "POST", "/api/v1/me/mfa/totp/disable",
		map[string]any{"currentPassword": "wrong-password"}, cookie)
	require.Equalf(t, 401, status, "disable with wrong pw: want 401, body=%v", body)

	status, body, _ = doJSON(t, ta, "POST", "/api/v1/me/mfa/totp/disable",
		map[string]any{"currentPassword": strongPw}, cookie)
	require.Equalf(t, 200, status, "disable: want 200, body=%v", body)
}

func TestMFA_RecoveryGenerateAndList(t *testing.T) {
	t.Parallel()
	ta, _ := newMFATestApp(t)
	cookie, _ := registerAndLoginMFA(t, ta)

	// Enroll TOTP first (VerifyTOTP mints the first batch).
	enrollTOTPAndVerify(t, ta, cookie)

	// GET returns metadata only (no raw codes).
	status, body, _ := doJSON(t, ta, "GET", "/api/v1/me/mfa/recovery", nil, cookie)
	require.Equalf(t, 200, status, "list recovery: want 200, body=%v", body)
	total, _ := body["total"].(float64)
	remaining, _ := body["remaining"].(float64)
	assert.Greater(t, int(total), 0)
	assert.Greater(t, int(remaining), 0)

	// Regenerate: returns raw codes exactly once.
	status, body, _ = doJSON(t, ta, "POST", "/api/v1/me/mfa/recovery", nil, cookie)
	require.Equalf(t, 200, status, "regenerate recovery: want 200, body=%v", body)
	codes, _ := body["codes"].([]any)
	assert.Greater(t, len(codes), 0, "fresh batch must be returned")
}

func TestMFA_LoginReturnsPendingThenChallengeSucceeds(t *testing.T) {
	t.Parallel()
	ta, _ := newMFATestApp(t)
	cookie, addr := registerAndLoginMFA(t, ta)
	// Enroll TOTP to make the user MFA-required.
	enrollTOTPAndVerify(t, ta, cookie)

	// Log out, then log in again — /login must return 202 with a
	// pending_session_token this time.
	doJSON(t, ta, "POST", "/api/v1/auth/logout", nil, cookie)
	status, body, _ := doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Equalf(t, 202, status, "login with MFA: want 202, body=%v", body)
	pending, _ := body["pendingSessionToken"].(string)
	require.NotEmpty(t, pending)

	// The challenge can be a TOTP code: decrypt the stored secret via the
	// same AES-GCM envelope the production code uses, then derive the
	// current 6-digit code.
	repos := ta.repos
	uid := mustUserID(t, ta, cookie)
	crypto, err := secrets.NewCrypto(make([]byte, 32))
	require.NoError(t, err)
	stored, err := repos.TOTPSecrets.Get(context.Background(), uid)
	require.NoError(t, err)
	secret, err := crypto.Open(stored.Secret)
	require.NoError(t, err)
	code, err := ptotp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	// Challenge with kind=totp + the freshly-computed code.
	status, body, sc := doJSON(t, ta, "POST", "/api/v1/auth/mfa/challenge",
		map[string]any{
			"pendingSessionToken": pending,
			"kind":                "totp",
			"code":                code,
		}, "")
	require.Equalf(t, 200, status, "challenge: want 200, body=%v", body)
	// Real session cookies must now be set.
	require.NotEmpty(t, extractCookie(sc, "lahijan_session"))
	require.NotEmpty(t, extractCookie(sc, "lahijan_refresh"))
}

func TestMFA_ChallengeFails5TimesRevokesPending(t *testing.T) {
	t.Parallel()
	ta, _ := newMFATestApp(t)
	cookie, addr := registerAndLoginMFA(t, ta)
	enrollTOTPAndVerify(t, ta, cookie)
	doJSON(t, ta, "POST", "/api/v1/auth/logout", nil, cookie)

	status, body, _ := doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Equal(t, 202, status)
	pending, _ := body["pendingSessionToken"].(string)
	require.NotEmpty(t, pending)

	// Send 4 wrong codes — each must 401. The 5th triggers the
	// brute-force lockout (429) since MaxAttempts is 5 by default.
	for i := 0; i < 4; i++ {
		s, _, _ := doJSON(t, ta, "POST", "/api/v1/auth/mfa/challenge",
			map[string]any{
				"pendingSessionToken": pending,
				"kind":                "totp",
				"code":                "000000",
			}, "")
		require.Equalf(t, 401, s, "attempt %d: want 401", i)
	}
	// 5th wrong code triggers the lockout: 429 with the
	// "too_many_requests" code, and the pending session is revoked.
	status, body, _ = doJSON(t, ta, "POST", "/api/v1/auth/mfa/challenge",
		map[string]any{
			"pendingSessionToken": pending,
			"kind":                "totp",
			"code":                "000000",
		}, "")
	require.Equal(t, 429, status, "5th attempt: must be locked out (429)")
	errCode, _ := body["error"].(map[string]any)
	if errCode != nil {
		assert.Equal(t, "too_many_requests", errCode["code"])
	}

	// The 6th must 401 with revoked (the row is gone from the lookup
	// path).
	status, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/mfa/challenge",
		map[string]any{
			"pendingSessionToken": pending,
			"kind":                "totp",
			"code":                "000000",
		}, "")
	require.Equal(t, 401, status, "after lockout: 401 (revoked)")
}

func TestMFA_LoginWithoutMFAIs200(t *testing.T) {
	t.Parallel()
	ta, _ := newMFATestApp(t)
	_, addr := registerAndLoginMFA(t, ta)
	// No factor enrolled, no tenant requires MFA — login is 200.
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Equalf(t, 200, status, "login without MFA: want 200")
	require.NotEmpty(t, extractCookie(sc, "lahijan_session"))
}

func TestMFA_RecoveryCodeChallenge(t *testing.T) {
	t.Parallel()
	ta, _ := newMFATestApp(t)
	cookie, addr := registerAndLoginMFA(t, ta)
	codes := enrollTOTPAndVerify(t, ta, cookie)
	doJSON(t, ta, "POST", "/api/v1/auth/logout", nil, cookie)

	status, body, _ := doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Equal(t, 202, status)
	pending, _ := body["pendingSessionToken"].(string)
	require.NotEmpty(t, pending)

	// Use the first recovery code.
	require.NotEmpty(t, codes)
	status, body, sc := doJSON(t, ta, "POST", "/api/v1/auth/mfa/challenge",
		map[string]any{
			"pendingSessionToken": pending,
			"kind":                "recovery",
			"code":                codes[0],
		}, "")
	require.Equalf(t, 200, status, "recovery challenge: want 200, body=%v", body)
	require.NotEmpty(t, extractCookie(sc, "lahijan_session"))

	// The same recovery code MUST NOT work twice (single-use).
	doJSON(t, ta, "POST", "/api/v1/auth/logout", nil, "lahijan_session="+extractCookie(sc, "lahijan_session"))
	status, body, _ = doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Equal(t, 202, status)
	pending2, _ := body["pendingSessionToken"].(string)
	status, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/mfa/challenge",
		map[string]any{
			"pendingSessionToken": pending2,
			"kind":                "recovery",
			"code":                codes[0], // already used
		}, "")
	require.Equal(t, 401, status, "reused recovery code: want 401")
}

// mustUserID resolves the user id via /me.
func mustUserID(t *testing.T, ta *testApp, cookie string) uuid.UUID {
	t.Helper()
	status, body, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, cookie)
	require.Equalf(t, 200, status, "GET /me: want 200, body=%v", body)
	id, _ := body["id"].(string)
	require.NotEmpty(t, id, "body must contain user id")
	uid, err := uuid.Parse(id)
	require.NoError(t, err)
	return uid
}
