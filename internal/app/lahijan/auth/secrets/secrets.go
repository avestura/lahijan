// Package secrets produces and verifies Lahijan's signed opaque tokens: the
// session cookie value, the refresh-token cookie value, raw personal access
// tokens, and email-link tokens (WS-06).
//
// Token format (uniform across all uses):
//
//	<base64url(payload)>.<base64url(hmac)>
//
// where hmac = HMAC-SHA256(signing_key, payload)[full digest]. The SHA-256
// digest of the payload is what gets stored in the database (sessions.token_hash,
// refresh_tokens.token_hash, ...); the raw token never lives at rest.
//
// Why HMAC-sign on top of a hash lookup?
//   - An attacker who exfiltrates the database (token hashes only) still cannot
//     forge a token without the signing key.
//   - We can reject tampered tokens with a cheap HMAC check before any DB hit.
package secrets

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrInvalidToken is returned when a token's signature does not verify or its
// shape is malformed. Callers translate it into the localizable "invalid token"
// message.
var ErrInvalidToken = errors.New("secrets: token signature invalid or malformed")

// Signer issues and verifies HMAC-signed opaque tokens using a process-wide
// signing key. The key must be at least 16 bytes; in dev an empty key is
// replaced by a fixed warning-prefixed value so misconfiguration is loud.
type Signer struct {
	key []byte
}

// NewSigner builds a Signer from a raw key. An empty key falls back to a
// deterministic dev value so local dev "just works" without configuration;
// program.Start rejects an empty key in non-dev environments (see conf).
func NewSigner(key string) *Signer {
	k := []byte(key)
	if len(k) == 0 {
		k = []byte("DEV-ONLY-INSECURE-CHANGE-ME-lahijan-signing-key")
	}
	return &Signer{key: k}
}

// Issue generates byteLength cryptographically-random bytes and returns:
//
//	raw  - the signed token string to hand to the client (cookie/PAT/email link)
//	hash - the SHA-256 hex digest to persist as the row's token_hash
//
// The raw token is base64url(payload).base64url(hmac). Callers MUST NOT log raw.
func (s *Signer) Issue(byteLength int) (raw, hash string, err error) {
	if byteLength < 16 {
		byteLength = 32
	}
	payload := make([]byte, byteLength)
	if _, err := rand.Read(payload); err != nil {
		return "", "", fmt.Errorf("secrets: read random: %w", err)
	}
	raw = s.encode(payload)
	return raw, hashPayload(payload), nil
}

// Verify checks the HMAC signature of raw and, on success, returns the SHA-256
// hex digest of the payload (the value to look up in the database). A failed
// signature or malformed token returns ErrInvalidToken.
func (s *Signer) Verify(raw string) (hash string, err error) {
	payload, ok := decodeAndVerify(s.key, raw)
	if !ok {
		return "", ErrInvalidToken
	}
	return hashPayload(payload), nil
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

// hashPayload returns the lowercase hex SHA-256 digest of payload, which is the
// value stored in the database's token_hash columns.
func hashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// b64 is base64.RawURLEncoding without padding, suitable for cookies and URLs.
func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
