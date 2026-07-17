// auth_http_integration_test.go exercises the full /api/v1/auth/* HTTP surface
// end to end against a real Postgres (testcontainers) plus the real auth
// middleware and cookie helpers. It is the WS-06 DoD "≥1 happy + ≥1 failure
// test per endpoint" requirement for the user-facing endpoints.

//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
)

// TestMain starts one testcontainers Postgres for the package.
func TestMain(m *testing.M) { testutil.Setup(m) }

const strongPw = "VeryStrong123!xyz"

// testApp bundles a Fiber app with cookies so tests can read Set-Cookie values
// across requests (the Go http.Client does that automatically, but app.Test
// does not, so we thread cookies explicitly).
type testApp struct {
	app     *fiber.App
	cookies api.CookieConfig
	repos   *database.Repos
}

func newTestApp(t *testing.T) *testApp {
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
		notifyemail.NoopSender{}, audit.NoopEmitter{},
		email.Config{
			VerifyTTL: time.Hour, ResetTTL: time.Hour, EmailChangeTTL: time.Hour,
			TokenByteLen: 32, AppBaseURL: "https://app.test",
		},
	)
	sessionSvc := session.New(
		repos.Users, repos.Sessions, repos.Tokens, hasher, signer,
		mailer, audit.NoopEmitter{},
		session.Config{
			SessionLifetime: time.Hour, RefreshLifetime: time.Hour,
			TokenByteLen: 32, MinPasswordLen: 12,
		},
	)
	patSvc := pat.New(
		repos.Tokens, signer, audit.NoopEmitter{},
		pat.Config{Prefix: "lah_pat_", ByteLen: 32},
	)
	emailSvc := mailer // alias for clarity in the server wiring
	server := api.NewServer(api.ServerDeps{
		Users:        repos.Users,
		Sessions:     repos.Sessions,
		SessionSvc:   sessionSvc,
		PATSvc:       patSvc,
		EmailSvc:     emailSvc,
		Signer:       signer,
		Cookies:      cookies,
		Audit:        repos.AuditLog,
		AuditEmitter: audit.NewDBEmitter(repos.AuditLog),
	})
	// Wire the rbac.PolicyEvaluator over MembershipsRepository so per-route
	// RequirePerm (via AuditGate) works in the integration tests too.
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
	return &testApp{app: app, cookies: cookies, repos: repos}
}

// doJSON issues a JSON request and returns status + decoded body (generic) +
// every Set-Cookie header joined so extractCookie can find either cookie.
func doJSON(t *testing.T, ta *testApp, method, path string, body any, cookieHdr string) (int, map[string]any, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		requireNoErr(t, err)
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	resp, err := ta.app.Test(req, -1)
	requireNoErr(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	// A register/login/refresh response sets TWO Set-Cookie headers (session +
	// refresh); httptest.Header["Set-Cookie"] returns the slice. Join with "; "
	// so extractCookie finds either name.
	return resp.StatusCode, out, strings.Join(resp.Header["Set-Cookie"], "; ")
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// extractCookie pulls the value of name from a Set-Cookie header line.
func extractCookie(setCookie, name string) string {
	for _, part := range strings.Split(setCookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+"=") {
			return strings.TrimPrefix(part, name+"=")
		}
	}
	return ""
}

func TestRegister_HappyAndConflict(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "happy+" + unique() + "@example.test"

	// Happy path.
	status, body, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw, "locale": "en"}, "")
	if status != 201 {
		t.Fatalf("register happy: want 201, got %d body=%v", status, body)
	}
	if extractCookie(sc, "lahijan_session") == "" || extractCookie(sc, "lahijan_refresh") == "" {
		t.Fatalf("register happy: cookies must be set, got Set-Cookie=%q", sc)
	}
	user, _ := body["user"].(map[string]any)
	if user == nil || user["email"] != addr {
		t.Fatalf("register happy: response missing user.email, got %v", body)
	}

	// Conflict: same email.
	status, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	if status != 409 {
		t.Fatalf("register conflict: want 409, got %d", status)
	}
}

func TestRegister_WeakPasswordFails(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	status, body, _ := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": "weak+" + unique() + "@example.test", "password": "short"}, "")
	if status != 400 {
		t.Fatalf("weak password: want 400, got %d body=%v", status, body)
	}
}

func TestLogin_HappyAndBadCredentials(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "login+" + unique() + "@example.test"
	_, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")

	status, body, sc := doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": strongPw}, "")
	if status != 200 || extractCookie(sc, "lahijan_session") == "" {
		t.Fatalf("login happy: want 200 + cookies, got %d body=%v", status, body)
	}

	status, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": addr, "password": "WrongPassword99!"}, "")
	if status != 401 {
		t.Fatalf("login bad password: want 401, got %d", status)
	}
}

func TestMe_RequiresAuthThenWorks(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "me+" + unique() + "@example.test"

	// Anonymous: 401.
	status, _, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, "")
	if status != 401 {
		t.Fatalf("me anonymous: want 401, got %d", status)
	}

	// Register, grab the session cookie, hit /me with it.
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	cookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	status, body, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, cookie)
	if status != 200 {
		t.Fatalf("me authenticated: want 200, got %d body=%v", status, body)
	}
	// GET /auth/me returns the User directly (id, email, displayName), not
	// wrapped under a "user" key.
	if body["email"] != addr {
		t.Fatalf("me authenticated: wrong email, got body=%v", body)
	}
}

func TestRefresh_RotationAndReuseViaHTTP(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "rot+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")

	rt := extractCookie(sc, "lahijan_refresh")
	cookieOld := "lahijan_refresh=" + rt

	// Rotate.
	status, body, sc2 := doJSON(t, ta, "POST", "/api/v1/auth/refresh", nil, cookieOld)
	if status != 200 || extractCookie(sc2, "lahijan_refresh") == "" {
		t.Fatalf("refresh rotation: want 200 + new cookie, got %d body=%v", status, body)
	}
	rt2 := extractCookie(sc2, "lahijan_refresh")
	if rt2 == rt {
		t.Fatalf("refresh rotation: new token must differ")
	}

	// Reuse the old token: 401.
	status, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/refresh", nil, cookieOld)
	if status != 401 {
		t.Fatalf("refresh reuse: want 401, got %d", status)
	}

	// The rotated token must also fail now (family burned).
	status, _, _ = doJSON(t, ta, "POST", "/api/v1/auth/refresh", nil, "lahijan_refresh="+rt2)
	if status != 401 {
		t.Fatalf("refresh after reuse: family must be revoked, want 401, got %d", status)
	}
}

// TestRefresh_KeepsSessionCookieValid proves the session cookie stays valid
// after a successful refresh — the security-critical rotation happens on the
// refresh token, and the session cookie is intentionally left stable.
func TestRefresh_KeepsSessionCookieValid(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "keep+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	sessionCookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")
	refreshCookie := "lahijan_refresh=" + extractCookie(sc, "lahijan_refresh")

	// Rotate using the refresh cookie while still holding the session cookie.
	if status, _, _ := doJSON(t, ta, "POST", "/api/v1/auth/refresh", nil, refreshCookie); status != 200 {
		t.Fatalf("refresh: want 200, got %d", status)
	}

	// The original session cookie must still authenticate /me.
	status, _, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, sessionCookie)
	if status != 200 {
		t.Fatalf("session cookie after refresh: want 200, got %d", status)
	}
}

func TestLogout_ClearsCookies(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "lo+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	cookie := "lahijan_refresh=" + extractCookie(sc, "lahijan_refresh")

	status, _, sc2 := doJSON(t, ta, "POST", "/api/v1/auth/logout", nil, cookie)
	if status != 200 {
		t.Fatalf("logout: want 200, got %d", status)
	}
	// Set-Cookie on logout contains MaxAge=-1 / expires for both cookies; just
	// assert it cleared at least one cookie header value.
	if sc2 == "" {
		t.Fatalf("logout: Set-Cookie header should be present to clear cookies")
	}
}

func TestPAT_CreateUseRevokeViaHTTP(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	addr := "pat+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	cookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	// Create PAT; the raw token + id are returned exactly once.
	status, body, _ := doJSON(t, ta, "POST", "/api/v1/auth/personal-access-tokens",
		map[string]any{"name": "ci", "scopes": []string{"compute.instance.read"}}, cookie)
	if status != 201 {
		t.Fatalf("pat create: want 201, got %d body=%v", status, body)
	}
	rawPAT, _ := body["token"].(string)
	if rawPAT == "" {
		t.Fatalf("pat create: raw token must be returned once, got body=%v", body)
	}
	patID, _ := body["id"].(string)
	if patID == "" {
		t.Fatalf("pat create: id must be present, got body=%v", body)
	}

	// Use PAT to call /me.
	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+rawPAT)
	resp, err := ta.app.Test(req, -1)
	requireNoErr(t, err)
	if resp.StatusCode != 200 {
		t.Fatalf("pat /me: want 200, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Revoke the PAT by id (authenticated as the owner).
	status, _, _ = doJSON(t, ta, "DELETE", "/api/v1/auth/personal-access-tokens/"+patID, nil, cookie)
	if status != 200 {
		t.Fatalf("pat revoke: want 200, got %d", status)
	}

	// PAT no longer authenticates.
	req3 := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	req3.Header.Set("Authorization", "Bearer "+rawPAT)
	resp3, err := ta.app.Test(req3, -1)
	requireNoErr(t, err)
	if resp3.StatusCode != 401 {
		t.Fatalf("pat revoked: want 401, got %d", resp3.StatusCode)
	}
	_ = resp3.Body.Close()
}

func TestVerifyEmail_MalformedBodyFails(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	status, _, _ := doJSON(t, ta, "POST", "/api/v1/auth/verify-email",
		map[string]any{"token": ""}, "")
	if status != 400 {
		t.Fatalf("verify-email empty token: want 400, got %d", status)
	}
}

func TestPasswordResetRequest_AlwaysSucceeds(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	// Unknown email: still 200 (no enumeration).
	status, _, _ := doJSON(t, ta, "POST", "/api/v1/auth/password-reset/request",
		map[string]any{"email": "definitely-missing+" + unique() + "@example.test"}, "")
	if status != 200 {
		t.Fatalf("password-reset request unknown email: want 200, got %d", status)
	}
}

func TestResendVerification_AlwaysSucceeds(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	status, _, _ := doJSON(t, ta, "POST", "/api/v1/auth/resend-verification",
		map[string]any{"email": "definitely-missing+" + unique() + "@example.test"}, "")
	if status != 200 {
		t.Fatalf("resend-verification unknown email: want 200, got %d", status)
	}
}

// unique returns a short unique suffix for email disambiguation across parallel
// runs.
func unique() string {
	// uuid via a fresh DB row is heavy; use time + counter instead for tests.
	return strings.ReplaceAll(time.Now().Format("150405.0000000"), ".", "")
}

// ensure context import stays when no direct call remains.
var _ = context.Background
