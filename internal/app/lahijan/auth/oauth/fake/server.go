// Package fake provides an in-process OAuth2 + userinfo fake server used by
// the WS-07a integration tests. It speaks the OAuth2 + userinfo shape that
// Lahijan's auth/oauth + auth/oidc packages expect, with no real crypto:
// tokens are recognisable deterministic strings the tests assert on.
//
// The fake lives in its own subpackage so the production auth/oauth package
// never imports net/http/httptest. Tests import it as needed; production
// code never does.
package fake

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

// Server is an in-process OAuth2 + userinfo fake. Start one per test, drive
// a provider preset against its URL, then Close in t.Cleanup.
type Server struct {
	HTTP *httptest.Server

	// ProfileResponse is the JSON body the fake's /userinfo endpoint returns.
	// Tests override per-case to exercise different IdP responses (missing
	// sub, hidden email, unverified email, etc.). Default is a verified
	// subject "sub-42" + email + name.
	ProfileResponse any

	// AuthCode is the value the fake's /auth endpoint hands back as ?code=.
	AuthCode string

	// AccessToken + RefreshToken are the values the fake's /token endpoint
	// returns for every exchange. Tests that need to assert "the right code
	// was exchanged with the right PKCE verifier" read MintedExchanges.
	AccessToken  string
	RefreshToken string

	mu        sync.Mutex
	exchanges []Exchange
}

// Exchange is one row in the fake's minted-tokens ledger, captured on every
// POST /token the fake services.
type Exchange struct {
	Code          string
	Verifier      string
	RedirectURI   string
	ClientID      string
	ClientSecret  string
	Scope         string
	Authorization string // Authorization header value, if presented
}

// New starts a fresh fake server. Tests should defer s.Close().
func New() *Server {
	s := &Server{
		AuthCode:        "fake-auth-code",
		AccessToken:     "fake-access-token",
		RefreshToken:    "fake-refresh-token",
		ProfileResponse: Profile{Sub: "sub-42", Email: "user@example.test", EmailVerified: true, Name: "Fake User"},
	}
	mux := http.NewServeMux()

	// GET /auth — the IdP authorization endpoint. The OAuth client redirects
	// the browser here with ?client_id, ?redirect_uri, ?state, ?scope,
	// ?code_challenge, ?code_challenge_method, ?response_type. The fake
	// ignores everything except redirect_uri + state and 302s the browser
	// back to redirect_uri?code=<AuthCode>&state=<echoed state>.
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		redirect := r.URL.Query().Get("redirect_uri")
		q := url.Values{}
		q.Set("code", s.AuthCode)
		if state != "" {
			q.Set("state", state)
		}
		http.Redirect(w, r, redirect+"?"+q.Encode(), http.StatusFound)
	})

	// POST /token — the IdP token endpoint. Accepts the standard OAuth2
	// token request (grant_type=authorization_code, code, redirect_uri,
	// client_id, client_secret, code_verifier). Client credentials may be
	// sent either as form params (AuthStyleInParams) or via HTTP Basic Auth
	// (AuthStyleInHeader); the fake normalises both into Exchange.ClientID
	// / ClientSecret so tests can assert on either transport.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		clientID := r.FormValue("client_id")
		clientSecret := r.FormValue("client_secret")
		if clientID == "" || clientSecret == "" {
			if u, p, ok := parseBasicAuth(r.Header.Get("Authorization")); ok {
				clientID = u
				clientSecret = p
			}
		}
		s.mu.Lock()
		s.exchanges = append(s.exchanges, Exchange{
			Code:          r.FormValue("code"),
			Verifier:      r.FormValue("code_verifier"),
			RedirectURI:   r.FormValue("redirect_uri"),
			ClientID:      clientID,
			ClientSecret:  clientSecret,
			Scope:         r.FormValue("scope"),
			Authorization: r.Header.Get("Authorization"),
		})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expires_in":3600,"scope":%q}`,
			s.AccessToken, s.RefreshToken, r.FormValue("scope"))
	})

	// GET /userinfo — bearer-token-protected profile endpoint. Echoes
	// ProfileResponse as JSON.
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(s.ProfileResponse); err != nil {
			http.Error(w, "encode failed", http.StatusInternalServerError)
		}
	})

	s.HTTP = httptest.NewServer(mux)
	return s
}

// Close stops the fake's httptest server.
func (s *Server) Close() { s.HTTP.Close() }

// MintedExchanges returns a snapshot of every code→token exchange the fake
// has serviced.
func (s *Server) MintedExchanges() []Exchange {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Exchange, len(s.exchanges))
	copy(out, s.exchanges)
	return out
}

// Profile is the JSON shape the fake's /userinfo endpoint returns by default.
// It matches the oauth.Profile struct field-for-field so the test fixtures
// read naturally.
type Profile struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// BaseURL returns the fake's root URL (no trailing slash).
func (s *Server) BaseURL() string { return strings.TrimRight(s.HTTP.URL, "/") }

// AuthURL is the absolute URL the OAuth client redirects the browser to.
func (s *Server) AuthURL() string { return s.BaseURL() + "/auth" }

// TokenURL is the absolute URL the OAuth client POSTs the code exchange to.
func (s *Server) TokenURL() string { return s.BaseURL() + "/token" }

// UserInfoURL is the absolute URL the OAuth client GETs the profile from.
func (s *Server) UserInfoURL() string { return s.BaseURL() + "/userinfo" }

// parseBasicAuth splits an "Authorization: Basic <base64>" header into its
// (user, pass) pair. Returns ok=false when the header is absent or malformed.
// The fake uses this so tests can assert on client_id/secret regardless of
// whether the OAuth2 client used AuthStyleInParams or AuthStyleInHeader.
func parseBasicAuth(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", false
	}
	dec, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(dec), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
