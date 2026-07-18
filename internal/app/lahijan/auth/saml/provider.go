// Package saml: provider.go builds the concrete SAML ServiceProvider
// instance from a ProviderConfig + SPCredentials. The crewjam ServiceProvider
// owns the signing key + the IdP metadata; the wrapper here adds Lahijan's
// state-token verification, per-provider attribute mapping, and the namespaced
// key stored on user_saml_identities.
package saml

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	crewjam "github.com/crewjam/saml"
)

// NewProvider builds a SAML Provider from the given config + SP credentials.
// IdP metadata is loaded once here (from inline XML or a fetched URL); every
// subsequent ProcessResponse uses the cached metadata.
//
// Returns ErrMetadata (wrapping the underlying cause) on a metadata load
// failure so the bootstrap can fail fast with a clear message.
func NewProvider(cfg ProviderConfig, creds SPCredentials, verifier stateVerifier) (Provider, error) {
	if cfg.Key == "" {
		return nil, errors.New("saml: ProviderConfig.Key is required")
	}
	if creds.KeyPEM == nil || creds.CertPEM == nil {
		return nil, errors.New("saml: SPCredentials.KeyPEM and CertPEM are required")
	}
	signerKey, err := parseSignerKey(creds.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("saml: parse signing key: %w", err)
	}
	cert, err := parseCert(creds.CertPEM)
	if err != nil {
		return nil, fmt.Errorf("saml: parse signing cert: %w", err)
	}

	idpMetadata, err := loadIDPMetadata(cfg.IDPMetadataXML, cfg.IDPMetadataURL)
	if err != nil {
		return nil, err
	}

	metadataURL, err := url.Parse(cfg.MetadataURL)
	if err != nil {
		return nil, fmt.Errorf("saml: parse metadata url: %w", err)
	}
	acsURL, err := url.Parse(cfg.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("saml: parse acs url: %w", err)
	}
	if verifier == nil {
		verifier = noopVerifier
	}

	sp := &crewjam.ServiceProvider{
		EntityID:              cfg.EntityID,
		Key:                   signerKey,
		Certificate:           cert,
		MetadataURL:           *metadataURL,
		AcsURL:                *acsURL,
		IDPMetadata:           idpMetadata,
		AuthnNameIDFormat:     crewjam.UnspecifiedNameIDFormat,
		MetadataValidDuration: 7 * 24 * time.Hour,
		AllowIDPInitiated:     cfg.AllowIDPInitiated,
	}

	return &provider{
		key:           cfg.Key,
		namespaced:    "saml:" + cfg.Key,
		sp:            sp,
		stateVerifier: verifier,
		attrMap:       cfg.AttributeMap.withDefaults(),
		allowIDPInit:  cfg.AllowIDPInitiated,
		metadataURL:   *metadataURL,
	}, nil
}

// BuildAuthRequest mints the IdP authorization URL with a signed AuthnRequest
// inside. The stateToken is included as the SAML RelayState AND the auth/state
// CSRF payload (signed separately). The returned requestID is the SAML
// AuthnRequest ID the IdP MUST echo back in Response.InResponseTo; the
// callback enforces this for replay protection.
//
// We use the HTTP-Redirect binding: the AuthnRequest is deflated + base64-
// encoded + signed, then placed on the redirect URL's query string. This is
// the simplest binding for browser-driven login and what Microsoft Entra, Okta,
// and Keycloak all support by default.
func (p *provider) BuildAuthRequest(stateToken string) (string, string, error) {
	// The relay state is the CSRF state token. The IdP echoes it back verbatim
	// on the callback; the callback verifies it via auth/state.
	authnReq, err := p.sp.MakeAuthenticationRequest(
		p.sp.GetSSOBindingLocation(crewjam.HTTPRedirectBinding),
		crewjam.HTTPRedirectBinding,
		crewjam.HTTPPostBinding,
	)
	if err != nil {
		return "", "", fmt.Errorf("saml: build authn request: %w", err)
	}
	redirectURL, err := authnReq.Redirect(stateToken, p.sp)
	if err != nil {
		return "", "", fmt.Errorf("saml: sign + redirect authn request: %w", err)
	}
	return redirectURL.String(), authnReq.ID, nil
}

// ProcessResponse verifies the IdP's posted assertion and returns the
// normalized Profile. The requestID is the value returned by
// BuildAuthRequest so the InResponseTo replay check can fire; pass "" to
// accept IdP-initiated SSO (only valid when AllowIDPInitiated is true).
//
// crewjam's ParseResponse performs the full verification surface:
//   - signature on the Response and/or Assertion (XML-DSig via goxmldsig)
//   - audience restriction (must include our SP entity ID)
//   - recipient (must be our ACS URL)
//   - conditions (NotBefore / NotOnOrAfter, with ±60s skew)
//   - InResponseTo (must match one of possibleRequestIDs, OR be empty when
//     AllowIDPInitiated is true)
//
// Any failure is returned as ErrAssertion wrapping crewjam's
// InvalidResponseError so the api handler can map the localised envelope.
func (p *provider) ProcessResponse(ctx context.Context, req *http.Request, requestID string) (Profile, error) {
	possibleRequestIDs := []string{}
	if requestID != "" {
		possibleRequestIDs = append(possibleRequestIDs, requestID)
	} else if !p.allowIDPInit {
		// SP-initiated flow but no requestID: this is a malformed callback
		// (we always emit a requestID in BuildAuthRequest). Reject rather
		// than silently accept as IdP-initiated.
		return Profile{}, fmt.Errorf("%w: missing InResponseTo and IdP-initiated SSO is disabled", ErrAssertion)
	}

	assertion, err := p.sp.ParseResponse(req, possibleRequestIDs)
	if err != nil {
		// crewjam wraps the underlying cause in InvalidResponseError; pull
		// it out so the api handler can log the real reason while surfacing
		// a generic ErrAssertion to the user.
		var invErr *crewjam.InvalidResponseError
		if errors.As(err, &invErr) {
			return Profile{}, fmt.Errorf("%w: %s", ErrAssertion, invErr.PrivateErr.Error())
		}
		return Profile{}, fmt.Errorf("%w: %w", ErrAssertion, err)
	}

	return profileFromAssertion(assertion, p.attrMap, p.sp.IDPMetadata), nil
}

// Metadata implements Provider. Returns the SP metadata XML the start
// handler serves at /api/v1/auth/saml/metadata.
func (p *provider) Metadata() []byte {
	entity := p.sp.Metadata()
	raw, err := xml.MarshalIndent(entity, "", "  ")
	if err != nil {
		// xml.Marshal of a crewjam EntityDescriptor should never fail; if it
		// does, the start handler returns a 500. Returning []byte{} here
		// keeps the signature simple (no error) and the handler treats an
		// empty body as an internal error.
		return []byte{}
	}
	return append(raw, '\n')
}

// loadIDPMetadata resolves the IdP's metadata from inline XML (preferred) or
// by fetching it from a URL. Returns ErrMetadata wrapping the cause on
// failure so the bootstrap fails fast with a clear message.
func loadIDPMetadata(xmlInline, urlStr string) (*crewjam.EntityDescriptor, error) {
	if xmlInline != "" {
		return parseIDPMetadata([]byte(xmlInline))
	}
	if urlStr == "" {
		return nil, fmt.Errorf("%w: no inline metadata and no metadata URL provided", ErrMetadata)
	}
	return fetchIDPMetadata(urlStr)
}

// parseIDPMetadata unmarshals raw XML into an EntityDescriptor.
func parseIDPMetadata(raw []byte) (*crewjam.EntityDescriptor, error) {
	desc := &crewjam.EntityDescriptor{}
	if err := xml.Unmarshal(raw, desc); err != nil {
		return nil, fmt.Errorf("%w: parse inline metadata: %w", ErrMetadata, err)
	}
	return desc, nil
}

// fetchIDPMetadata GETs the IdP metadata URL and parses the response.
func fetchIDPMetadata(urlStr string) (*crewjam.EntityDescriptor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build metadata request: %w", ErrMetadata, err)
	}
	req.Header.Set("Accept", "application/xml, text/xml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch %s: %w", ErrMetadata, urlStr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%w: %s returned status %d: %s", ErrMetadata, urlStr, resp.StatusCode, string(body))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read metadata body: %w", ErrMetadata, err)
	}
	desc := &crewjam.EntityDescriptor{}
	if err := xml.Unmarshal(raw, desc); err != nil {
		return nil, fmt.Errorf("%w: parse fetched metadata: %w", ErrMetadata, err)
	}
	return desc, nil
}

// profileFromAssertion normalizes a verified crewjam Assertion into our
// Profile shape. The Subject is the NameID; Email + DisplayName are derived
// from the attribute statement via the per-provider attribute map.
func profileFromAssertion(
	a *crewjam.Assertion,
	attrMap AttributeMap,
	idp *crewjam.EntityDescriptor,
) Profile {
	prof := Profile{
		Attributes:  assertionToMap(a),
		IDPEntityID: idp.EntityID,
	}
	if a.Subject != nil && a.Subject.NameID != nil {
		prof.Subject = a.Subject.NameID.Value
	}
	if attrMap.Email != "" {
		if v := findAttribute(a, attrMap.Email); v != "" {
			prof.Email = v
		}
	}
	if attrMap.Name != "" {
		if v := findAttribute(a, attrMap.Name); v != "" {
			prof.DisplayName = v
		}
	}
	return prof
}

// findAttribute scans the assertion's attribute statement(s) for one with the
// given name OR FriendlyName and returns its first value. SAML attributes can
// be multi-valued but for the email + name mapping the first value is the
// right one for Lahijan's use.
func findAttribute(a *crewjam.Assertion, name string) string {
	for i := range a.AttributeStatements {
		for _, attr := range a.AttributeStatements[i].Attributes {
			if attr.Name == name || attr.FriendlyName == name {
				if len(attr.Values) > 0 {
					return attr.Values[0].Value
				}
			}
		}
	}
	return ""
}

// assertionToMap flattens the attribute statement(s) into a map suitable for
// JSON-encoding into user_saml_identities.attributes_json. Multi-valued
// attributes become a JSON array; single-valued attributes become a one-element
// array. The shape stays consistent: every value is a []string so the consumer
// never has to switch on JSON kind.
func assertionToMap(a *crewjam.Assertion) map[string]any {
	out := make(map[string]any)
	for i := range a.AttributeStatements {
		for _, attr := range a.AttributeStatements[i].Attributes {
			key := attr.Name
			if key == "" {
				key = attr.FriendlyName
			}
			if key == "" {
				continue
			}
			values := []string{}
			for _, v := range attr.Values {
				if v.Value != "" {
					values = append(values, v.Value)
				}
			}
			if len(values) > 0 {
				out[key] = values
			}
		}
	}
	return out
}

// parseSignerKey unmarshals a PEM-encoded RSA private key. Returns the key
// as a *rsa.PrivateKey (which satisfies crypto.Signer) so crewjam can use it
// for XML-DSig signing. ECDSA keys are rejected: SAML XML-DSig is fiddly with
// ECDSA and RSA is the universally-supported default.
func parseSignerKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block in signing key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse pkcs8: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("signing key is not RSA (RSA is required for SAML XML-DSig)")
	}
	return rsaKey, nil
}

// parseCert unmarshals a PEM-encoded x509 certificate. Returns the parsed
// cert so crewjam can publish it in the SP metadata.
func parseCert(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block in signing cert")
	}
	return x509.ParseCertificate(block.Bytes)
}
