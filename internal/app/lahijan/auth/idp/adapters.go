// Package idp: adapters.go wraps the oauth.Provider and oidc.Provider so
// both satisfy the service's ExternalIDP interface. The OAuth adapter is a
// trivial pass-through; the OIDC adapter holds the nonce between BuildAuthURL
// and Exchange (the OIDC Exchange signature requires it).
//
// SAML does not satisfy ExternalIDP — its flow is materially different (POST
// binding vs authorization-code, NameID vs subject, no tokens to encrypt). The
// api handler drives SAML through LinkSAML directly rather than through the
// ExternalIDP adapter, keeping the OAuth/OIDC adapter surface small.
package idp

import (
	"context"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oidc"
)

// OAuthAdapter wraps an oauth.Provider so it satisfies ExternalIDP. The
// nonce argument to Exchange is ignored (OAuth2 has no replay-protection
// nonce; only OIDC does).
type OAuthAdapter struct {
	P oauth.Provider
}

// Key implements ExternalIDP.
func (a *OAuthAdapter) Key() string { return a.P.Key() }

// Exchange implements ExternalIDP. Calls oauth.Provider.Exchange and adapts
// the result + a separate FetchProfile into the (Tokens, Profile) pair the
// service expects.
func (a *OAuthAdapter) Exchange(ctx context.Context, code, codeVerifier string) (Tokens, Profile, error) {
	tok, err := a.P.Exchange(ctx, code, codeVerifier)
	if err != nil {
		return Tokens{}, Profile{}, err
	}
	prof, err := a.P.FetchProfile(ctx, tok)
	if err != nil {
		return Tokens{}, Profile{}, err
	}
	return adaptTokens(tok), adaptProfile(prof), nil
}

// OIDCAdapter wraps an oidc.Provider so it satisfies ExternalIDP. The nonce
// captured at BuildAuthURL is passed through to Exchange so the id_token's
// nonce claim can be verified.
type OIDCAdapter struct {
	P     oidc.Provider
	Nonce string
}

// Key implements ExternalIDP. The service stores the OIDC provider's key
// under the "oidc:<key>" namespace so it never collides with OAuth presets.
func (a *OIDCAdapter) Key() string { return "oidc:" + a.P.Key() }

// Exchange implements ExternalIDP. Calls oidc.Provider.Exchange with the
// stored nonce; the profile is read from the verified id_token claims (no
// separate /userinfo round-trip).
func (a *OIDCAdapter) Exchange(ctx context.Context, code, codeVerifier string) (Tokens, Profile, error) {
	tok, err := a.P.Exchange(ctx, code, codeVerifier, a.Nonce)
	if err != nil {
		return Tokens{}, Profile{}, err
	}
	return adaptOIDCTokens(tok), Profile{
		Subject:       tok.Claims.Subject,
		Email:         tok.Claims.Email,
		EmailVerified: tok.Claims.EmailVerified,
		DisplayName:   tok.Claims.Name,
	}, nil
}

// adaptTokens copies the oauth.Tokens shape into the service's idp.Tokens.
// The service's surface is intentionally minimal so future IdP backends
// (SAML in WS-07b) can satisfy it without dragging OAuth2-specific types.
func adaptTokens(t oauth.Tokens) Tokens {
	return Tokens{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
		Scope:        t.Scope,
	}
}

// adaptOIDCTokens mirrors adaptTokens but starts from the OIDC-specific
// Tokens shape (which carries IDToken + Claims the service does not surface).
func adaptOIDCTokens(t oidc.Tokens) Tokens {
	return Tokens{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
		Scope:        t.Scope,
	}
}

// adaptProfile copies the oauth.Profile shape into the service's idp.Profile
// type. Identity is structural — no behaviour — so the conversion is trivial.
func adaptProfile(p oauth.Profile) Profile {
	return Profile{
		Subject:       p.Subject,
		Email:         p.Email,
		EmailVerified: p.EmailVerified,
		DisplayName:   p.DisplayName,
	}
}
