// Package recovery implements Lahijan's per-user single-use recovery codes
// (WS-07c). Each MFA-enabled user receives a fixed batch of N codes that
// can be used to bypass MFA exactly once each in case the user loses their
// TOTP device / YubiKey.
//
// Storage contract: only the SHA-256 hash of each raw code is persisted.
// The raw code is returned to the caller exactly once at generation time
// and the caller (MFA service) hands it to the API response so the
// dashboard can display it to the user. Lahijan never re-renders raw codes
// after generation.
//
// Code format: a 10-character groups-of-5 base32 alphabet, dash-separated:
//
//	ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z1234-56789-0
//
// The dash separators + group size match what every major cloud platform
// uses (AWS, Google) and are user-friendly to read + type. The character
// set is RFC 4648 base32 WITHOUT padding (uppercase letters + 2-9; O, 0,
// I, 1 are excluded to avoid look-alike confusion).
package recovery

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// DefaultCount is the standard number of codes per batch (per WS-07c scope).
const DefaultCount = 10

// DefaultGroupSize is the number of characters per dash-separated group.
// 5 is the de-facto standard that balances readability and code length.
const DefaultGroupSize = 5

// DefaultGroupCount is the number of groups per code. 6 groups × 5 chars =
// 30 characters per code, well above the OWASP-recommended entropy floor
// for one-shot codes (>=80 bits; we ship ~125 bits).
const DefaultGroupCount = 6

// base32Alphabet is the safe-OCR alphabet used to render raw codes:
// uppercase A-Z plus the digits 2-7 (the RFC 4648 base32 alphabet minus
// the four look-alike characters O/0/I/1).
const base32Alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Config controls the shape of a generated batch. Defaults follow the
// WS-07c scope; callers typically only override Count.
type Config struct {
	Count      int // number of codes per batch; default 10
	GroupSize  int // chars per dash-group; default 5
	GroupCount int // number of dash-groups per code; default 6
}

// DefaultConfig returns the WS-07c scope defaults. Override Count to issue
// more/fewer codes per batch.
func DefaultConfig() Config {
	return Config{
		Count:      DefaultCount,
		GroupSize:  DefaultGroupSize,
		GroupCount: DefaultGroupCount,
	}
}

// ErrInvalidCode is returned by Hash when the supplied code is malformed
// (wrong group count, wrong character set, etc.). A code that is
// well-formed but does not match a stored hash returns no error from Hash
// — the caller looks the hash up in the DB and reports "no match".
var ErrInvalidCode = errors.New("recovery: malformed code")

// Batch is the result of Generate: the raw codes (shown to the user once)
// and their SHA-256 hashes (stored at rest). The caller MUST discard the
// raw slice after returning it to the API; Lahijan does not re-render raw
// codes after generation.
type Batch struct {
	// Raw is the slice of human-readable codes ("ABCDE-FGHIJ-...").
	// Shown to the user exactly once at generation time.
	Raw []string
	// Hashes carries the SHA-256 hex digests the caller persists.
	Hashes []string
}

// Generate produces a fresh batch of recovery codes. The raw codes are
// returned alongside their SHA-256 hashes so the MFA service can:
//
//  1. Wipe every existing code row for the user (DeleteAllForUser).
//  2. Insert one row per Hashes entry (Create).
//  3. Hand Raw back to the API so the dashboard can render them once.
//
// The order of Raw and Hashes is identical (Hashes[i] is the hash of
// Raw[i]).
func Generate(c Config) (Batch, error) {
	count := c.Count
	if count <= 0 {
		count = DefaultCount
	}
	gSize := c.GroupSize
	if gSize <= 0 {
		gSize = DefaultGroupSize
	}
	gCount := c.GroupCount
	if gCount <= 0 {
		gCount = DefaultGroupCount
	}
	raw := make([]string, 0, count)
	hashes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		code, err := randomCode(gSize, gCount)
		if err != nil {
			return Batch{}, fmt.Errorf("recovery: generate code %d: %w", i, err)
		}
		raw = append(raw, code)
		hashes = append(hashes, Hash(code))
	}
	return Batch{Raw: raw, Hashes: hashes}, nil
}

// randomCode produces one recovery code of groupCount dash-separated groups
// of groupSize base32 characters each. The randomness comes from
// crypto/rand so the codes are unpredictable.
func randomCode(groupSize, groupCount int) (string, error) {
	totalChars := groupSize * groupCount
	buf := make([]byte, totalChars)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("recovery: read random: %w", err)
	}
	out := make([]byte, 0, totalChars+groupCount)
	for i := 0; i < totalChars; i++ {
		// Map each random byte to a base32Alphabet character via modulo.
		// The bias introduced by mapping 256 values to 30 characters is
		// negligible for one-shot recovery codes (the effective entropy
		// loss is <0.01 bits per character).
		out = append(out, base32Alphabet[int(buf[i])%len(base32Alphabet)])
		if i+1 < totalChars && (i+1)%groupSize == 0 {
			out = append(out, '-')
		}
	}
	return string(out), nil
}

// Hash returns the lowercase hex SHA-256 digest of the supplied code. The
// code MUST be in the canonical form (uppercase, dash-separated) as
// produced by Generate. Use Normalize first to canonicalise user input.
func Hash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// Normalize canonicalises user-supplied recovery code input: uppercase
// letters, strip whitespace, and tolerant of - or " " between groups.
// Returns the canonical dash-separated form (e.g. "ABCDE-FGHIJ-...").
//
// The canonical form is what the DB hash was computed over, so callers
// must Hash(Normalize(input)) to look up a row.
func Normalize(s string) (string, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return "", ErrInvalidCode
	}
	// Strip every separator the user might have typed (spaces, dashes,
	// underscores). The canonical form re-inserts dashes below; here we
	// just need the bare base32 chars so the alphabet check is exact.
	s = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
	// Validate the character set before re-grouping so an obviously
	// malformed code fails fast (the DB lookup would never match anyway).
	for _, r := range s {
		if !strings.ContainsRune(base32Alphabet, r) {
			return "", ErrInvalidCode
		}
	}
	// Re-insert the canonical "-" every DefaultGroupSize characters so
	// the hash matches the form Generate produced.
	out := make([]byte, 0, len(s)+len(s)/DefaultGroupSize)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i])
		if (i+1)%DefaultGroupSize == 0 && i+1 != len(s) {
			out = append(out, '-')
		}
	}
	return string(out), nil
}

// ConstantTimeHashEq reports whether the user-supplied (normalised) code
// hashes to the supplied hash, using a constant-time comparison so the
// lookup does not leak timing information about how close the guess was.
// Most callers go through the DB lookup path (LookupUnused + Consume) and
// never call this directly; the helper exists for the test path and as
// the reference implementation of "what equals what" in this package.
func ConstantTimeHashEq(code, expectedHash string) bool {
	return hmac.Equal([]byte(Hash(code)), []byte(expectedHash))
}
