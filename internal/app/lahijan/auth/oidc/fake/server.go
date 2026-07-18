// Package fake provides an in-process OIDC provider for the WS-07a integration
// tests. It composes coreos/go-oidc/v3/oidctest (which handles discovery +
// JWKS publishing) with the /auth, /token, and /userinfo handlers an OIDC
// client needs to drive the full flow.
//
// The fake signs id_tokens with a real RSA key (oidctest.SignIDToken), so
// Lahijan's id_token verification path (JWKS fetch + signature check +
// audience check + expiry check) is exercised end to end.
package fake

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
)

// Server is an in-process OIDC provider. Start one per test, drive a Provider
// preset against its URL, then Close in t.Cleanup.
type Server struct {
	HTTP *httptest.Server

	// Key is the RSA private key the fake uses to sign id_tokens. The
	// matching public key is published at /keys via oidctest.
	Key *rsa.PrivateKey

	// KeyID is the kid the fake advertises in /keys and on every id_token
	// header. Stable for the lifetime of the Server.
	KeyID string

	// Issuer is the OIDC issuer URL (= Server.URL). Set after the httptest
	// server starts so the discovery doc lands at the right place.
	Issuer string

	// AuthCode is the value the /auth handler hands back as ?code=. Tests
	// override per-case to drive deterministic flows.
	AuthCode string

	// Subject/Email/Name are the claims baked into every id_token. Override
	// per-case to exercise different IdP responses (verified vs unverified
	// email, missing name, etc.).
	Subject       string
	Email         string
	EmailVerified bool
	Name          string

	// NonceCaptured is the value the client passed as `nonce` in the /auth
	// request, captured so the test can assert the id_token echoes it back.
	mu            sync.Mutex
	nonceCaptured string
	exchanges     int
}

// New starts a fresh OIDC fake server. Tests should defer s.Close().
//
// Panics on a key-generation failure (only test setup; acceptable per
// oidctest's own convention).
func New() *Server {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("oidc/fake: rsa keygen: %v", err))
	}
	s := &Server{
		Key:           priv,
		KeyID:         "test-key-1",
		AuthCode:      "fake-oidc-code",
		Subject:       "oidc-sub-42",
		Email:         "oidc-user@example.test",
		EmailVerified: true,
		Name:          "OIDC User",
	}

	// oidctest.Server handles /.well-known/openid-configuration + /keys.
	// We compose it with our own /auth, /token, /userinfo handlers.
	inner := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{
			{PublicKey: &priv.PublicKey, KeyID: s.KeyID, Algorithm: string(oidc.RS256)},
		},
	}
	mux := http.NewServeMux()
	mux.Handle("/", inner) // both discovery + /keys hit inner via path match

	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		s.mu.Lock()
		s.nonceCaptured = q.Get("nonce")
		s.mu.Unlock()
		redirect := q.Get("redirect_uri")
		state := q.Get("state")
		out := url.Values{}
		out.Set("code", s.AuthCode)
		if state != "" {
			out.Set("state", state)
		}
		http.Redirect(w, r, redirect+"?"+out.Encode(), http.StatusFound)
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		// Client credentials may be sent either as form params or via HTTP
		// Basic Auth (oauth2's AuthStyleAutoDetect picks the latter when the
		// discovery doc does not advertise token_endpoint_auth_methods).
		// Normalise both into clientID so the id_token's aud matches what
		// the verifier expects regardless of transport.
		clientID := r.FormValue("client_id")
		if clientID == "" {
			if u, _, ok := parseBasicAuth(r.Header.Get("Authorization")); ok {
				clientID = u
			}
		}
		// Build the id_token claims. iss + aud are REQUIRED for go-oidc's
		// verifier to accept the token; sub + exp + iat + nonce + email +
		// email_verified + name are what Lahijan reads.
		claims := map[string]any{
			"iss":            s.Issuer,
			"aud":            clientID,
			"sub":            s.Subject,
			"exp":            0, // overwritten below
			"iat":            0,
			"email":          s.Email,
			"email_verified": s.EmailVerified,
			"name":           s.Name,
			"nonce":          s.NonceCaptured(),
		}
		// Hand-roll the timestamp population because map[string]any cannot
		// hold json.RawMessage-style values cleanly. The marshaller turns
		// int64 into a JSON number, which is what the JWT spec wants.
		fillTimestamps(claims)
		raw, err := json.Marshal(claims)
		if err != nil {
			http.Error(w, "marshal claims: "+err.Error(), http.StatusInternalServerError)
			return
		}
		idToken := oidctest.SignIDToken(s.Key, s.KeyID, string(oidc.RS256), string(raw))

		s.mu.Lock()
		s.exchanges++
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w,
			`{"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expires_in":3600,"id_token":%q,"scope":"openid email profile"}`,
			"fake-oidc-access-token", "fake-oidc-refresh-token", idToken)
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":            s.Subject,
			"email":          s.Email,
			"email_verified": s.EmailVerified,
			"name":           s.Name,
		})
	})

	s.HTTP = httptest.NewServer(mux)
	s.Issuer = s.HTTP.URL
	inner.SetIssuer(s.Issuer)
	return s
}

// Close stops the fake's httptest server.
func (s *Server) Close() { s.HTTP.Close() }

// NonceCaptured returns the nonce the client sent in the /auth request. The
// token handler echoes this back into the id_token's nonce claim.
func (s *Server) NonceCaptured() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nonceCaptured
}

// ExchangeCount returns the number of /token requests serviced. Tests assert
// on this to verify the code exchange happened exactly once.
func (s *Server) ExchangeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exchanges
}

// BaseURL returns the fake's root URL (no trailing slash).
func (s *Server) BaseURL() string { return strings.TrimRight(s.HTTP.URL, "/") }

// fillTimestamps sets the JWT iat/exp claims to a 1-hour window starting now.
// Used by the /token handler so go-oidc's expiry check (which the OIDC
// provider does NOT skip) accepts the token.
func fillTimestamps(claims map[string]any) {
	now := time.Now().Unix()
	claims["iat"] = now
	claims["exp"] = now + 3600
	claims["auth_time"] = now
}

// parseBasicAuth splits an "Authorization: Basic <base64>" header into its
// (user, pass) pair. Returns ok=false when the header is absent or malformed.
// Used by the /token handler so the fake can extract client_id from either
// transport (form params or HTTP Basic Auth).
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
