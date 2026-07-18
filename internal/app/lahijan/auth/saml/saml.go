// Package saml implements Lahijan's SAML 2.0 service-provider client (WS-07b):
// the surface that lets users log in (and link their existing account to) an
// enterprise SAML IdP (Microsoft Entra / Azure AD, Okta, OneLogin,
// Shibboleth, Google Workspace SAML).
//
// The package mirrors the auth/oauth surface from WS-07a so the auth/idp
// account-linking service can consume SAML through the same ExternalIDP
// adapter shape. The contract is:
//
//   - Metadata()                     → serve SP metadata at a well-known URL
//   - BuildAuthRequest(state)        → mint a signed AuthnRequest + redirect URL
//   - VerifyState(state, nonce, ...) → CSRF state-token check (auth/state)
//   - ProcessResponse(req, requestID)→ verify the IdP's signed assertion
//
// The package depends on github.com/crewjam/saml (BSD-2-Clause; see ADR-0020)
// for the heavy SAML + XML-DSig plumbing: signing AuthnRequests, parsing +
// verifying signed assertions (signature, audience, recipient, conditions,
// InResponseTo replay check).
//
// Like auth/oauth, this package is the SAML layer only — it does NOT touch the
// database, does NOT issue sessions, and does NOT persist identities. Account
// linking is the auth/idp service's job. The provider key stored on
// user_saml_identities is namespaced as "saml:<key>" so SAML providers never
// collide with OAuth presets or OIDC providers in the parallel
// user_oauth_identities table.
package saml

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	crewjam "github.com/crewjam/saml"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
)

// ErrProviderUnknown is returned when a SAML provider key is not configured.
var ErrProviderUnknown = errors.New("saml: unknown or disabled provider")

// ErrAssertion is returned when the IdP's response fails verification (bad
// signature, wrong audience, expired conditions, replay via InResponseTo,
// missing NameID). The wrapped cause carries the underlying crewjam error.
var ErrAssertion = errors.New("saml: assertion verification failed")

// ErrMetadata is returned when an IdP's published metadata cannot be fetched
// or parsed. Wrapped around the underlying network / parse error.
var ErrMetadata = errors.New("saml: idp metadata load failed")

// Profile is the post-verification normalized user info from the IdP. Mirrors
// oauth.Profile / idp.Profile so the auth/idp service treats SAML, OAuth, and
// OIDC flows identically downstream. Subject is the SAML NameID; Email /
// DisplayName are derived from the attribute statement via the per-provider
// attribute map.
type Profile struct {
	Subject     string // the SAML NameID
	Email       string
	DisplayName string
	// Attributes is the full attribute statement the IdP asserted. The idp
	// service persists a JSON-encoded snapshot on every login so the user's
	// profile reflects the latest claims.
	Attributes map[string]any
	// IDPEntityID is the Issuer of the SAML response (the IdP's entity ID).
	// Stored on user_saml_identities.idp_entity_id so a future migration can
	// disambiguate if two IdPs ever reuse a NameID.
	IDPEntityID string
}

// ProviderConfig carries the deployer-supplied fields needed to build one
// SAML provider. The SP signing key + cert are loaded by the bootstrap from
// conf.auth.saml.spSigningKey + spSigningCert; per-provider IdP metadata is
// loaded from IDPMetadataXML (inline) OR fetched from IDPMetadataURL.
type ProviderConfig struct {
	Key string
	// EntityID is this SP's entity ID (the value placed in <Issuer> of
	// AuthnRequests and in the SP metadata). Typically
	// https://app.example.com/saml/metadata.
	EntityID string
	// ACSURL is the SP's Assertion Consumer Service URL (POST binding).
	ACSURL string
	// MetadataURL is the SP's metadata endpoint URL.
	MetadataURL string

	// IDPMetadataXML is the inline XML metadata for the IdP. When non-empty,
	// the provider uses it directly and IDPMetadataURL is ignored.
	IDPMetadataXML string
	// IDPMetadataURL is the URL the provider fetches the IdP metadata from
	// when IDPMetadataXML is empty. Fetched once at NewProvider time.
	IDPMetadataURL string

	// AttributeMap maps Lahijan field names ("email", "name") to the SAML
	// attribute names the IdP uses. Defaults are the standard OIDC-style
	// names; deployers override for IdPs that use Microsoft's schema
	// (http://schemas.microsoft.com/...).
	AttributeMap AttributeMap

	// AllowIDPInitiated, when true, accepts SAML responses that have no
	// InResponseTo (i.e. IdP-initiated SSO). Default false (only SP-initiated
	// is accepted) because allowing IdP-initiated without an allow-list of
	// trusted IdPs is a known SAML CSRF vector.
	AllowIDPInitiated bool
}

// AttributeMap carries the per-provider mapping from SAML attribute names to
// Lahijan fields. The zero value uses StandardAttributeMap. Deployers override
// for IdPs that publish attributes under non-standard names (Entra, Okta).
type AttributeMap struct {
	// Email is the SAML attribute name carrying the user's email. Defaults
	// to "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress".
	Email string
	// Name is the SAML attribute name carrying the user's display name.
	// Defaults to "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name".
	Name string
}

// StandardAttributeMap is the default attribute mapping used when the deployer
// does not specify one. These names are the WS-Federation / SAML standard
// claim names that Microsoft Entra, Okta, and most IdPs use by default.
var StandardAttributeMap = AttributeMap{
	Email: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	Name:  "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
}

// withDefaults returns a copy of m with empty fields replaced by the standard
// mapping. Used by NewProvider so the rest of the package can assume a fully-
// populated map.
func (m AttributeMap) withDefaults() AttributeMap {
	out := m
	if out.Email == "" {
		out.Email = StandardAttributeMap.Email
	}
	if out.Name == "" {
		out.Name = StandardAttributeMap.Name
	}
	return out
}

// SPCredentials carries the SP's signing key + certificate. The signing key
// is the value of conf.auth.saml.spSigningKey (a PEM-encoded RSA or ECDSA
// private key); the certificate is the matching public cert (PEM-encoded
// x509). Both are loaded once at bootstrap and shared by every SAML provider
// instance.
//
// The signing key is HIGH-SENSITIVITY material. The bootstrap reads it from
// an environment-supplied value (or an external file path) and never logs it.
// A future ADR may move this to an encrypted-at-rest column to support
// key-per-tenant isolation; for MVP a single process-wide key pair covers
// every SAML provider.
type SPCredentials struct {
	// KeyPEM is the PEM-encoded private key (RSA or ECDSA) used to sign
	// AuthnRequests and SP metadata.
	KeyPEM []byte
	// CertPEM is the PEM-encoded x509 certificate matching KeyPEM, published
	// in the SP metadata so the IdP can verify our signed requests.
	CertPEM []byte
}

// stateVerifier is the signature auth/state.Signer.Verify satisfies; the SAML
// package takes it as-is so it shares the same state-token signing path as
// auth/oauth and auth/oidc. The provider argument is the namespaced key
// ("saml:<config-key>") so a callback to one SAML provider cannot be replayed
// against another or against an OAuth preset path.
type stateVerifier func(stateToken, cookieNonce, provider, linkUserID string) error

// Provider is one configured SAML IdP. The contract mirrors oauth.Provider so
// the api handler can use either through the same shape; the SAML-only
// surface is the SP metadata + the verified Profile returned by Exchange.
type Provider interface {
	// Key is the stable identifier in URL paths and user_saml_identities
	// (e.g. "saml:entra", "saml:okta").
	Key() string

	// NamespacedKey is the value stored on user_saml_identities.provider
	// ("saml:<Key>"). It matches the provider claim baked into the state
	// token so a callback to one SAML provider cannot be replayed against
	// another or against an OAuth/OIDC path.
	NamespacedKey() string

	// BuildAuthRequest mints the IdP AuthnRequest URL the browser is
	// redirected to. The stateToken is included as the SAML RelayState (and
	// also as our CSRF state token's payload, signed separately). The
	// returned requestID is the SAML AuthnRequest ID the IdP MUST echo back
	// in the Response.InResponseTo; the callback enforces this for replay
	// protection.
	BuildAuthRequest(stateToken string) (authURL, requestID string, err error)

	// VerifyState delegates to the auth/state signer (shared with auth/oauth
	// and auth/oidc). The provider argument is namespaced ("saml:<key>") so
	// the path-bound check rejects cross-provider replay.
	VerifyState(stateToken, cookieNonce, linkUserID string) error

	// ProcessResponse verifies the IdP's signed assertion and returns the
	// normalized Profile. The requestID is the value returned by
	// BuildAuthRequest so the InResponseTo replay check can fire; pass "" to
	// accept IdP-initiated SSO (only valid when AllowIDPInitiated is true).
	ProcessResponse(ctx context.Context, req *http.Request, requestID string) (Profile, error)

	// Metadata returns the SP metadata as XML. The start handler serves this
	// at /api/v1/auth/saml/metadata so the IdP can register our SP.
	Metadata() []byte
}

// provider is the concrete implementation wrapping crewjam/saml's
// ServiceProvider. The crewjam type owns the signing key, the IdP metadata,
// and the verification logic; we adapt its surface to Lahijan's Provider
// interface + add the state-token check shared with OAuth/OIDC.
type provider struct {
	key           string
	namespaced    string
	sp            *crewjam.ServiceProvider
	stateVerifier stateVerifier
	attrMap       AttributeMap
	allowIDPInit  bool
	metadataURL   url.URL
}

// Key implements Provider.
func (p *provider) Key() string { return p.key }

// NamespacedKey implements Provider.
func (p *provider) NamespacedKey() string { return p.namespaced }

// VerifyState implements Provider. The state-token's provider claim is
// compared against the namespaced key ("saml:<key>") so a callback to
// /saml/X cannot be replayed against /saml/Y or against an OAuth/OIDC path.
func (p *provider) VerifyState(stateToken, cookieNonce, linkUserID string) error {
	return p.stateVerifier(stateToken, cookieNonce, p.namespaced, linkUserID)
}

// Compile-time check that the auth/state Signer satisfies our stateVerifier.
var _ stateVerifier = (*state.Signer)(nil).Verify

// Registry resolves a SAML provider by its stable key. Mirrors oauth.Registry
// / oidc.Registry so the api handler can look up any IdP type through the
// same shape.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry builds a registry over the given providers. Duplicate keys
// silently overwrite (programmer error; the bootstrap validates the config).
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{providers: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		r.providers[p.Key()] = p
	}
	return r
}

// Lookup returns the provider for the given key, or ErrProviderUnknown.
// Lookup accepts BOTH the raw key ("entra") and the namespaced form
// ("saml:entra") so the api handler can pass the path parameter straight
// through; the SP-initiated start path uses the raw form, the callback
// resolves the namespaced form stored on user_saml_identities.
func (r *Registry) Lookup(key string) (Provider, error) {
	if p, ok := r.providers[key]; ok {
		return p, nil
	}
	// Try stripping the "saml:" prefix in case the caller passed the
	// namespaced form (the ACS handler resolves the provider from the path
	// param; a future client could pass either form).
	const prefix = "saml:"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		if p, ok := r.providers[key[len(prefix):]]; ok {
			return p, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrProviderUnknown, key)
}

// Keys returns the configured provider keys.
func (r *Registry) Keys() []string {
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k)
	}
	return out
}

// noopVerifier is a stateVerifier that always returns nil. Useful for tests
// that exercise the SAML plumbing without driving the state-token
// verification path (state has its own table-driven coverage in auth/state).
func noopVerifier(_, _, _, _ string) error { return nil }
