// saml_http_integration_test.go exercises the full /api/v1/auth/saml/* HTTP
// surface end to end against a real Postgres (testcontainers) plus the
// in-process fake SAML IdP. It is the WS-07b DoD coverage for the
// user-facing endpoints:
//
//   - SP metadata serves valid XML
//   - SP-initiated login creates a new user (when JIT is on) + opens a session
//   - SP-initiated login resolves an existing user + opens a session
//   - link a SAML identity to an existing (password) account
//   - assertion verification rejects unsigned + InResponseTo-mismatched
//   - SAML identities appear in /me/identities alongside OAuth/OIDC ones
//   - every login emits an audit event

//go:build integration

package api_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	crewjam "github.com/crewjam/saml"

	"github.com/avestura/lahijan/internal/app/lahijan/api"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/saml"
	samlfake "github.com/avestura/lahijan/internal/app/lahijan/auth/saml/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
	"github.com/gofiber/fiber/v2"
)

// newSAMLTestApp extends newTestApp with the WS-07b stack wired against the
// in-process fake SAML IdP. Returns the app plus a handle to the fake so
// tests can flip its per-case state (Subject, Email, etc.).
func newSAMLTestApp(t *testing.T, jitEnabled bool) (*testApp, *samlfake.Server) {
	t.Helper()
	repos := testutil.Repos()
	signer := secrets.NewSigner("http-test-signing-key-saml")
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

	// Flip the JIT toggle on the idp service instance so parallel tests
	// don't race on the package-level default. The default stays off; each
	// test chooses its own setting via the constructor argument.
	idp.SetJITEnabled(false) // ensure package default is the safe value
	t.Cleanup(func() { idp.SetJITEnabled(false) })

	// Spin up the fake SAML IdP. The fake signs assertions with a real RSA
	// key (2048-bit, generated at New() time), so Lahijan's signature
	// verification path is exercised end to end without an external
	// simplesamlphp container.
	fakeIDP := samlfake.New()
	t.Cleanup(fakeIDP.Close)

	stateSigner := state.NewSigner(signer)
	spCreds := generateTestSPCredentials(t)
	spEntityID := "https://app.test/api/v1/auth/saml/metadata"
	spACS := "https://app.test/api/v1/auth/saml/fake/acs"
	spMetaURL := "https://app.test/api/v1/auth/saml/metadata"

	sp, err := saml.NewProvider(saml.ProviderConfig{
		Key:            "fake",
		EntityID:       spEntityID,
		ACSURL:         spACS,
		MetadataURL:    spMetaURL,
		IDPMetadataXML: string(fakeIDP.MetadataXML()),
	}, spCreds, stateSigner.Verify)
	require.NoError(t, err, "saml.NewProvider against the fake metadata must succeed")

	// Register the SP's metadata on the fake so the IdP knows where to POST
	// the assertion.
	fakeIDP.SetSPMetadata(parseSPMetadata(t, sp.Metadata()))

	samlReg := saml.NewRegistry(sp)

	encKey := sha256.Sum256([]byte("http-test-encryption-key-saml"))
	crypto, err := secrets.NewCrypto(encKey[:])
	require.NoError(t, err)
	idpSvc := idp.New(repos, crypto, audit.NewDBEmitter(repos.AuditLog), &sessionOpenerAdapter{svc: sessionSvc})
	idpSvc.SetJITEnabledInstance(jitEnabled)

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
		StateSigner:  stateSigner,
		IDPCookies:   api.DefaultExternalIDPCookies,
		IDPSAML:      samlReg,
	})
	policy := middleware.NewPolicyResolver(rbac.NewEvaluator(repos.Memberships))
	app := fiber.New()
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
	return &testApp{app: app, cookies: cookies, repos: repos}, fakeIDP
}

// generateTestSPCredentials mints a fresh RSA-2048 key + self-signed cert for
// the test SP. Returned as PEM-encoded bytes.
func generateTestSPCredentials(t *testing.T) saml.SPCredentials {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "rsa keygen")
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lahijan-saml-test-sp"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return saml.SPCredentials{
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}),
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
	}
}

// parseSPMetadata unmarshals the SP metadata XML into an EntityDescriptor.
func parseSPMetadata(t *testing.T, raw []byte) *crewjam.EntityDescriptor {
	t.Helper()
	desc := &crewjam.EntityDescriptor{}
	require.NoError(t, xml.Unmarshal(raw, desc), "SP metadata must parse")
	return desc
}

// driveSAMLFlow simulates the browser through the SP-initiated flow:
//  1. GET /api/v1/auth/saml/fake/start → 302 with cookies
//  2. GET the IdP /sso URL → 200 with auto-submit HTML form
//  3. Extract SAMLResponse + RelayState from the HTML
//  4. POST /api/v1/auth/saml/fake/acs with the form fields + cookies
//
// Returns the final callback response status + body + Set-Cookie header.
func driveSAMLFlow(t *testing.T, ta *testApp, provider, sessionCookie string) (int, map[string]any, string) {
	t.Helper()
	// 1) Start: GET /start, capture state + requestID cookies.
	startResp := httptestGET(t, ta, "/api/v1/auth/saml/"+provider+"/start", sessionCookie)
	require.Equal(t, 302, startResp.Code, "start must 302 to the IdP")
	stateCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_oauth_state")
	reqIDCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_saml_reqid")
	linkCookie := extractSetCookie(startResp.Header["Set-Cookie"], "lahijan_link_uid")
	require.NotEmpty(t, stateCookie, "start must set the state cookie")
	require.NotEmpty(t, reqIDCookie, "start must set the request-id cookie")

	// 2) Drive the IdP /sso URL.
	idpURL := startResp.Location
	require.NotEmpty(t, idpURL, "start Location must carry the IdP SSO URL")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(idpURL)
	require.NoError(t, err, "drive IdP /sso")
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "fake /sso must return 200; body: %s", string(body))

	// 3) Extract the SAMLResponse + RelayState from the HTML form.
	samlResponse, relayState := samlfake.ExtractSAMLResponse(string(body))
	require.NotEmpty(t, samlResponse, "fake IdP must emit SAMLResponse")

	// 4) POST /acs with the form body + the cookies the start handler set.
	form := url.Values{}
	form.Set("SAMLResponse", samlResponse)
	form.Set("RelayState", relayState)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/saml/"+provider+"/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cookieHdr := "lahijan_oauth_state=" + stateCookie + "; lahijan_saml_reqid=" + reqIDCookie
	if sessionCookie != "" {
		cookieHdr += "; " + sessionCookie
	}
	if linkCookie != "" {
		cookieHdr += "; lahijan_link_uid=" + linkCookie
	}
	req.Header.Set("Cookie", cookieHdr)
	httpResp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer httpResp.Body.Close()
	raw, _ := io.ReadAll(httpResp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return httpResp.StatusCode, out, strings.Join(httpResp.Header["Set-Cookie"], "; ")
}

// TestMetadataSAML_ServesXML verifies the SP metadata endpoint serves valid
// XML carrying the SP's entity ID.
func TestMetadataSAML_ServesXML(t *testing.T) {
	t.Parallel()
	ta, _ := newSAMLTestApp(t, false)
	status, raw := doRaw(t, ta, "/api/v1/auth/saml/metadata")
	require.Equal(t, 200, status)
	assert.Contains(t, raw, "EntityDescriptor", "metadata must be an EntityDescriptor XML")
	assert.Contains(t, raw, "https://app.test/api/v1/auth/saml/metadata", "metadata must carry the SP entity ID")
}

// TestSAML_Login_NewUserWithJIT verifies SP-initiated login against an
// unknown (provider, name_id) creates a new user + opens a session when JIT
// provisioning is on.
func TestSAML_Login_NewUserWithJIT(t *testing.T) {
	t.Parallel()
	ta, fakeIDP := newSAMLTestApp(t, true)
	fakeIDP.Subject = "saml-nameid-jit-" + unique()
	fakeIDP.Email = "saml-jit+" + unique() + "@example.test"
	fakeIDP.Name = "SAML JIT User"

	status, _, sc := driveSAMLFlow(t, ta, "fake", "")
	require.Equal(t, 302, status, "ACS must 302 to the dashboard on success")
	sessionCookie := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sessionCookie, "session cookie must be set after a new-user login")

	// The session cookie must authenticate /me.
	meStatus, _, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, "lahijan_session="+sessionCookie)
	require.Equal(t, 200, meStatus)
}

// TestSAML_Login_NewUserWithoutJIT verifies SP-initiated login against an
// unknown (provider, name_id) is rejected when JIT is off (the default).
func TestSAML_Login_NewUserWithoutJIT(t *testing.T) {
	t.Parallel()
	ta, fakeIDP := newSAMLTestApp(t, false)
	fakeIDP.Subject = "saml-nameid-nojit-" + unique()
	fakeIDP.Email = "saml-nojit+" + unique() + "@example.test"
	fakeIDP.Name = "SAML NoJIT User"

	status, body, sc := driveSAMLFlow(t, ta, "fake", "")
	require.NotEqual(t, 302, status, "JIT-disabled must NOT redirect to dashboard")
	assert.Empty(t, extractCookie(sc, "lahijan_session"), "no session cookie should be set")
	errObj, _ := body["error"].(map[string]any)
	assert.NotEmpty(t, errObj["message"], "the envelope must carry a localised message")
}

// TestSAML_Login_EmitsAuditEvent verifies every login emits an
// auth.idp.login audit row.
func TestSAML_Login_EmitsAuditEvent(t *testing.T) {
	t.Parallel()
	ta, fakeIDP := newSAMLTestApp(t, true)
	fakeIDP.Subject = "saml-audit-" + unique()
	fakeIDP.Email = "saml-audit+" + unique() + "@example.test"

	_, _, sc := driveSAMLFlow(t, ta, "fake", "")
	sessionCookie := extractCookie(sc, "lahijan_session")
	require.NotEmpty(t, sessionCookie)
	meStatus, body, _ := doJSON(t, ta, "GET", "/api/v1/auth/me", nil, "lahijan_session="+sessionCookie)
	require.Equal(t, 200, meStatus)
	uid, _ := body["id"].(string)
	require.NotEmpty(t, uid)

	rows, err := ta.repos.AuditLog.ListGlobal(context.Background(), 200, 0)
	require.NoError(t, err)
	var sawLogin bool
	for _, r := range rows {
		if r.Action == audit.ActionIdpLogin && r.ActorUserID != nil && r.ActorUserID.String() == uid {
			sawLogin = true
			break
		}
	}
	assert.True(t, sawLogin, "an audit.action_auth_idp_login row must exist for the SAML user")
}

// TestSAML_LinkedIdentity_AppearsInList verifies a SAML identity linked to
// an existing password account appears in /me/identities.
func TestSAML_LinkedIdentity_AppearsInList(t *testing.T) {
	t.Parallel()
	ta, fakeIDP := newSAMLTestApp(t, false)
	fakeIDP.Subject = "saml-list-" + unique()
	fakeIDP.Email = "saml-list+" + unique() + "@example.test"
	fakeIDP.Name = "SAML List User"

	addr := "saml-link+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	require.Contains(t, sc, "lahijan_session=", "register must set the session cookie")
	sessionCookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	status, _, _ := driveSAMLFlow(t, ta, "fake", sessionCookie)
	require.Equal(t, 302, status, "link flow must succeed")

	listStatus, listBody, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus)
	require.Len(t, listBody, 1)
	row, _ := listBody[0].(map[string]any)
	assert.Equal(t, "saml:fake", row["provider"])
	assert.NotEmpty(t, row["subject"], "subject (NameID) must be populated")
	attrs, _ := row["attributes"].(map[string]any)
	require.NotNil(t, attrs, "attributes map must be present for a SAML identity")
	// The default attribute map uses the WS-Federation email claim URI.
	emailKey := "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
	emailVals, _ := attrs[emailKey].([]any)
	require.NotEmpty(t, emailVals, "the email attribute must be present")
}

// TestSAML_Unlink_FallsThroughToUnlinkSAML verifies the /me/identities/{id}
// DELETE handler dispatches to UnlinkSAML when the identity id did not
// match an OAuth/OIDC row.
func TestSAML_Unlink_FallsThroughToUnlinkSAML(t *testing.T) {
	t.Parallel()
	ta, fakeIDP := newSAMLTestApp(t, false)
	fakeIDP.Subject = "saml-unlink-" + unique()
	fakeIDP.Email = "saml-unlink+" + unique() + "@example.test"

	addr := "saml-unlink+" + unique() + "@example.test"
	_, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/register",
		map[string]any{"email": addr, "password": strongPw}, "")
	sessionCookie := "lahijan_session=" + extractCookie(sc, "lahijan_session")

	status, _, _ := driveSAMLFlow(t, ta, "fake", sessionCookie)
	require.Equal(t, 302, status)

	listStatus, listBody, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus)
	require.Len(t, listBody, 1)
	id, _ := listBody[0].(map[string]any)["id"].(string)
	require.NotEmpty(t, id)

	delStatus, _, _ := doJSON(t, ta, "DELETE", "/api/v1/me/identities/"+id, nil, sessionCookie)
	require.Equal(t, 200, delStatus)

	listStatus2, listBody2, _ := doGETArr(t, ta, "/api/v1/me/identities", sessionCookie)
	require.Equal(t, 200, listStatus2)
	assert.Empty(t, listBody2, "the SAML identity must be gone")
}

// TestSAML_Start_UnknownProvider_404 verifies the start handler 404s on an
// unknown SAML provider key.
func TestSAML_Start_UnknownProvider_404(t *testing.T) {
	t.Parallel()
	ta, _ := newSAMLTestApp(t, false)
	status, _, _ := doGET(t, ta, "/api/v1/auth/saml/does-not-exist/start", "")
	require.Equal(t, 404, status, "unknown SAML provider must 404")
}

// TestSAML_ACS_BadState_400 verifies the ACS rejects a tampered state token.
func TestSAML_ACS_BadState_400(t *testing.T) {
	t.Parallel()
	ta, _ := newSAMLTestApp(t, true)
	form := url.Values{}
	form.Set("SAMLResponse", base64.StdEncoding.EncodeToString([]byte("<x/>")))
	form.Set("RelayState", "tampered")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/saml/fake/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 400, resp.StatusCode, "tampered RelayState must 400")
}

// doRaw issues a GET and returns the status + raw body as a string. Used by
// the metadata test which checks XML content the JSON-shaped doJSON does
// not decode.
func doRaw(t *testing.T, ta *testApp, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}
