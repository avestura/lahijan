// crypto_test.go exercises the AES-GCM envelope: round-trip, wrong-key
// rejection, tamper detection, empty-input handling, and the *string helpers
// used by the IdP service. Pure logic, no DB.
package secrets

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validKey is a 32-byte AES-256 key derived from a fixed phrase; stable across
// runs so the wrong-key test can use a derivable different key.
var validKey = sha256.Sum256([]byte("lahijan-test-encryption-key"))

// wrongKey is a different 32-byte key used to assert Open fails.
var wrongKey = sha256.Sum256([]byte("lahijan-OTHER-encryption-key"))

func TestCrypto_RoundTrip(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(validKey[:])
	require.NoError(t, err)

	ct, err := c.Seal("ya29.example-access-token")
	require.NoError(t, err)
	assert.NotEqual(t, "ya29.example-access-token", ct, "ciphertext must not equal plaintext")
	assert.NotContains(t, ct, "ya29", "ciphertext must not contain the plaintext as a substring")

	pt, err := c.Open(ct)
	require.NoError(t, err)
	assert.Equal(t, "ya29.example-access-token", pt)
}

func TestCrypto_SealIsRandomised(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(validKey[:])
	require.NoError(t, err)

	a, err := c.Seal("same plaintext")
	require.NoError(t, err)
	b, err := c.Seal("same plaintext")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "Seal must produce a fresh nonce + ciphertext each call")

	// Both must decrypt to the same plaintext.
	pa, err := c.Open(a)
	require.NoError(t, err)
	pb, err := c.Open(b)
	require.NoError(t, err)
	assert.Equal(t, pa, pb)
}

func TestCrypto_WrongKeyFails(t *testing.T) {
	t.Parallel()
	enc, err := NewCrypto(validKey[:])
	require.NoError(t, err)
	dec, err := NewCrypto(wrongKey[:])
	require.NoError(t, err)

	ct, err := enc.Seal("secret")
	require.NoError(t, err)
	_, err = dec.Open(ct)
	assert.ErrorIs(t, err, ErrCryptoInvalid, "Open with wrong key must return ErrCryptoInvalid")
}

func TestCrypto_TamperedCiphertextFails(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(validKey[:])
	require.NoError(t, err)

	ct, err := c.Seal("secret")
	require.NoError(t, err)

	raw, err := base64.StdEncoding.DecodeString(ct)
	require.NoError(t, err)
	// Flip a bit in the ciphertext body (after the 12-byte nonce).
	raw[len(raw)-1] ^= 0x01
	tampered := base64.StdEncoding.EncodeToString(raw)

	_, err = c.Open(tampered)
	assert.ErrorIs(t, err, ErrCryptoInvalid, "tampered ciphertext must fail authentication")
}

func TestCrypto_MalformedInputFails(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(validKey[:])
	require.NoError(t, err)

	for _, bad := range []string{
		"not-base64!@#$",
		base64.StdEncoding.EncodeToString([]byte("short")),
		"",
	} {
		bad := bad
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			// Empty string is the symmetric "no input" case and returns "".
			if bad == "" {
				out, err := c.Open(bad)
				require.NoError(t, err)
				assert.Equal(t, "", out)
				return
			}
			_, err := c.Open(bad)
			assert.ErrorIs(t, err, ErrCryptoInvalid, "malformed ciphertext must fail cleanly")
		})
	}
}

func TestCrypto_EmptyPlaintextRoundTrips(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(validKey[:])
	require.NoError(t, err)

	ct, err := c.Seal("")
	require.NoError(t, err)
	assert.Equal(t, "", ct, "Seal('') returns '' so callers can store NULL/empty cleanly")

	pt, err := c.Open("")
	require.NoError(t, err)
	assert.Equal(t, "", pt)
}

func TestCrypto_SealPtr_OpenPtr(t *testing.T) {
	t.Parallel()
	c, err := NewCrypto(validKey[:])
	require.NoError(t, err)

	// Non-nil pointer round-trips.
	in := "secret-token"
	ct, err := c.SealPtr(&in)
	require.NoError(t, err)
	require.NotNil(t, ct)
	assert.NotEqual(t, in, *ct)

	out, err := c.OpenPtr(ct)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, in, *out)

	// Nil input yields nil output without touching the AEAD.
	nct, err := c.SealPtr(nil)
	require.NoError(t, err)
	assert.Nil(t, nct)

	npt, err := c.OpenPtr(nil)
	require.NoError(t, err)
	assert.Nil(t, npt)
}

func TestNewCrypto_RejectsBadKeyLength(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 16, 24, 31, 33, 64} {
		n := n
		t.Run("len", func(t *testing.T) {
			t.Parallel()
			_, err := NewCrypto(bytes.Repeat([]byte{0xAB}, n))
			assert.Error(t, err, "AES-256-GCM requires a 32-byte key; %d must be rejected", n)
			if err != nil && !strings.Contains(err.Error(), "32 bytes") {
				t.Errorf("error should mention 32 bytes, got: %v", err)
			}
		})
	}
}
