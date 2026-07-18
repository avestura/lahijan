// Package secrets: crypto.go provides the AES-GCM envelope used to encrypt
// external IdP tokens (OAuth access_token / refresh_token, OIDC id_token) at
// rest. The ciphertext columns of user_oauth_identities hold the output of
// Crypto.Seal; the plaintext never lives in the DB.
//
// Wire format: base64(nonce || ciphertext || tag). The nonce is 12 bytes
// (GCM standard); the tag is 16 bytes appended to the ciphertext by Go's
// crypto/cipher GCM Sealing. A 32-byte key (AES-256) is required.
//
// Why a separate envelope from the existing HMAC-signed opaque tokens?
//   - The opaque session/refresh/PAT tokens are random bytes the platform
//     itself mints, so we store only their SHA-256 hash and verify by
//     regenerating from the raw token.
//   - External IdP tokens are minted by the IdP, the platform must present
//     them verbatim when calling the IdP, and we may need to refresh them
//     server-side (offline access). A symmetric authenticated-encryption
//     envelope (AES-GCM) lets us recover the plaintext with the encryption
//     key while protecting against DB-only exfiltration.
//
// The encryption key is process-wide, sourced from
// conf.GetAuthSecretsEncryptionKey (env LAHIJAN_AUTH_SECRETS_ENCRYPTION_KEY).
// Key rotation is a future WS; today a single active key covers all rows.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrCryptoInvalid is returned when a ciphertext cannot be decrypted because
// it is malformed, the key is wrong, or the nonce/tag failed to authenticate.
// Callers translate it into the localizable "internal error" message; the
// shape (malformed vs wrong key) is deliberately indistinguishable to avoid
// leaking which one.
var ErrCryptoInvalid = errors.New("secrets: ciphertext invalid or key mismatch")

// Crypto AES-GCM-encrypts and decrypts short strings (OAuth/OIDC tokens) using
// a 32-byte process-wide key. Safe for concurrent use: AES-GCM does not carry
// per-instance mutable state, and the nonce is fresh on every Seal call.
type Crypto struct {
	aead cipher.AEAD
}

// NewCrypto builds an AES-256-GCM Crypto from a raw key. The key MUST be 32
// bytes; any other length is a programmer error and panics loud rather than
// degrading to a weaker cipher.
func NewCrypto(key []byte) (*Crypto, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: AES-256-GCM key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: init AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: init GCM: %w", err)
	}
	return &Crypto{aead: aead}, nil
}

// Seal encrypts plaintext under the process-wide key and returns
// base64(nonce || ciphertext || tag). An empty plaintext is allowed (it round-
// trips through Open as ""); a nil plaintext returns "" without touching the
// AEAD so callers can pass (*string)(nil) straight through.
func (c *Crypto) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secrets: read nonce: %w", err)
	}
	// Sealing appends the ciphertext+tag to the nonce so the wire format is a
	// single self-contained blob.
	out := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal under the same key. Returns "" for
// an empty input (the symmetric pair of Seal's empty-input path). A
// malformed blob or wrong key yields ErrCryptoInvalid.
func (c *Crypto) Open(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrCryptoInvalid
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns+c.aead.Overhead() {
		return "", ErrCryptoInvalid
	}
	nonce, body := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", ErrCryptoInvalid
	}
	return string(plain), nil
}

// SealPtr is the *string form of Seal: it returns a pointer to the ciphertext
// when plaintext is non-nil, or nil otherwise. The symmetric OpenPtr helper
// recovers the plaintext pointer.
func (c *Crypto) SealPtr(plaintext *string) (*string, error) {
	if plaintext == nil {
		//nolint:nilnil // (nil, nil) is the intentional symmetric pair for a
		// nil input: callers thread *string end-to-end and want a nil column
		// back when there was nothing to encrypt.
		return nil, nil
	}
	out, err := c.Seal(*plaintext)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// OpenPtr is the *string form of Open. nil input yields nil output without
// touching the AEAD.
func (c *Crypto) OpenPtr(ciphertext *string) (*string, error) {
	if ciphertext == nil {
		//nolint:nilnil // (nil, nil) is the intentional symmetric pair for a
		// nil input: callers thread *string end-to-end and want a nil column
		// back when there was nothing to decrypt.
		return nil, nil
	}
	out, err := c.Open(*ciphertext)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
