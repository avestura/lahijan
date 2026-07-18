// idp_http_integration_test.go exercises the full /api/v1/auth/oauth/* and
// /api/v1/auth/oidc/* HTTP surface end to end against a real Postgres
// (testcontainers) plus the in-process fake OAuth + OIDC IdPs. It is the
// WS-07a DoD coverage for the user-facing endpoints:
//
//   - register a new account via OAuth (Google / GitHub preset path)
//   - register a new account via OIDC (test provider)
//   - link an existing account to an external IdP
//   - unlink an IdP (with the last-auth-method invariant)
//   - tokens encrypted at rest (DB row ciphertext != raw)
//   - every login emits an audit event

//go:build integration

package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	ofake "github.com/avestura/lahijan/internal/app/lahijan/auth/oauth/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
	oidcfake "github.com/avestura/lahijan/internal/app/lahijan/auth/oidc/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// newIDPTestApp extends newTestApp with the WS-07a stack wired against the
// in-process fake IdPs. Returns the app plus handles to the fakes so tests
// can flip their per-case state (AuthCode, ProfileResponse, etc.).
func newIDPTestApp(t *testing.T) (*testApp, *ofake.Server, *oidcfake.Server) {
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
		repos.Tokens, signer, audit.NoopEmitter{},
		pat.Config{Prefix: "lah_pat_", ByteLen: 32},
	)

	// Spin up the fake OAuth IdP + the fake OIDC IdP. The OIDC fake binds
	// discovery + JWKS at New(); closing them happens via t.Cleanup.
	oauthSrv := ofake.New()
	t.Cleanup(oauthSrv.Close)
	oidcSrv := oidcfake.New()
	t.Cleanup(oidcSrv.Close)

	stateSigner := state.NewSigner(signer)
	oauthP := oauth.NewGeneric(
		oauth.PresetConfig{
			Key: "fake", ClientID: "fake-id", ClientSecret: "fake-secret",
			RedirectURL: "https://app.test/api/v1/auth/oauth/fake/callback",
			Scopes:      []string{"openid", "email"},
		},
		stateSigner.Verify,
		oauth.PresetEndpoints{
			AuthURL: oauthSrv.AuthURL(), TokenURL: oauthSrv.TokenURL(), UserInfoURL: oauthSrv.UserInfoURL(),
		},
	)
	oauthReg := oauth.NewRegistry(oauthP)

	oidcP, err := oidc.NewProvider(context.Background(), oidc.ProviderConfig{
		Key: "fakeoidc", Issuer: oidcSrv.Issuer,
		ClientID: "oidc-client-id", ClientSecret: "oidc-client-secret",
		RedirectURL: "https://app.test/api/v1/auth/oidc/fakeoidc/callback",
		Scopes:      []string{"openid", "email", "profile"},
	}, stateSigner.Verify)
	require.NoError(t, err, "oidc discovery against fake must succeed")
	oidcReg := oidc.NewRegistry(oidcP)

	encKey := sha256.Sum256([]byte("http-test-encryption-key"))
	crypto, err := secrets.NewCrypto(encKey[:])
	require.NoError(t, err)
	idpSvc := idp.New(repos, crypto, audit.NewDBEmitter(repos.AuditLog), &sessionOpenerAdapter{svc: sessionSvc})

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
		IDPSvc:       idpSvc,
		IDPOAuth:     oauthReg,
		IDPOIDC:      oidcReg,
		StateSigner:  stateSigner,
	})
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(repos.Memberships))
	app := fiberNew()
	middleware.Apply(app, middleware.Options{
		Tenant: middleware.TenantWithResolver(middleware.TenantResolver{Tenants: repos.Tenants}),
		Auth: middleware.AuthWithResolver(middleware.AuthResolver{
			Cookies: middleware.CookieConfig{
				SessionName: cookies.SessionName, RefreshName: cookies.RefreshName,
			},
			Signer:       signer,
			SessionsRepo: repos.Sessions,
			PATService:   patSvc,
		}),
	})
	api.RegisterRoutes(app, server, policy)
	return &testApp{app: app, cookies: cookies, repos: repos}, oauthSrv, oidcSrv
}

// sessionOpenerAdapter is the same adapter program.go uses; duplicated here
// so the test does not depend on the program package (which has no test
// surface today). It is intentionally NOT an exported type — the program.go
// adapter is the canonical implementation; this is just for tests.
type sessionOpenerAdapter struct {
	svc *session.Service
}

// OpenForExistingUser implements idp.SessionOpener.
func (a *sessionOpenerAdapter) OpenForExistingUser(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (idp.SessionOpen, error) {
	sess, err := a.svc.OpenForExistingUser(ctx, userID, ua, ip)
	if err != nil {
		return idp.SessionOpen{}, err
	}
	return idp.SessionOpen{
		UserID:      sess.UserID,
		SessionID:   sess.SessionID,
		ExpiresAt:   sess.ExpiresAt,
		CookieValue: sess.CookieValue,
		Refresh: idp.RefreshIssue{
			Raw:       sess.Refresh.Raw,
			FamilyID:  sess.Refresh.FamilyID,
			ExpiresAt: sess.Refresh.ExpiresAt,
		},
	}, nil
}

// fiberNew constructs a fresh *fiber.App. Mirrors the production setup minus
// the ErrorHandler (which is exercised through api.RegisterRoutes).
func fiberNew() *fiber.App {
	return fiber.New()
}

// driveOAuthFlow simulates the browser: hits /start, captures the redirect,
// then calls /callback with the captured state + the fake's AuthCode.
//
// Returns the callback response status + body + Set-Cookie header.
func driveOAuthFlow(t *testing.T, ta *testApp, provider string) (int, map[string]any, string) {
	t.Helper()
	// 1) Start: GET /start, capture state + PKCE cookies.
	startResp := httptestGET(t, ta, "/api/v1/auth/oauth/"+provider+"/start", "")
	require.Equal(t, 302, startResp.Code, "start must 302 to the IdP")
	stateCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_state")
	pkceCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_pkce")
	require.NotEmpty(t, stateCookie, "start must set the state cookie")
	require.NotEmpty(t, pkceCookie, "start must set the PKCE cookie")

	// Pull the state token out of the start redirect Location so the
	// callback receives the same value the IdP would have echoed back.
	stateToken := extractQueryParam(startResp.Location, "state")
	require.NotEmpty(t, stateToken, "start Location must carry the state token")

	// 2) Drive the IdP /auth so the fake records the code. Use a real HTTP
	// client because ta.app.Test only routes through the local fiber app.
	driveIDPAuth(t, startResp.Location)

	// 3) Callback: GET /callback?code=...&state=...  with the cookies.
	cbPath := "/api/v1/auth/oauth/" + provider + "/callback?code=fake-auth-code&state=" + stateToken
	cookieHdr := "lahijan_oauth_state=" + stateCookie + "; lahijan_oauth_pkce=" + pkceCookie
	return doGET(t, ta, cbPath, cookieHdr)
}

// driveOIDCFlow mirrors driveOAuthFlow for the OIDC path; it carries the
// extra nonce cookie.
func driveOIDCFlow(t *testing.T, ta *testApp, provider string) (int, map[string]any, string) {
	t.Helper()
	startResp := httptestGET(t, ta, "/api/v1/auth/oidc/"+provider+"/start", "")
	require.Equal(t, 302, startResp.Code, "oidc start must 302 to the IdP")
	stateCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_state")
	pkceCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_pkce")
	nonceCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oidc_nonce")
	require.NotEmpty(t, stateCookie)
	require.NotEmpty(t, pkceCookie)
	require.NotEmpty(t, nonceCookie, "oidc start must set the nonce cookie")

	// Actually hit the IdP /auth URL so the fake captures the nonce. We use
	// http.DefaultClient because ta.app.Test routes through the local fiber
	// app, which has no route for the IdP URL.
	driveIDPAuth(t, startResp.Location)

	stateToken := extractQueryParam(startResp.Location, "state")
	cbPath := "/api/v1/auth/oidc/" + provider + "/callback?code=fake-oidc-code&state=" + stateToken
	cookieHdr := strings.Join([]string{
		"lahijan_oauth_state=" + stateCookie,
		"lahijan_oauth_pkce=" + pkceCookie,
		"lahijan_oidc_nonce=" + nonceCookie,
	}, "; ")
	return doGET(t, ta, cbPath, cookieHdr)
}

// driveIDPAuth GETs the IdP /auth URL via an HTTP client that does NOT
// follow redirects. The fake's /auth handler returns a 302 to the (fake)
// redirect_uri; we want to drive the /auth side-effect (nonce capture)
// without following the redirect to a URL that does not exist locally.
func driveIDPAuth(t *testing.T, idpAuthURL string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(idpAuthURL) //nolint:gosec // test-only; URL comes from our own fake
	require.NoError(t, err, "drive IdP /auth")
	_ = resp.Body.Close()
}

func TestOAuth_Login_NewUser_CreatesAccountAndSession(t *testing.T) {
	t.Parallel()
	ta, oauthSrv, _ := newIDPTestApp(t)
	oauthSrv.ProfileResponse = ofake.Profile{
		Sub: "google-sub-1", Email: "oauth1+" + unique() + "@example.test",
		EmailVerified: true, Name: "OAuth User",
	}

	status, _, sc := driveOAuthFlow(t, ta, "fake")
	require.Equal(t, 302, status, "callback must 302 to the dashboard")
	sessionCookie := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sessionCookie, "session cookie must be set after a new-user login")

	// The session cookie must authenticate /me.
	meStatus, _, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, "lahijan_session="+sessionCookie)
	require.Equal(t, 200, meStatus, "session cookie must work for /me")
}

func TestOIDC_Login_NewUser_CreatesAccountAndSession(t *testing.T) {
	t.Parallel()
	ta, _, oidcSrv := newIDPTestApp(t)
	oidcSrv.Subject = "oidc-sub-" + unique()
	oidcSrv.Email = "oidc1+" + unique() + "@example.test"
	oidcSrv.EmailVerified = true
	oidcSrv.Name = "OIDC User"

	status, _, sc := driveOIDCFlow(t, ta, "fakeoidc")
	require.Equal(t, 302, status, "oidc callback must 302")
	require.NotEmpty(t, extractCookie(sc, "lahijan_session"), "session cookie must be set")
}

func TestOAuth_Login_EmitsAuditEvent(t *testing.T) {
	t.Parallel()
	ta, oauthSrv, _ := newIDPTestApp(t)
	oauthSrv.ProfileResponse = ofake.Profile{
		Sub: "audit-sub", Email: "audit+" + unique() + "@example.test",
		EmailVerified: true, Name: "Audit User",
	}

	_, _, sc := driveOAuthFlow(t, ta, "fake")
	sessionCookie := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sessionCookie)

	meStatus, body, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, "lahijan_session="+sessionCookie)
	require.Equal(t, 200, meStatus)
	uid, _ := body["id"].(string)
	require.NotEmpty(t, uid)

	// Assert the audit log contains an auth.idp.login event for this user.
	rows, err := ta.repos.AuditLog.ListGlobal(context.Background(), 200, 0)
	require.NoError(t, err)
	var sawLogin bool
	for _, r := range rows {
		if r.Action == audit.ActionIdpLogin && r.ActorUserID != nil && r.ActorUserID.String() == uid {
			sawLogin = true
			break
		}
	}
	assert.True(t, sawLogin, "an audit.action_auth_idp_login row must exist for the new user")
}

func TestOAuth_TokensAreEncryptedAtRest(t *testing.T) {
	t.Parallel()
	ta, oauthSrv, _ := newIDPTestApp(t)
	oauthSrv.ProfileResponse = ofake.Profile{
		Sub: "enc-sub", Email: "enc+" + unique() + "@example.test",
		EmailVerified: true, Name: "Enc User",
	}
	oauthSrv.AccessToken = "plaintext-access-token-12345"
	oauthSrv.RefreshToken = "plaintext-refresh-token-12345"

	_, _, sc := driveOAuthFlow(t, ta, "fake")
	require.NotEmpty(t, extractCookie(sc, "lahijan_session"))

	// Pull the identity row directly; the access_token column must NOT
	// contain the raw plaintext token.
	row, err := ta.repos.OAuthIdentities.GetByProviderSubject(context.Background(), "fake", "enc-sub")
	require.NoError(t, err)
	require.NotNil(t, row.AccessToken)
	assert.NotContains(t, *row.AccessToken, "plaintext-access-token-12345",
		"the stored access_token must be the AES-GCM ciphertext, not the raw")
	assert.NotContains(t, *row.RefreshToken, "plaintext-refresh-token-12345")
}

func TestListIdentities_ReturnsLinkedIdentities(t *testing.T) {
	t.Parallel()
	ta, oauthSrv, _ := newIDPTestApp(t)
	// Each test must use a UNIQUE subject so parallel runs do not collide on
	// the (provider, subject) unique constraint in the shared test DB.
	oauthSrv.ProfileResponse = ofake.Profile{
		Sub: "list-sub-" + unique(), Email: "list+" + unique() + "@example.test",
		EmailVerified: true, Name: "List User",
	}
	// Register a password user; then link a fake IdP identity via the OAuth
	// flow while logged in.
	addr := "idp-list+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	sessionCookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	startResp := httptestGET(t, ta, "/api/v1/auth/oauth/fake/start", sessionCookie)
	require.Equal(t, 302, startResp.Code)
	stateCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_state")
	pkceCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_pkce")
	linkCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_link_uid")
	require.NotEmpty(t, stateCookie)
	require.NotEmpty(t, linkCookie, "start while logged in must set the link_uid cookie")
	stateToken := extractQueryParam(startResp.Location, "state")
	_, _ = ta.app.Test(httptest.NewRequest("GET", startResp.Location, nil), -1)
	cbPath := "/api/v1/auth/oauth/fake/callback?code=fake-auth-code&state=" + stateToken
	cookieHdr := sessionCookie + "; lahijan_oauth_state=" + stateCookie + "; lahijan_oauth_pkce=" + pkceCookie + "; lahijan_link_uid=" + linkCookie
	status, _, _ := doGET(t, ta, cbPath, cookieHdr)
	require.Equal(t, 302, status)

	// List the identities; the linked identity must appear.
	listStatus, listBody, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus, "list must succeed")
	require.Len(t, listBody, 1, "the user must have exactly one linked identity")
	row, _ := listBody[0].(map[string]any)
	assert.Equal(t, "fake", row["provider"])
}

func TestUnlink_LastAuthMethod_Rejected(t *testing.T) {
	t.Parallel()
	ta, _, _ := newIDPTestApp(t)
	// OAuth-only user (no password); the unlink must be rejected.
	_, _, sc := driveOAuthFlow(t, ta, "fake")
	sessionCookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	listStatus, listBody, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus)
	arr := listBody
	require.Len(t, arr, 1)
	id, _ := arr[0].(map[string]any)["id"].(string)
	require.NotEmpty(t, id)

	status, body, _ := doJSON(t, ta, "DELETE", "/api/v1/me/identities/"+id, nil, sessionCookie)
	require.Equal(t, 409, status, "unlink last auth method must be 409")
	errMsg, _ := body["error"].(map[string]any)
	assert.NotEmpty(t, errMsg["message"], "the envelope must carry a localised message")
}

func TestUnlink_WithPassword_Succeeds(t *testing.T) {
	t.Parallel()
	ta, oauthSrv, _ := newIDPTestApp(t)
	oauthSrv.ProfileResponse = ofake.Profile{
		Sub: "ul-sub-" + unique(), Email: "ul+" + unique() + "@example.test",
		EmailVerified: true, Name: "UL User",
	}
	addr := "ul+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	sessionCookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	startResp := httptestGET(t, ta, "/api/v1/auth/oauth/fake/start", sessionCookie)
	require.Equal(t, 302, startResp.Code)
	stateCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_state")
	pkceCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_pkce")
	linkCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_link_uid")
	require.NotEmpty(t, stateCookie)
	require.NotEmpty(t, linkCookie)
	stateToken := extractQueryParam(startResp.Location, "state")
	_, _ = ta.app.Test(httptest.NewRequest("GET", startResp.Location, nil), -1)
	cbPath := "/api/v1/auth/oauth/fake/callback?code=fake-auth-code&state=" + stateToken
	cookieHdr := sessionCookie + "; lahijan_oauth_state=" + stateCookie + "; lahijan_oauth_pkce=" + pkceCookie + "; lahijan_link_uid=" + linkCookie
	status, _, _ := doGET(t, ta, cbPath, cookieHdr)
	require.Equal(t, 302, status)

	listStatus, listBody, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus)
	require.Len(t, listBody, 1)
	id, _ := listBody[0].(map[string]any)["id"].(string)

	status, _, _ = doJSON(t, ta, "DELETE", "/api/v1/me/identities/"+id, nil, sessionCookie)
	require.Equal(t, 200, status, "unlink must succeed when the user has a password")

	listStatus2, listBody2, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus2)
	assert.Empty(t, listBody2)
}

func TestStartOAuth_UnknownProvider_404(t *testing.T) {
	t.Parallel()
	ta, _, _ := newIDPTestApp(t)
	status, _, _ := doGET(t, ta, "/api/v1/auth/oauth/does-not-exist/start", "")
	require.Equal(t, 404, status, "unknown provider must 404")
}

func TestCallbackOAuth_BadState_400(t *testing.T) {
	t.Parallel()
	ta, _, _ := newIDPTestApp(t)
	status, _, _ := doGET(t, ta, "/api/v1/auth/oauth/fake/callback?code=x&state=tampered", "")
	require.Equal(t, 400, status, "bad state must 400")
}

// httptestResult is a tiny wrapper around *http.Response that exposes the
// fields the tests need (status + Set-Cookie + Location). The standard
// httptest.ResponseRecorder is not returned by fiber.App.Test, which returns
// *http.Response instead; this struct normalises the surface.
type httptestResult struct {
	Code     int
	Header   httpHeader
	Location string
}

// httpHeader is a thin alias around http.Header for the helper signature.
type httpHeader = map[string][]string

// httptestGET issues a GET and returns the wrapper result.
func httptestGET(t *testing.T, ta *testApp, path, cookieHdr string) *httptestResult {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	resp, err := ta.app.Test(req, -1)
	requireNoErr(t, err)
	defer resp.Body.Close()
	out := &httptestResult{
		Code:     resp.StatusCode,
		Header:   map[string][]string{},
		Location: resp.Header.Get("Location"),
	}
	for k, vs := range resp.Header {
		out.Header[k] = vs
	}
	return out
}

// doGET is the GET-only variant of doJSON; no body to encode/decode.
func doGET(t *testing.T, ta *testApp, path, cookieHdr string) (int, map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	resp, err := ta.app.Test(req, -1)
	requireNoErr(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, strings.Join(resp.Header["Set-Cookie"], "; ")
}

// doGETArr mirrors doGET but decodes the body as a JSON array. Used by the
// /me/identities tests where the response is a top-level array, not an object.
func doGETArr(t *testing.T, ta *testApp, path, cookieHdr string) (int, []any, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if cookieHdr != "" {
		req.Header.Set("Cookie", cookieHdr)
	}
	resp, err := ta.app.Test(req, -1)
	requireNoErr(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out []any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, strings.Join(resp.Header["Set-Cookie"], "; ")
}

// extractSetCookie scans the Set-Cookie header slice for one carrying the
// given name and returns its value (everything after name= up to the first ;).
func extractSetCookie(headers []string, name string) string {
	for _, h := range headers {
		for _, part := range strings.Split(h, ";") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, name+"=") {
				return strings.TrimPrefix(part, name+"=")
			}
		}
	}
	return ""
}

// extractQueryParam pulls a single value off a URL's query string.
func extractQueryParam(rawURL, key string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		for _, pair := range strings.Split(rawURL[i+1:], "&") {
			if eq := strings.IndexByte(pair, '='); eq > 0 && pair[:eq] == key {
				return pair[eq+1:]
			}
		}
	}
	return ""
}
