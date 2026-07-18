// saml_test.go covers the SAML service-provider surface against the in-process
// fake IdP (auth/saml/fake): NewProvider wires the SP from inline metadata,
// Metadata() serves valid XML, BuildAuthRequest mints a redirect URL the fake
// IdP accepts, and ProcessResponse verifies the signed assertion end to end.
package saml_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
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

	crewjam "github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/saml"
	samlfake "github.com/avestura/lahijan/internal/app/lahijan/auth/saml/fake"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
)

// noopVerifier is the simplest stateVerifier that always succeeds, so the
// saml tests can stay focused on the SAML plumbing (state verification has
// its own table-driven coverage in auth/state).
func noopVerifier(_, _, _, _ string) error { return nil }

// realVerifier wires up the actual auth/state.Signer so the state-token
// round-trip path runs through the real signer.
func realVerifier(t *testing.T) func(stateToken, cookieNonce, provider, linkUserID string) error {
	t.Helper()
	s := state.NewSigner(secrets.NewSigner("saml-test-signing-key"))
	return s.Verify
}

// spTestEnv wires up an SP + fake IdP pair bound to each other's metadata.
type spTestEnv struct {
	fake *samlfake.Server
	sp   saml.Provider
	// spCreds are the SP signing key + cert, exposed for tests that need
	// to register the SP metadata on the fake.
	spCreds saml.SPCredentials
}

// newSPTestEnv builds a fresh fake IdP + a Lahijan saml.Provider bound to it.
// Tests override fields on env.fake (Subject, Email, Name, SignResponse)
// before calling driveFlow to drive the SAML POST-binding round trip.
func newSPTestEnv(t *testing.T, verifier func(string, string, string, string) error) *spTestEnv {
	t.Helper()
	fake := samlfake.New()
	t.Cleanup(fake.Close)

	spCreds := generateSPCredentials(t)
	spEntityID := "https://app.test/api/v1/auth/saml/metadata"
	spACS := "https://app.test/api/v1/auth/saml/test/acs"
	spMetaURL := "https://app.test/api/v1/auth/saml/metadata"

	p, err := saml.NewProvider(saml.ProviderConfig{
		Key:            "test",
		EntityID:       spEntityID,
		ACSURL:         spACS,
		MetadataURL:    spMetaURL,
		IDPMetadataXML: string(fake.MetadataXML()),
	}, spCreds, verifier)
	require.NoError(t, err, "NewProvider against the fake metadata must succeed")

	// Register the SP's metadata on the fake so the IdP knows where to
	// POST the assertion. The metadata is built from the same SP instance
	// so the entity ID + ACS URL match.
	fake.SetSPMetadata(buildSPMetadata(t, p, spEntityID, spACS, spMetaURL, spCreds))

	return &spTestEnv{fake: fake, sp: p, spCreds: spCreds}
}

// driveFlow simulates the browser: GETs the SP's redirect URL, captures the
// auto-submit HTML form the IdP renders, and returns the parsed
// (SAMLResponse, RelayState, requestID) values. The requestID is what the SP
// passed to ProcessResponse so the InResponseTo replay check can fire.
func driveFlow(t *testing.T, env *spTestEnv, stateToken string) (samlResponse, relayState, requestID string) {
	t.Helper()
	authURL, reqID, err := env.sp.BuildAuthRequest(stateToken)
	require.NoError(t, err)

	// Hit the IdP /sso endpoint via an HTTP client that does NOT follow
	// redirects (the fake's /sso returns an HTML form, not a 302).
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(authURL)
	require.NoError(t, err, "drive IdP /sso")
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "fake /sso must return 200 with the auto-submit form; got body: %s", string(body))

	resp2, relay := samlfake.ExtractSAMLResponse(string(body))
	require.NotEmpty(t, resp2, "fake /sso must emit SAMLResponse; HTML was: %s", string(body))
	return resp2, relay, reqID
}

// buildSPMetadata constructs the SP EntityDescriptor the fake IdP needs to
// know about. The descriptor must list the SP's entity ID, the ACS URL (POST
// binding), and the SP's signing cert so the IdP can verify signed
// AuthnRequests (when the SP signs them) and POST the assertion back to the
// right ACS endpoint.
func buildSPMetadata(t *testing.T, p saml.Provider, entityID, acsURL, metadataURL string, creds saml.SPCredentials) *crewjam.EntityDescriptor {
	t.Helper()
	// Re-marshal the SP's self-metadata so we can hand it to the fake.
	// p.Metadata() returns the same XML the SP would publish at metadataURL.
	raw := p.Metadata()
	desc := &crewjam.EntityDescriptor{}
	require.NoError(t, xml.Unmarshal(raw, desc), "SP metadata must parse")
	if desc.EntityID == "" {
		desc.EntityID = entityID
	}
	return desc
}

// generateSPCredentials mints a fresh RSA-2048 key + self-signed cert for the
// SP. Returned as PEM-encoded bytes so the saml.NewProvider path mirrors the
// production bootstrap shape exactly.
func generateSPCredentials(t *testing.T) saml.SPCredentials {
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

// makeACSRequest builds a fake *http.Request that mirrors what the Lahijan
// ACS handler would see: a POST with SAMLResponse + RelayState form fields.
// ParseForm is called explicitly because crewjam's ParseResponse expects the
// form to already be parsed (it does not call ParseForm itself).
func makeACSRequest(t *testing.T, samlResponse, relayState string) *http.Request {
	t.Helper()
	form := url.Values{}
	form.Set("SAMLResponse", samlResponse)
	if relayState != "" {
		form.Set("RelayState", relayState)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/saml/test/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, req.ParseForm(), "ParseForm must succeed on the ACS request")
	return req
}

// TestNewProvider_InlineMetadata_Binds verifies NewProvider succeeds when
// given inline IdP metadata XML (the production default path).
func TestNewProvider_InlineMetadata_Binds(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	assert.Equal(t, "test", env.sp.Key())
	assert.Equal(t, "saml:test", env.sp.NamespacedKey())
}

// TestProvider_Metadata_ServesValidXML verifies the SP metadata XML parses
// as a SAML EntityDescriptor and carries the SP's entity ID + ACS URL.
func TestProvider_Metadata_ServesValidXML(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	raw := env.sp.Metadata()
	require.NotEmpty(t, raw, "Metadata() must return non-empty XML")

	var desc crewjam.EntityDescriptor
	require.NoError(t, xml.Unmarshal(raw, &desc), "metadata must parse as EntityDescriptor")
	assert.Equal(t, "https://app.test/api/v1/auth/saml/metadata", desc.EntityID)
	require.NotEmpty(t, desc.SPSSODescriptors)
	acsURLs := desc.SPSSODescriptors[0].AssertionConsumerServices
	require.NotEmpty(t, acsURLs, "metadata must list at least one ACS URL")
	assert.Equal(t, "https://app.test/api/v1/auth/saml/test/acs", acsURLs[0].Location)
}

// TestProvider_BuildAuthRequest_EmitsRedirectURL verifies the SP produces a
// redirect URL whose query string carries the SAMLRequest + RelayState
// parameters the IdP expects.
func TestProvider_BuildAuthRequest_EmitsRedirectURL(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	authURL, requestID, err := env.sp.BuildAuthRequest("RELAY-STATE-TOKEN")
	require.NoError(t, err)
	require.NotEmpty(t, requestID, "BuildAuthRequest must return a request ID")

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.NotEmpty(t, parsed.Query().Get("SAMLRequest"), "redirect URL must carry SAMLRequest")
	assert.Equal(t, "RELAY-STATE-TOKEN", parsed.Query().Get("RelayState"), "RelayState must match the state token")
}

// TestProvider_ProcessResponse_VerifiesSignedAssertion drives the full SAML
// POST-binding round trip against the fake IdP and verifies the SP returns
// the normalized Profile (Subject, Email, DisplayName).
func TestProvider_ProcessResponse_VerifiesSignedAssertion(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	env.fake.Subject = "nameid-42"
	env.fake.Email = "user@example.test"
	env.fake.Name = "SAML User"

	samlResponse, relayState, reqID := driveFlow(t, env, "RELAY-STATE")
	require.NotEmpty(t, samlResponse, "fake IdP must produce a SAMLResponse")
	require.NotEmpty(t, reqID, "BuildAuthRequest must return a request ID")

	req := makeACSRequest(t, samlResponse, relayState)
	prof, err := env.sp.ProcessResponse(context.Background(), req, reqID)
	require.NoError(t, err)
	assert.Equal(t, "nameid-42", prof.Subject)
	assert.Equal(t, "user@example.test", prof.Email)
	assert.Equal(t, "SAML User", prof.DisplayName)
}

// TestProvider_ProcessResponse_RejectsUnsigned verifies the SP rejects an
// assertion that carries no XML-DSig signature.
func TestProvider_ProcessResponse_RejectsUnsigned(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	unsigned := samlfake.UnsignedResponseXML(
		env.fake.Issuer, // issuer
		"https://app.test/api/v1/auth/saml/test/acs", // destination
		"",          // audience (unused in this builder)
		"nameid-42", // name id
		"",          // in response to (none: simulates IdP-initiated unsigned)
	)
	req := makeACSRequest(t, base64.StdEncoding.EncodeToString([]byte(unsigned)), "")
	_, err := env.sp.ProcessResponse(context.Background(), req, "")
	require.Error(t, err, "unsigned assertion must be rejected")
	assert.ErrorIs(t, err, saml.ErrAssertion)
}

// TestProvider_ProcessResponse_ReplayAttack_RejectedByInResponseTo verifies
// the SP rejects a Response whose InResponseTo does not match the request ID
// the SP emitted.
func TestProvider_ProcessResponse_ReplayAttack_RejectedByInResponseTo(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	samlResponse, relayState, _ := driveFlow(t, env, "RELAY-STATE")
	require.NotEmpty(t, samlResponse)

	// Tell the SP to expect a request ID that the IdP did NOT echo back.
	// The signed assertion's InResponseTo carries the real request ID;
	// passing a different ID here makes ParseResponse reject.
	req := makeACSRequest(t, samlResponse, relayState)
	_, err := env.sp.ProcessResponse(context.Background(), req, "wrong-request-id")
	require.Error(t, err, "InResponseTo mismatch must be rejected")
	assert.ErrorIs(t, err, saml.ErrAssertion)
}

// TestRegistry_Lookup verifies the registry resolves the raw key, the
// namespaced form, and rejects unknowns.
func TestRegistry_Lookup(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier)
	reg := saml.NewRegistry(env.sp)

	got, err := reg.Lookup("test")
	require.NoError(t, err)
	assert.Equal(t, env.sp, got)

	got2, err := reg.Lookup("saml:test")
	require.NoError(t, err, "namespaced form must also resolve")
	assert.Equal(t, env.sp, got2)

	_, err = reg.Lookup("unknown")
	assert.ErrorIs(t, err, saml.ErrProviderUnknown)
}

// TestProvider_VerifyState_RealSignerAcceptsPairedToken proves the state-
// token contract round-trips through the real auth/state signer.
func TestProvider_VerifyState_RealSignerAcceptsPairedToken(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, realVerifier(t))
	signer := state.NewSigner(secrets.NewSigner("saml-test-signing-key"))

	token, nonce, err := signer.Issue("saml:test", "")
	require.NoError(t, err)
	require.NoError(t, env.sp.VerifyState(token, nonce, ""))

	err = env.sp.VerifyState(token, "wrong-nonce", "")
	assert.ErrorIs(t, err, state.ErrInvalid)
}

// TestProvider_AllowIDPInitiated_AcceptsResponseWithoutInResponseTo proves
// that when AllowIDPInitiated is true, the SP accepts a SAML Response that
// has no InResponseTo (the IdP-initiated SSO flow). The fake IdP's
// ServeIDPInitiated endpoint produces such a response.
func TestProvider_AllowIDPInitiated_AcceptsResponseWithoutInResponseTo(t *testing.T) {
	t.Parallel()
	// Build a fake + SP pair where AllowIDPInitiated is true.
	fake := samlfake.New()
	t.Cleanup(fake.Close)
	spCreds := generateSPCredentials(t)
	spEntityID := "https://app.test/api/v1/auth/saml/metadata"
	spACS := "https://app.test/api/v1/auth/saml/test/acs"
	spMetaURL := "https://app.test/api/v1/auth/saml/metadata"

	p, err := saml.NewProvider(saml.ProviderConfig{
		Key:               "test",
		EntityID:          spEntityID,
		ACSURL:            spACS,
		MetadataURL:       spMetaURL,
		IDPMetadataXML:    string(fake.MetadataXML()),
		AllowIDPInitiated: true,
	}, spCreds, noopVerifier)
	require.NoError(t, err)
	fake.SetSPMetadata(buildSPMetadata(t, p, spEntityID, spACS, spMetaURL, spCreds))
	fake.Subject = "nameid-idp-init"

	// Drive the IdP-initiated flow via the fake's ServeIDPInitiated method.
	// The method writes an HTML auto-submit form to the response that POSTs
	// the SAMLResponse back to the SP's ACS URL with no InResponseTo.
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/idp-initiated", nil)
	fake.ServeIDPInitiatedHTTP(recorder, req, spEntityID, "")
	body := recorder.Body.String()
	samlResponse, _ := samlfake.ExtractSAMLResponse(body)
	require.NotEmpty(t, samlResponse, "IdP-initiated fake must produce a SAMLResponse")

	acsReq := makeACSRequest(t, samlResponse, "")
	prof, err := p.ProcessResponse(context.Background(), acsReq, "")
	require.NoError(t, err, "IdP-initiated response must verify when AllowIDPInitiated is true")
	assert.Equal(t, "nameid-idp-init", prof.Subject)
}

// TestProvider_NoIDPInitiated_RejectsResponseWithoutInResponseTo proves
// that when AllowIDPInitiated is false (the default), the SP rejects a
// response that carries no InResponseTo.
func TestProvider_NoIDPInitiated_RejectsResponseWithoutInResponseTo(t *testing.T) {
	t.Parallel()
	env := newSPTestEnv(t, noopVerifier) // default: AllowIDPInitiated=false
	env.fake.Subject = "nameid-no-idp-init"

	// Drive an IdP-initiated flow against the SP that does NOT allow it.
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/idp-initiated", nil)
	env.fake.ServeIDPInitiatedHTTP(recorder, req, "https://app.test/api/v1/auth/saml/metadata", "")
	body := recorder.Body.String()
	samlResponse, _ := samlfake.ExtractSAMLResponse(body)
	require.NotEmpty(t, samlResponse)

	acsReq := makeACSRequest(t, samlResponse, "")
	_, err := env.sp.ProcessResponse(context.Background(), acsReq, "")
	require.Error(t, err, "IdP-initiated must reject when AllowIDPInitiated is false")
	assert.ErrorIs(t, err, saml.ErrAssertion)
}
