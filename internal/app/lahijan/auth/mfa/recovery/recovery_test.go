// Package recovery: recovery_test.go covers the code generation, hash
// round-trip, and normalization of user-supplied codes. These are pure
// unit tests — the DB single-use enforcement is exercised in auth/mfa's
// integration tests.
package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_DefaultBatch(t *testing.T) {
	t.Parallel()
	b, err := Generate(DefaultConfig())
	require.NoError(t, err)
	require.Len(t, b.Raw, DefaultCount, "default batch size")
	require.Len(t, b.Hashes, DefaultCount, "hashes match raw count")
	seen := map[string]struct{}{}
	for _, c := range b.Raw {
		// canonical form: GroupCount groups of GroupSize base32 chars
		// separated by dashes.
		parts := strings.Split(c, "-")
		require.Len(t, parts, DefaultGroupCount, "code group count")
		for _, p := range parts {
			require.Len(t, p, DefaultGroupSize, "code group size")
			for _, r := range p {
				assert.True(t, strings.ContainsRune(base32Alphabet, r), "char in alphabet: %q", r)
			}
		}
		// no duplicate codes within the batch
		_, dup := seen[c]
		require.False(t, dup, "duplicate code in batch")
		seen[c] = struct{}{}
	}
}

func TestGenerate_HashesMatchRaw(t *testing.T) {
	t.Parallel()
	b, err := Generate(DefaultConfig())
	require.NoError(t, err)
	for i, code := range b.Raw {
		// Manual SHA-256 to verify our Hash() implementation.
		sum := sha256.Sum256([]byte(code))
		require.Equal(t, hex.EncodeToString(sum[:]), b.Hashes[i])
	}
}

func TestGenerate_CustomBatchSize(t *testing.T) {
	t.Parallel()
	b, err := Generate(Config{Count: 4})
	require.NoError(t, err)
	require.Len(t, b.Raw, 4)
}

func TestNormalize_HandlesUserVariations(t *testing.T) {
	t.Parallel()
	b, err := Generate(DefaultConfig())
	require.NoError(t, err)
	original := b.Raw[0]
	// User-typed variants: lowercase, no dashes, with spaces.
	variants := []string{
		strings.ToLower(original),
		strings.ReplaceAll(original, "-", ""),
		strings.ReplaceAll(original, "-", " "),
		strings.ToLower(strings.ReplaceAll(original, "-", " ")),
	}
	for _, v := range variants {
		got, err := Normalize(v)
		require.NoError(t, err)
		require.Equal(t, original, got, "all variants normalise to the canonical form")
	}
}

func TestNormalize_RejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := Normalize("")
	require.ErrorIs(t, err, ErrInvalidCode)
}

func TestNormalize_RejectsBadCharacters(t *testing.T) {
	t.Parallel()
	_, err := Normalize("ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z123!6") // exclamation
	require.ErrorIs(t, err, ErrInvalidCode)
	_, err = Normalize("ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z10O44") // 0 and O excluded
	require.ErrorIs(t, err, ErrInvalidCode)
}

func TestConstantTimeHashEq(t *testing.T) {
	t.Parallel()
	b, err := Generate(DefaultConfig())
	require.NoError(t, err)
	require.True(t, ConstantTimeHashEq(b.Raw[0], b.Hashes[0]))
	require.False(t, ConstantTimeHashEq(b.Raw[1], b.Hashes[0]))
}

func TestHash_RoundTripsThroughNormalize(t *testing.T) {
	t.Parallel()
	b, err := Generate(DefaultConfig())
	require.NoError(t, err)
	// Simulate the DB lookup path: Hash(Normalize(userInput)) == stored hash.
	normalized, err := Normalize(strings.ToLower(strings.ReplaceAll(b.Raw[0], "-", "")))
	require.NoError(t, err)
	require.Equal(t, b.Hashes[0], Hash(normalized))
}
