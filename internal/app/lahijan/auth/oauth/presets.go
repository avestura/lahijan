// Package oauth: presets.go builds the concrete OAuth2 provider instances for
// Google, GitHub, and a configurable generic provider. Each preset wires its
// oauth2.Config (auth URL, token URL, profile endpoint) and a profile fetcher
// that turns the IdP's response into the shared Profile struct.
//
// All presets share the same OAuth2 plumbing (PKCE, exchange, scope encoding);
// only the endpoints and the response-shape parser differ.
package oauth

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

// PresetConfig carries the deployer-supplied fields needed to build a
// provider preset. The bootstrap fills this from conf.auth.oauth.providers.*.
type PresetConfig struct {
	Key          string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// StateVerifier is the signature auth/state.Signer.Verify satisfies; the
// presets accept it so they do not import auth/state directly (avoids a
// cycle when state imports secrets, which secrets does not import but
// conventionally we keep each preset decoupled from the signing source).
type StateVerifier = func(stateToken, cookieNonce, provider, linkUserID string) error

// NoopStateVerifier is a StateVerifier that always returns nil. Useful for
// tests that exercise the OAuth plumbing without driving the state-token
// verification path. Production callers wire auth/state.Signer.Verify.
func NoopStateVerifier(stateToken, cookieNonce, provider, linkUserID string) error {
	_ = stateToken
	_ = cookieNonce
	_ = provider
	_ = linkUserID
	return nil
}

// NewGoogle builds the Google OAuth2 provider preset.
//
// Google's userinfo endpoint is https://www.googleapis.com/oauth2/v3/userinfo
// which returns a stable `sub` (the subject we persist), email,
// email_verified, and name. Email_verified is always true for verified
// Google accounts; unverified emails (rare) are surfaced as such.
func NewGoogle(cfg PresetConfig, verifier StateVerifier) Provider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		Endpoint:     google.Endpoint,
	}
	return &provider{
		key:           cfg.Key,
		cfg:           oc,
		stateVerifier: verifier,
		fetchProfile:  fetchGoogleProfile,
	}
}

// googleUserInfo mirrors the JSON shape of the Google userinfo endpoint. Only
// the fields Lahijan uses are decoded; the rest are ignored.
type googleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// fetchGoogleProfile calls the userinfo endpoint and turns the response into
// the shared Profile struct.
func fetchGoogleProfile(ctx context.Context, tok Tokens) (Profile, error) {
	var ui googleUserInfo
	if err := httpGetJSON(ctx,
		"https://www.googleapis.com/oauth2/v3/userinfo", tok.AccessToken, &ui); err != nil {
		return Profile{}, err
	}
	if ui.Sub == "" {
		return Profile{}, errors.New("google userinfo missing sub claim")
	}
	return Profile{
		Subject:       ui.Sub,
		Email:         ui.Email,
		EmailVerified: ui.EmailVerified,
		DisplayName:   ui.Name,
	}, nil
}

// NewGitHub builds the GitHub OAuth2 provider preset.
//
// GitHub's /user endpoint returns a numeric `id` (the stable subject) plus
// login, name, and email. The primary email is often NULL for users who
// hide it; in that case the profile fetcher calls /user/emails and picks the
// primary verified one. If neither path yields an email, Email is left
// blank and EmailVerified stays false — the link/login flow treats that as
// "insufficient profile info" and rejects the callback.
func NewGitHub(cfg PresetConfig, verifier StateVerifier) Provider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"read:user", "user:email"}
	}
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		Endpoint:     github.Endpoint,
	}
	return &provider{
		key:           cfg.Key,
		cfg:           oc,
		stateVerifier: verifier,
		fetchProfile:  fetchGitHubProfile,
	}
}

// githubUser mirrors the JSON shape of the GitHub /user endpoint.
type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// githubEmail mirrors one entry of the GitHub /user/emails array.
type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// fetchGitHubProfile calls /user (and /user/emails when the primary email is
// hidden) and turns the response into the shared Profile struct. The subject
// is the stringified numeric GitHub user id.
func fetchGitHubProfile(ctx context.Context, tok Tokens) (Profile, error) {
	var u githubUser
	if err := httpGetJSON(ctx, "https://api.github.com/user", tok.AccessToken, &u); err != nil {
		return Profile{}, err
	}
	if u.ID == 0 {
		return Profile{}, errors.New("github /user missing id")
	}
	prof := Profile{
		Subject:     strconv.FormatInt(u.ID, 10),
		DisplayName: u.Name,
		Email:       u.Email,
	}
	// GitHub often returns a NULL email for users who hid it; pull the
	// primary verified one from /user/emails.
	if prof.Email == "" {
		var emails []githubEmail
		if err := httpGetJSON(ctx, "https://api.github.com/user/emails", tok.AccessToken, &emails); err == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					prof.Email = e.Email
					prof.EmailVerified = true
					break
				}
			}
		}
	} else {
		// When the primary email was visible, surface verification status by
		// checking /user/emails for the matching entry (best-effort; an
		// unverifiable lookup leaves EmailVerified false).
		var emails []githubEmail
		if err := httpGetJSON(ctx, "https://api.github.com/user/emails", tok.AccessToken, &emails); err == nil {
			for _, e := range emails {
				if strings.EqualFold(e.Email, prof.Email) {
					prof.EmailVerified = e.Verified
					break
				}
			}
		}
	}
	return prof, nil
}

// NewGeneric builds a configurable OAuth2 provider for any IdP that exposes
// standard authorization-code endpoints (auth, token) plus a JSON userinfo
// URL. The profile parser expects a JSON object with at least a "sub" field;
// "email", "email_verified", and "name" are surfaced when present.
//
// This preset is intended for IdPs that are not OIDC-compliant but do follow
// the OAuth2 + Bearer-userinfo pattern. For full OIDC compliance (discovery,
// id_token verification), use the auth/oidc package instead.
func NewGeneric(cfg PresetConfig, verifier StateVerifier, endpoints PresetEndpoints) Provider {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	oc := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  endpoints.AuthURL,
			TokenURL: endpoints.TokenURL,
		},
	}
	return &provider{
		key:           cfg.Key,
		cfg:           oc,
		stateVerifier: verifier,
		fetchProfile:  makeGenericProfileFetcher(endpoints.UserInfoURL),
	}
}

// PresetEndpoints carries the IdP endpoint URLs a generic preset needs. The
// bootstrap reads them from config; the built-in Google/GitHub presets pull
// them from golang.org/x/oauth2 instead.
type PresetEndpoints struct {
	AuthURL     string
	TokenURL    string
	UserInfoURL string
}

// genericUserInfo mirrors the typical OAuth2 userinfo response: a "sub" field
// carrying the stable subject, plus the usual optional email/name fields.
type genericUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// makeGenericProfileFetcher returns a profile fetcher bound to the given
// userinfo URL. Trivially closures over the URL so each generic provider
// instance has its own bound fetcher.
func makeGenericProfileFetcher(userinfoURL string) func(context.Context, Tokens) (Profile, error) {
	return func(ctx context.Context, tok Tokens) (Profile, error) {
		var ui genericUserInfo
		if err := httpGetJSON(ctx, userinfoURL, tok.AccessToken, &ui); err != nil {
			return Profile{}, err
		}
		if ui.Sub == "" {
			return Profile{}, errors.New("generic userinfo missing sub claim")
		}
		return Profile{
			Subject:       ui.Sub,
			Email:         ui.Email,
			EmailVerified: ui.EmailVerified,
			DisplayName:   ui.Name,
		}, nil
	}
}

// Verify StateVerifier signature matches auth/state.Signer.Verify at compile
// time. This guards against drift: if state.Signer.Verify's signature ever
// changes, this build breaks before the provider presets silently break.
var _ StateVerifier = (*state.Signer)(nil).Verify
