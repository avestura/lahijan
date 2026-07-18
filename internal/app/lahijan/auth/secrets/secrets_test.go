package secrets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssue_Verify_RoundTrip(t *testing.T) {
	t.Parallel()
	s := NewSigner("super-secret-key-for-tests")
	raw, hash, err := s.Issue(32)
	require.NoError(t, err)
	assert.Contains(t, raw, ".", "raw token must have the payload.signature shape")
	assert.Len(t, hash, 64, "hash must be sha256 hex (64 chars)")

	got, err := s.Verify(raw)
	require.NoError(t, err)
	assert.Equal(t, hash, got, "Verify must return the same hash produced at issue time")
}

func TestIssue_DistinctRandomPayloads(t *testing.T) {
	t.Parallel()
	s := NewSigner("k")
	a, _, _ := s.Issue(32)
	b, _, _ := s.Issue(32)
	assert.NotEqual(t, a, b, "two issued tokens must differ (random payload)")
}

func TestVerify_RejectsTampered(t *testing.T) {
	t.Parallel()
	s := NewSigner("the-key")
	raw, _, err := s.Issue(32)
	require.NoError(t, err)

	// Flip the last character of the signature to a different base64url char.
	// If the flip happens to be a no-op (the char was already the target), pick
	// a different target so the test is deterministic regardless of the random
	// signature produced.
	parts := strings.SplitN(raw, ".", 2)
	require.Len(t, parts, 2)
	sig := parts[1]
	last := sig[len(sig)-1]
	repl := byte('A')
	if last == 'A' {
		repl = 'B'
	}
	tampered := parts[0] + "." + sig[:len(sig)-1] + string(repl)
	_, err = s.Verify(tampered)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestVerify_RejectsDifferentKey(t *testing.T) {
	t.Parallel()
	issuer := NewSigner("key-one")
	verifier := NewSigner("key-two")
	raw, _, err := issuer.Issue(16)
	require.NoError(t, err)
	_, err = verifier.Verify(raw)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestVerify_RejectsMalformed(t *testing.T) {
	t.Parallel()
	s := NewSigner("k")
	for _, bad := range []string{"", "no-dot", "a.", ".b", "a.b.c"} {
		_, err := s.Verify(bad)
		assert.ErrorIsf(t, err, ErrInvalidToken, "input=%q", bad)
	}
}

func TestNewSigner_EmptyKeyFallsBackToDevValue(t *testing.T) {
	t.Parallel()
	s := NewSigner("") // must not panic, must still sign/verify
	raw, _, err := s.Issue(16)
	require.NoError(t, err)
	_, err = s.Verify(raw)
	require.NoError(t, err)
}

func TestIssue_MinLengthEnforced(t *testing.T) {
	t.Parallel()
	s := NewSigner("k")
	raw, _, err := s.Issue(4) // too small; bumped to 32
	require.NoError(t, err)
	payload := strings.SplitN(raw, ".", 2)[0]
	decoded := make([]byte, 64)
	n, err := b64decodeTo(payload, decoded)
	require.NoError(t, err)
	assert.Equal(t, 32, n, "byteLength below the floor must be raised to 32")
}

// b64decodeTo is a tiny local helper mirroring the package's b64decode so the
// test can assert the decoded length without exporting internals.
func b64decodeTo(s string, dst []byte) (int, error) {
	out, err := b64decode(s)
	if err != nil {
		return 0, err
	}
	return copy(dst, out), nil
}
