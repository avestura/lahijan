// Package oidc implements Lahijan's generic OpenID Connect client (WS-07a).
// It is distinct from auth/oauth because OIDC adds two layers on top of plain
// OAuth2:
//
//  1. Discovery: the IdP publishes its endpoints (auth, token, userinfo, JWKS)
//     at /.well-known/openid-configuration. We fetch + cache that document so
//     the deployer only configures the issuer URL.
//  2. ID token verification: the IdP hands back a signed JWT (the id_token)
//     alongside the access_token. We verify its signature (via the IdP's JWKS
//     published keys), its audience (must include our client_id), its expiry,
//     and its nonce (when one was sent in the auth request).
//
// The package is the OIDC layer only — it does NOT touch the database, does
// NOT issue sessions, and does NOT persist tokens. Account linking is the
// auth/idp service's job.
//
// Like auth/oauth, PKCE is enforced on every flow. The state-token contract
// (auth/state) is shared with the OAuth flow; the same browser cookies drive
// both. The provider key stored on user_oauth_identities is namespaced as
// "oidc:<config-key>" so OIDC providers never collide with OAuth presets.
//
// The package depends on github.com/coreos/go-oidc/v3 (Apache-2.0) for the
// heavy lifting (discovery, JWKS caching, JWT signature verification).
package oidc

import (
	"context"
	"errors"
	"fmt"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"golang.org/x/oauth2"

	"github.com/coreos/go-oidc/v3/oidc"
)

// ErrProviderUnknown is returned when an OIDC provider key is not configured.
var ErrProviderUnknown = errors.New("oidc: unknown or disabled provider")

// ErrDiscovery is returned when /.well-known/openid-configuration cannot be
// fetched or parsed. Wrapped around the underlying network / parse error.
var ErrDiscovery = errors.New("oidc: discovery failed")

// ErrIDToken is returned when the id_token handed back by the IdP fails
// verification (bad signature, wrong audience, expired, nonce mismatch).
var ErrIDToken = errors.New("oidc: id_token verification failed")

// Profile is the post-exchange normalized user info. Mirrors oauth.Profile
// so the auth/idp service treats both flows identically downstream.
type Profile = oauth.Profile

// Tokens carries the post-exchange IdP response plus the verified ID token
// claims (sub, email, etc.).
type Tokens struct {
	// OAuth2 layer (access_token, refresh_token, expiry).
	oauth.Tokens
	// IDToken is the raw, verified ID token JWT (for storage / RP-initiated
	// logout later).
	IDToken string
	// Claims carries the verified claims the id_token asserted.
	Claims IDClaims
}

// IDClaims is the subset of standard OIDC claims Lahijan uses. Fields not
// listed here (picture, address, etc.) are intentionally dropped.
type IDClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	// Nonce is the value the client sent in the auth request; verified to
	// match the locally-stored nonce.
	Nonce string `json:"nonce,omitempty"`
}

// ProviderConfig carries the deployer-supplied fields needed to build one
// OIDC provider. Issuer is used for discovery (we fetch
// /.well-known/openid-configuration from <Issuer> + that path).
type ProviderConfig struct {
	Key          string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// stateVerifier is the signature auth/state.Signer.Verify satisfies; the OIDC
// package takes it as-is so it shares the same state-token signing path as
// auth/oauth.
type stateVerifier func(stateToken, cookieNonce, provider, linkUserID string) error

// Provider is one configured OIDC IdP. The contract mirrors oauth.Provider so
// the api handler can use either through the same shape; the only OIDC-only
// surface is the verified IDClaims on the returned Tokens.
type Provider interface {
	Key() string
	// BuildAuthURL returns the IdP authorization URL plus the PKCE code
	// verifier AND a fresh OIDC nonce. The caller MUST persist both as
	// cookies for the callback: the verifier is presented at /token, the
	// nonce is compared against the id_token's nonce claim.
	BuildAuthURL(stateToken string) (authURL, codeVerifier, nonce string, err error)
	VerifyState(stateToken, cookieNonce, linkUserID string) error
	Exchange(ctx context.Context, code, codeVerifier, nonce string) (Tokens, error)
}

// provider is the concrete implementation. The OIDC discovery + JWKS state is
// owned by the underlying *oidc.Provider; we cache one per configured IdP at
// NewProvider time so discovery happens exactly once per provider per process.
type provider struct {
	key           string
	clientID      string
	oidc          *oidc.Provider
	oauth2Cfg     *oauth2.Config
	stateVerifier stateVerifier
}

// NewProvider discovers the IdP's endpoints via /.well-known/openid-configuration
// and returns a Provider bound to them. Discovery happens once here; every
// subsequent Exchange uses the cached endpoints + JWKS keys.
//
// Returns ErrDiscovery (wrapping the underlying cause) on a discovery failure
// so the bootstrap can fail fast with a clear message.
func NewProvider(ctx context.Context, cfg ProviderConfig, verifier stateVerifier) (Provider, error) {
	p, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, errors.Join(ErrDiscovery, err)
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, oidc.ScopeEmail, oidc.ScopeProfile}
	}
	// Force OIDC scope to be present; without it the IdP will not return an
	// id_token, which defeats the entire point of OIDC.
	if !hasOpenIDScope(scopes) {
		scopes = append(scopes, oidc.ScopeOpenID)
	}
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		Endpoint:     p.Endpoint(),
	}
	return &provider{
		key:           cfg.Key,
		clientID:      cfg.ClientID,
		oidc:          p,
		oauth2Cfg:     oc,
		stateVerifier: verifier,
	}, nil
}

// Key implements Provider.
func (p *provider) Key() string { return p.key }

// BuildAuthURL mints the IdP authorization URL with PKCE + the OIDC nonce
// baked in. The nonce is a fresh random string the caller MUST remember (via
// a short-lived cookie) and pass to Exchange for the id_token's nonce claim
// to be verified against. The PKCE code_verifier is returned alongside; the
// caller MUST likewise persist it as a cookie.
//
// The nonce is distinct from the state-token's CSRF nonce: the state-token
// nonce protects against CSRF on the callback URL, the OIDC nonce protects
// against id_token replay. Both are required.
func (p *provider) BuildAuthURL(stateToken string) (string, string, string, error) {
	verifier, err := generateVerifier()
	if err != nil {
		return "", "", "", fmt.Errorf("oidc: pkce verifier: %w", err)
	}
	nonce, err := generateVerifier() // same shape: 43-char base64url random
	if err != nil {
		return "", "", "", fmt.Errorf("oidc: nonce: %w", err)
	}
	url := p.oauth2Cfg.AuthCodeURL(
		stateToken,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	return url, verifier, nonce, nil
}

// VerifyState delegates to the auth/state signer (shared with auth/oauth).
func (p *provider) VerifyState(stateToken, cookieNonce, linkUserID string) error {
	return p.stateVerifier(stateToken, cookieNonce, p.key, linkUserID)
}

// Exchange turns the callback code into IdP tokens AND verifies the id_token:
// signature (via the IdP's JWKS), audience (must include our client_id),
// expiry, and nonce (must equal the value sent in BuildAuthURL).
func (p *provider) Exchange(ctx context.Context, code, codeVerifier, nonce string) (Tokens, error) {
	tok, err := p.oauth2Cfg.Exchange(
		ctx, code,
		oauth2.VerifierOption(codeVerifier),
	)
	if err != nil {
		return Tokens{}, errors.Join(oauth.ErrExchange, err)
	}

	rawID := tok.Extra("id_token")
	if rawID == nil {
		return Tokens{}, fmt.Errorf("%w: id_token missing from token response", ErrIDToken)
	}
	idTokenStr, ok := rawID.(string)
	if !ok || idTokenStr == "" {
		return Tokens{}, fmt.Errorf("%w: id_token is not a string", ErrIDToken)
	}

	verifier := p.oidc.Verifier(&oidc.Config{
		ClientID:          p.clientID,
		SkipClientIDCheck: false,
		SkipExpiryCheck:   false,
	})
	idToken, err := verifier.Verify(ctx, idTokenStr)
	if err != nil {
		return Tokens{}, errors.Join(ErrIDToken, err)
	}

	// Pull the claims Lahijan uses out of the verified token. We do not
	// blanket-unmarshal into IDClaims because go-oidc exposes claims via
	// Claims(struct) which only fills fields with matching JSON tags.
	var claims IDClaims
	if err := idToken.Claims(&claims); err != nil {
		return Tokens{}, errors.Join(ErrIDToken, fmt.Errorf("decode claims: %w", err))
	}

	// Nonce check: if we sent one (always, in BuildAuthURL via state.Issue's
	// nonce), the id_token's nonce must match it. This closes the session
	// fixation window that the auth-code flow opens.
	if nonce != "" {
		if claims.Nonce != nonce {
			return Tokens{}, fmt.Errorf("%w: nonce mismatch", ErrIDToken)
		}
	}

	ot := oauthTokensFrom(tok)
	return Tokens{
		Tokens: oauth.Tokens{
			AccessToken:  ot.AccessToken,
			RefreshToken: ot.RefreshToken,
			Expiry:       ot.Expiry,
			Scope:        ot.Scope,
		},
		IDToken: idTokenStr,
		Claims:  claims,
	}, nil
}

// FetchProfile is a thin convenience: when a caller wants the userinfo
// endpoint's view (which may include more claims than the id_token), they
// call this. For most flows the id_token Claims are sufficient.
//
//nolint:unused // kept for parity with oauth.Provider; used by future consumers.
func (p *provider) FetchProfile(ctx context.Context, tok Tokens) (Profile, error) {
	info, err := p.oidc.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: tok.AccessToken,
		TokenType:   "Bearer",
	}))
	if err != nil {
		return Profile{}, fmt.Errorf("oidc: userinfo: %w", err)
	}
	var claims IDClaims
	if err := info.Claims(&claims); err != nil {
		return Profile{}, fmt.Errorf("oidc: userinfo claims: %w", err)
	}
	return Profile{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		DisplayName:   claims.Name,
	}, nil
}

// hasOpenIDScope reports whether "openid" is in the slice. Scopes are case-
// sensitive per RFC 6749 §3.3; we do not lowercase.
func hasOpenIDScope(scopes []string) bool {
	for _, s := range scopes {
		if s == oidc.ScopeOpenID {
			return true
		}
	}
	return false
}

// oauthTokensFrom copies the public fields of an oauth2.Token into our oauth
// Tokens wrapper. Mirrors the unexported helper in auth/oauth; we duplicate
// it here to avoid expanding oauth's public API for one consumer.
func oauthTokensFrom(t *oauth2.Token) oauth.Tokens {
	out := oauth.Tokens{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
		Scope:        t.TokenType,
	}
	if raw, ok := t.Extra("scope").(string); ok && raw != "" {
		out.Scope = raw
	}
	return out
}

// Registry resolves an OIDC provider by its stable key. Mirrors oauth.Registry
// so the api handler can look up either type through the same shape.
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
func (r *Registry) Lookup(key string) (Provider, error) {
	p, ok := r.providers[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderUnknown, key)
	}
	return p, nil
}

// Keys returns the configured provider keys.
func (r *Registry) Keys() []string {
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k)
	}
	return out
}

// generateVerifier mints a 43-char cryptographically-random code_verifier.
// Mirrors the oauth package's helper; OIDC requires the same PKCE shape.
func generateVerifier() (string, error) {
	return oauth.GenerateVerifier()
}

// Compile-time check that the auth/state Signer satisfies our stateVerifier.
var _ stateVerifier = (*state.Signer)(nil).Verify
