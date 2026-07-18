// Package oauth implements Lahijan's generic OAuth 2.0 social-login client
// (WS-07a): authorization-code flow with mandatory PKCE, built-in provider
// presets for Google and GitHub, and a configurable generic provider via
// conf.auth.oauth.providers.*.
//
// The package is the OAuth2 layer only — it does NOT touch the database, does
// NOT issue sessions, and does NOT persist tokens. Account linking is the
// auth/idp service's job; this package's contract is:
//
//   - BuildAuthURL(state)    → redirect the browser to the IdP
//   - Exchange(code, pkce)   → turn the callback code into tokens
//   - FetchProfile(ctx, tok) → turn the access token into a stable (subject, email, name) triple
//
// PKCE (RFC 7636) is enforced on EVERY flow, including the confidential
// client→server presets, per the WS-07a design rule. The code verifier is
// returned alongside the AuthURL and threaded back via a one-shot cookie so
// the callback can present the matching code_challenge to the token endpoint.
//
// The package depends on golang.org/x/oauth2 for the heavy lifting (token
// endpoint requests, PKCE math, scope encoding). License: BSD-3-Clause.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// PKCECookieName is the cookie the start handler sets carrying the PKCE code
// verifier, and the callback reads to complete the token exchange. Lives
// alongside state.CookieName for the duration of the OAuth roundtrip.
const PKCECookieName = "lahijan_oauth_pkce"

// ErrProviderUnknown is returned when a handler is asked to use a provider key
// that is not configured (or not enabled).
var ErrProviderUnknown = errors.New("oauth: unknown or disabled provider")

// ErrExchange is returned when the IdP token endpoint rejects the auth code.
// The wrapped cause carries the IdP's error_description when available.
var ErrExchange = errors.New("oauth: code exchange failed")

// ErrProfile is returned when the IdP's userinfo / profile endpoint cannot be
// reached or returns a body we cannot parse.
var ErrProfile = errors.New("oauth: profile fetch failed")

// Profile is the post-exchange normalized user info from the IdP. The Subject
// is the only field guaranteed to be present; Email / DisplayName are best-
// effort and may be empty for private accounts. EmailVerified reflects what
// the IdP says (Google: always true; GitHub: derived from /user/emails).
type Profile struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}

// Tokens is the post-exchange IdP response. Only the fields Lahijan uses are
// surfaced — the underlying oauth2.Token is wrapped so callers do not need to
// import golang.org/x/oauth2 directly.
type Tokens struct {
	AccessToken  string
	RefreshToken string // may be empty when the IdP did not grant offline_access
	Expiry       time.Time
	Scope        string // space-separated scope strings
	Extra        map[string]any
}

// Provider is one configured OAuth2 social-login provider. Concrete presets
// (Google, GitHub) implement it; the api handler resolves a provider by name
// through Registry.
type Provider interface {
	// Key is the stable identifier in URL paths and user_oauth_identities
	// (e.g. "google", "github").
	Key() string

	// BuildAuthURL mints the IdP authorization URL the browser is redirected
	// to. The state token (signed by auth/state) and a fresh PKCE code
	// verifier are returned alongside; the caller MUST set both as cookies
	// (PKCECookieName + state.CookieName) and embed state in the redirect URL.
	BuildAuthURL(stateToken string) (authURL, codeVerifier string, err error)

	// VerifyState is a thin wrapper over auth/state.Signer.Verify so the
	// provider's wiring code is in one place. The handler passes the values
	// pulled from the callback request (query state, cookie nonce, path
	// provider, link uid).
	VerifyState(stateToken, cookieNonce, linkUserID string) error

	// Exchange turns the callback code + PKCE verifier into IdP tokens.
	Exchange(ctx context.Context, code, codeVerifier string) (Tokens, error)

	// FetchProfile uses the access token to fetch the normalized user info.
	FetchProfile(ctx context.Context, tok Tokens) (Profile, error)
}

// provider is the concrete implementation backing every Provider preset.
// Google and GitHub differ only in their config + profile fetcher; the OAuth2
// plumbing (BuildAuthURL with PKCE, Exchange, scope encoding) is identical.
type provider struct {
	key           string
	cfg           *oauth2.Config
	fetchProfile  func(ctx context.Context, tok Tokens) (Profile, error)
	stateVerifier func(stateToken, cookieNonce, provider, linkUserID string) error
}

// Key implements Provider.
func (p *provider) Key() string { return p.key }

// BuildAuthURL mints the IdP authorization URL with PKCE baked into the URL
// params. The returned codeVerifier is what the start handler sets as the
// PKCE cookie and what the callback handler passes back to Exchange.
//
// AuthStyle is intentionally NOT passed here: it is set on the Endpoint
// (google.Endpoint.AuthStyle = AuthStyleInParams, github.Endpoint.AuthStyle =
// AuthStyleInHeader) which is the canonical place for it.
func (p *provider) BuildAuthURL(stateToken string) (string, string, error) {
	verifier, err := generateVerifier()
	if err != nil {
		return "", "", fmt.Errorf("oauth: pkce verifier: %w", err)
	}
	url := p.cfg.AuthCodeURL(
		stateToken,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
	)
	return url, verifier, nil
}

// VerifyState delegates to the auth/state signer so the provider remains the
// single wiring point for OAuth callback validation. It re-binds the provider
// key from the path so a callback to /callback?provider=X cannot be replayed
// against /callback?provider=Y.
func (p *provider) VerifyState(stateToken, cookieNonce, linkUserID string) error {
	return p.stateVerifier(stateToken, cookieNonce, p.key, linkUserID)
}

// Exchange turns the auth code into tokens via the IdP token endpoint, with
// the PKCE verifier presented alongside.
func (p *provider) Exchange(ctx context.Context, code, codeVerifier string) (Tokens, error) {
	tok, err := p.cfg.Exchange(
		ctx, code,
		oauth2.VerifierOption(codeVerifier),
	)
	if err != nil {
		// Join so callers can errors.Is(result, ErrExchange) AND pull the
		// underlying IdP error out via errors.As / errors.Is.
		return Tokens{}, errors.Join(ErrExchange, err)
	}
	return tokensFromOAuth2(tok), nil
}

// FetchProfile delegates to the provider-specific profile fetcher.
func (p *provider) FetchProfile(ctx context.Context, tok Tokens) (Profile, error) {
	prof, err := p.fetchProfile(ctx, tok)
	if err != nil {
		return Profile{}, errors.Join(ErrProfile, err)
	}
	return prof, nil
}

func tokensFromOAuth2(t *oauth2.Token) Tokens {
	out := Tokens{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
		Scope:        t.TokenType,
		Extra:        nil,
	}
	if raw, ok := t.Extra("scope").(string); ok && raw != "" {
		out.Scope = raw
	}
	return out
}

// generateVerifier mints a 43-128 char cryptographically-random code_verifier
// per RFC 7636 §4.1. We use 32 random bytes (base64url-encoded to 43 chars)
// — the minimum length — because the verifier is single-use and travels in a
// short-lived cookie.
func generateVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Registry resolves a provider by its stable key (the path param). The api
// handler uses it so adding a new provider does not require touching the
// router.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry builds a registry over the given providers. Keys are derived
// from Provider.Key(); duplicate keys silently overwrite (programmer error;
// validated at bootstrap by the conf layer).
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

// Keys returns the configured provider keys. Used by the bootstrap to assert
// at least one provider is wired before the routes are mounted.
func (r *Registry) Keys() []string {
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k)
	}
	return out
}

// httpGetJSON is a small helper that GETs the URL with the bearer token and
// unmarshals the JSON body into out. Shared by the Google and GitHub profile
// fetchers.
func httpGetJSON(ctx context.Context, url, bearer string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
