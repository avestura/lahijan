// Package fake provides an in-process SAML 2.0 Identity Provider for the
// WS-07b integration tests. It composes crewjam/saml's `IdentityProvider`
// type with tiny stub implementations of its ServiceProviderProvider and
// SessionProvider interfaces — enough to drive the full SAML POST-binding
// flow against Lahijan's service-provider code without an external
// simplesamlphp container.
//
// The fake signs assertions with a real RSA key (2048-bit, generated at
// New() time), so Lahijan's id_token verification path (XML-DSig signature
// check, audience check, recipient check, conditions check, InResponseTo
// replay check) is exercised end to end.
//
// Tests build a SAML Provider against the fake's metadata, drive the
// SP-initiated redirect, capture the auto-submit HTML form the fake renders,
// and POST the form to the Lahijan ACS endpoint. The RelayState the SP
// emitted (carrying the auth/state CSRF token) round-trips unchanged.
package fake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	crewjam "github.com/crewjam/saml"
)

// Server is an in-process SAML Identity Provider. Start one per test, build
// a saml.Provider against its metadata, then drive the SP flow. Close in
// t.Cleanup.
type Server struct {
	HTTP *httptest.Server

	// Key is the RSA private key the fake uses to sign assertions. The
	// matching public certificate (Certificate) is published in the IdP
	// metadata so a real SP can verify the signature.
	Key *rsa.PrivateKey

	// Certificate is the x509 cert matching Key.
	Certificate *x509.Certificate

	// KeyID is the kid advertised in the IdP metadata's KeyDescriptor.
	KeyID string

	// Issuer is the IdP's entity ID (= Server.URL). Set after the httptest
	// server starts so the metadata's entityID matches where it's served.
	Issuer string

	// SSOUURL is the IdP's HTTP-Redirect SSO endpoint (= Server.URL + /sso).
	SSOURL string

	// Subject/Email/Name are the values baked into every assertion's NameID
	// + attribute statement. Override per-case to exercise different IdP
	// responses.
	Subject string
	Email   string
	Name    string

	// SignResponse controls whether the fake signs the SAML Response.
	// Default true. Tests that need to verify "unsigned rejected" set this
	// to false and use UnsignedResponseXML instead.
	SignResponse bool

	// InResponseTo, when non-empty, is the value baked into the Response's
	// InResponseTo attribute. When empty, the fake uses the AuthnRequest's
	// ID parsed from the inbound SAMLRequest (the normal SP-initiated path).
	// Tests that need to simulate replay or IdP-initiated SSO override this.
	InResponseTo string

	// mu guards idp + spMeta during the per-test SetSPMetadata call.
	mu sync.RWMutex

	// idp is the underlying crewjam IdentityProvider. Tests do NOT drive it
	// directly; it's exposed via the HTTP handler so the SSO + metadata
	// endpoints behave identically to a real IdP.
	idp *crewjam.IdentityProvider

	// spProvider is the stub ServiceProviderProvider the idp uses to look
	// up the test SP's metadata. The test calls SetSPMetadata to register
	// the SP after building it.
	spProvider *spMetadataProvider

	// sessions is the stub SessionProvider. It always returns a session
	// carrying the test-configured Subject + attributes.
	sessions *stubSessionProvider
}

// New starts a fresh SAML IdP fake. Tests should defer s.Close().
//
// Panics on key-generation failure (only test setup; acceptable).
func New() *Server {
	priv, cert, err := generateSelfSignedCert()
	if err != nil {
		panic(fmt.Sprintf("saml/fake: cert generation: %v", err))
	}
	s := &Server{
		Key:          priv,
		Certificate:  cert,
		KeyID:        "fake-idp-key-1",
		Subject:      "saml-nameid-42",
		Email:        "saml-user@example.test",
		Name:         "SAML User",
		SignResponse: true,
	}
	s.spProvider = &spMetadataProvider{providers: map[string]*crewjam.EntityDescriptor{}}
	s.sessions = &stubSessionProvider{server: s}

	mux := http.NewServeMux()
	// The httptest server hasn't started yet, so we can't build the IdP
	// until we have a URL. We split construction: start the server first
	// with a placeholder, then construct the IdP once we know its URL, then
	// swap the mux's handlers.
	s.HTTP = httptest.NewServer(mux)
	s.Issuer = s.HTTP.URL + "/"
	s.SSOURL = s.HTTP.URL + "/sso"

	issuerURL, _ := url.Parse(s.Issuer)
	_ = issuerURL // kept for clarity; the IdP derives its issuer from MetadataURL
	ssoURL, _ := url.Parse(s.SSOURL)
	loginURL, _ := url.Parse(s.HTTP.URL + "/login")
	logoutURL, _ := url.Parse(s.HTTP.URL + "/logout")
	metaURL, _ := url.Parse(s.HTTP.URL + "/metadata")

	s.idp = &crewjam.IdentityProvider{
		Key:                     priv,
		Signer:                  priv,
		Certificate:             cert,
		MetadataURL:             *metaURL,
		SSOURL:                  *ssoURL,
		LoginURL:                *loginURL,
		LogoutURL:               *logoutURL,
		ServiceProviderProvider: s.spProvider,
		SessionProvider:         s.sessions,
		SignatureMethod:         "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256",
	}

	// Replace the mux handlers now that s.idp exists.
	mux.HandleFunc("/metadata", s.idp.ServeMetadata)
	mux.HandleFunc("/sso", s.idp.ServeSSO)
	// /login is the SessionProvider's responsibility; the stub completes the
	// request directly (no real login form) so this handler exists only to
	// satisfy the URL wire-up.
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stub login should not be reached", http.StatusInternalServerError)
	})
	return s
}

// Close stops the fake's httptest server.
func (s *Server) Close() { s.HTTP.Close() }

// MetadataXML returns the IdP's metadata as raw XML. Tests pass this as
// ProviderConfig.IdPMetadataXML so the SP is bound to the fake without an
// HTTP fetch.
func (s *Server) MetadataXML() []byte {
	desc := s.idp.Metadata()
	raw, err := xml.Marshal(desc)
	if err != nil {
		return []byte{}
	}
	return raw
}

// SetSPMetadata registers a service provider with the fake. The SP's entity
// ID is read from the metadata's entityID attribute. Must be called before
// driving the SSO flow.
func (s *Server) SetSPMetadata(spMeta *crewjam.EntityDescriptor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spProvider.providers[spMeta.EntityID] = spMeta
}

// BaseURL returns the fake's root URL (no trailing slash).
func (s *Server) BaseURL() string { return strings.TrimRight(s.HTTP.URL, "/") }

// spMetadataProvider is the stub ServiceProviderProvider the fake IdP uses.
// It holds a map from SP entity ID → SP metadata, populated by the test via
// SetSPMetadata.
type spMetadataProvider struct {
	mu        sync.RWMutex
	providers map[string]*crewjam.EntityDescriptor
}

// GetServiceProvider implements crewjam.ServiceProviderProvider.
func (p *spMetadataProvider) GetServiceProvider(_ *http.Request, serviceProviderID string) (*crewjam.EntityDescriptor, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	sp, ok := p.providers[serviceProviderID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errSPUnknown, serviceProviderID)
	}
	return sp, nil
}

// errSPUnknown is the sentinel returned when the test forgot to register the
// SP via SetSPMetadata before driving the flow. Surfaces as a 400 from the
// fake's /sso handler so the test fails loudly with a clear cause.
var errSPUnknown = errors.New("saml/fake: SP not registered with the fake IdP")

// stubSessionProvider is the stub SessionProvider the fake IdP uses. It
// always returns a session carrying the test-configured Subject/Email/Name;
// there is no real login form.
type stubSessionProvider struct {
	server *Server
}

// GetSession implements crewjam.SessionProvider. It returns a session for
// the test-configured user without prompting; the fake's /login handler is
// never reached.
func (p *stubSessionProvider) GetSession(_ http.ResponseWriter, _ *http.Request, req *crewjam.IdpAuthnRequest) *crewjam.Session {
	now := time.Now()
	attrs := []crewjam.Attribute{
		{
			Name:         "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
			FriendlyName: "email",
			Values:       []crewjam.AttributeValue{{Value: p.server.Email}},
		},
		{
			Name:         "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
			FriendlyName: "name",
			Values:       []crewjam.AttributeValue{{Value: p.server.Name}},
		},
	}
	sessionID := "fake-session"
	if req != nil {
		sessionID = "fake-session-" + req.Request.ID
	}
	return &crewjam.Session{
		ID:               sessionID,
		CreateTime:       now,
		ExpireTime:       now.Add(time.Hour),
		Index:            "fake-session-index",
		NameID:           p.server.Subject,
		UserName:         p.server.Email,
		UserEmail:        p.server.Email,
		UserCommonName:   p.server.Name,
		UserGivenName:    p.server.Name,
		CustomAttributes: attrs,
	}
}

// ExtractSAMLResponse pulls the SAMLResponse form field out of the auto-
// submit HTML the IdP's /sso handler renders. The HTML has the shape:
//
//	<form method="post" action="https://app.test/api/v1/auth/saml/X/acs">
//	  <input type="hidden" name="SAMLResponse" value="..." />
//	  <input type="hidden" name="RelayState" value="..." />
//	</form>
//
// Tests call this to feed the form into a POST against the Lahijan ACS.
// The extracted value is HTML-unescaped because the auto-submit template
// escapes the base64 payload's special characters (&, <, >, ", ') per the
// HTML spec — a raw substring scan would surface the escaped forms and the
// SP's base64 decoder would reject them.
func ExtractSAMLResponse(htmlBody string) (samlResponse, relayState string) {
	samlResponse = html.UnescapeString(extractHiddenInput(htmlBody, "SAMLResponse"))
	relayState = html.UnescapeString(extractHiddenInput(htmlBody, "RelayState"))
	return
}

// extractHiddenInput scans the auto-submit HTML for one <input type="hidden"
// name="NAME" value="VALUE"> and returns VALUE. The IdP's template is stable
// so a defensive substring scan beats a full HTML parse for test clarity.
func extractHiddenInput(htmlBody, name string) string {
	// Match either name="X" value="Y" or value="Y" name="X" — the template
	// picks one ordering, but we tolerate both.
	marker := `name="` + name + `"`
	idx := strings.Index(htmlBody, marker)
	if idx < 0 {
		return ""
	}
	rest := htmlBody[idx+len(marker):]
	valIdx := strings.Index(rest, `value="`)
	if valIdx < 0 {
		return ""
	}
	valStart := valIdx + len(`value="`)
	valEnd := strings.IndexByte(rest[valStart:], '"')
	if valEnd < 0 {
		return ""
	}
	return rest[valStart : valStart+valEnd]
}

// EncodeFormBody URL-encodes the SAMLResponse + RelayState as a POST body
// the way the auto-submit form would. The Lahijan ACS handler parses it from
// the request's Form.
func EncodeFormBody(samlResponse, relayState string) string {
	v := url.Values{}
	v.Set("SAMLResponse", samlResponse)
	if relayState != "" {
		v.Set("RelayState", relayState)
	}
	return v.Encode()
}

// UnsignedResponseXML returns a SAML Response XML carrying an unsigned
// assertion. Tests that need to verify "unsigned rejected" use this helper
// to build the payload the IdP would have POSTed, then feed it through the
// SP's ACS handler and assert the rejection.
func UnsignedResponseXML(issuer, destination, audience, nameID, inResponseTo string) string {
	now := time.Now().UTC().Format(time.RFC3339)
	// Build the assertion XML by hand so it has no <Signature> element.
	// The ACS handler's signature verifier (XML-DSig via goxmldsig) treats
	// a missing signature as a hard failure, which is exactly the contract
	// the "unsigned rejected" DoD row asserts.
	return fmt.Sprintf(
		`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"`+
			` xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"`+
			` ID="_fake-unsigned-response" InResponseTo=%q Version="2.0"`+
			` IssueInstant=%q Destination=%q>`+
			`<saml:Issuer>%s</saml:Issuer>`+
			`<samlp:Status><samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/></samlp:Status>`+
			`<saml:Assertion Version="2.0" ID="_assertion" IssueInstant=%q>`+
			`<saml:Issuer>%s</saml:Issuer>`+
			`<saml:Subject><saml:NameID>%s</saml:NameID></saml:Subject>`+
			`</saml:Assertion>`+
			`</samlp:Response>`,
		inResponseTo, now, destination, issuer, now, issuer, nameID,
	)
}

// Base64Std returns base64-std-encoded input. Mirrors what the auto-submit
// HTML form carries.
func Base64Std(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

// generateSelfSignedCert mints a fresh RSA-2048 key + self-signed x509 cert
// for the fake IdP. The cert is NOT trusted by anything (tests treat the
// fake's metadata as the source of truth, not a CA chain); it exists only so
// XML-DSig has a real public key to verify against.
func generateSelfSignedCert() (*rsa.PrivateKey, *x509.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("rsa keygen: %w", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   "lahijan-saml-fake-idp",
			Organization: []string{"Lahijan Test"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("x509 create: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("x509 parse: %w", err)
	}
	return priv, cert, nil
}
