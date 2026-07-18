// Package state produces and verifies the signed CSRF state token used in the
// OAuth/OIDC authorization-code flows (WS-07a). The state token travels in the
// browser query string on the outbound IdP redirect and is echoed back by the
// IdP on the return trip; verifying it proves the callback came from a flow
// WE started, closing the CSRF window the OAuth/OIDC redirect flow opens.
//
// State format (compact, signed):
//
//	<payload-base64url>.<hmac-base64url>
//
// Payload is a tiny JSON blob carrying:
//
//	{
//	  "nonce": "<16-byte hex>",     // fresh per request; the cookie value the
//	                                // browser keeps and posts back
//	  "provider": "google",         // echoes the path param back so a callback
//	                                // to /callback?provider=X cannot be replayed
//	                                // against /callback?provider=Y
//	  "link_uid": "...",            // optional; present only when a logged-in
//	                                // user is LINKING a new IdP (vs anonymous
//	                                // login). The callback enforces it matches.
//	  "created_at": <unix-seconds>
//	}
//
// The HMAC is computed with the same key used by auth/secrets.Signer so there
// is exactly one signing key in the system. A 10-minute TTL guards against
// indefinite replay. The token's nonce is also placed in a short-lived cookie
// the callback reads and compares; this is the OAuth2 "state" double-submit
// cookie pattern.
package state

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
)

// ErrInvalid is returned when a state token fails signature verification, is
// malformed, has expired, or does not match the request context (provider,
// link_uid, nonce). The handler translates this into a 400 envelope; the
// shape of the failure is intentionally opaque.
var ErrInvalid = errors.New("state: invalid or expired state token")

// TTL bounds how long a single outbound IdP redirect may sit on the user's
// browser before the callback must come back. Ten minutes is generous for
// interactive login yet short enough that a leaked state cannot be replayed
// indefinitely.
const TTL = 10 * time.Minute

// CookieName is the cookie the start handler sets and the callback reads to
// perform the double-submit check. Same name for OAuth and OIDC since the two
// flows never run concurrently for the same browser.
const CookieName = "lahijan_oauth_state"

// Claims carries the payload the start handler bakes into the state token.
// LinkUserID is optional; nil for anonymous "log in with X", set when a
// logged-in user is "linking X to my account".
type Claims struct {
	Nonce      string    `json:"nonce"`
	Provider   string    `json:"provider"`
	LinkUserID string    `json:"link_uid,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Signer issues and verifies state tokens. It wraps auth/secrets.Signer so
// the OAuth/OIDC flow shares the process-wide signing key without re-reading
// configuration.
type Signer struct {
	key []byte
}

// NewSigner builds a state-token signer over the same key as auth/secrets.
// Passing the existing *secrets.Signer keeps a single source of truth for the
// signing key in the system.
func NewSigner(s *secrets.Signer) *Signer {
	return &Signer{key: secrets.ExportKey(s)}
}

// Issue produces a fresh signed state token + the raw nonce value to set as
// the double-submit cookie. Callers MUST set CookieName=nonce in the response
// alongside redirecting the user to the IdP with ?state=token.
func (s *Signer) Issue(provider, linkUserID string) (token, nonce string, err error) {
	raw := make([]byte, 16)
	if _, rErr := rand.Read(raw); rErr != nil {
		return "", "", fmt.Errorf("state: read nonce: %w", rErr)
	}
	nonce = hex.EncodeToString(raw)
	c := Claims{
		Nonce:      nonce,
		Provider:   provider,
		LinkUserID: linkUserID,
		CreatedAt:  time.Now(),
	}
	payload, mErr := json.Marshal(c)
	if mErr != nil {
		return "", "", fmt.Errorf("state: marshal claims: %w", mErr)
	}
	token = s.encode(payload)
	return token, nonce, nil
}

// Verify validates a state token returned by the IdP against the request
// context. cookieNonce is the value of CookieName on the callback request;
// it must equal the nonce baked into the token (double-submit check).
// provider is the path param so a token issued for one provider cannot be
// replayed against another. linkUserID is empty for the anonymous login flow
// and the user's id when a logged-in user is linking a new IdP; it must
// equal the token's link_uid.
func (s *Signer) Verify(token, cookieNonce, provider, linkUserID string) error {
	payload, ok := decodeAndVerify(s.key, token)
	if !ok {
		return ErrInvalid
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return ErrInvalid
	}
	if d := time.Since(c.CreatedAt); d > TTL || d < -TTL {
		// The negative-side check guards against a clock-skewed token dated
		// too far in the future; legitimate issuers will be within seconds
		// of now, so a >TTL skew is treated as tampering.
		return ErrInvalid
	}
	if c.Nonce == "" || c.Nonce != cookieNonce {
		return ErrInvalid
	}
	if c.Provider == "" || c.Provider != provider {
		return ErrInvalid
	}
	if c.LinkUserID != linkUserID {
		return ErrInvalid
	}
	return nil
}

// encode produces base64url(payload).base64url(hmac) for the given payload.
func (s *Signer) encode(payload []byte) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return b64(payload) + "." + b64(sig)
}

// decodeAndVerify splits the token, recomputes the HMAC over the payload, and
// returns the payload only if the signatures match (constant-time). Malformed
// tokens or signature mismatches return ok=false.
func decodeAndVerify(key []byte, token string) ([]byte, bool) {
	dot := -1
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			dot = i
			break
		}
	}
	if dot <= 0 || dot == len(token)-1 {
		return nil, false
	}
	payload, err := b64decode(token[:dot])
	if err != nil {
		return nil, false
	}
	gotSig, err := b64decode(token[dot+1:])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	wantSig := mac.Sum(nil)
	if !hmac.Equal(gotSig, wantSig) {
		return nil, false
	}
	return payload, true
}

// b64 is base64.RawURLEncoding without padding, suitable for URLs and cookies.
func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func b64decode(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
