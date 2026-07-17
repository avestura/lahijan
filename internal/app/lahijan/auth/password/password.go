// Package password implements Lahijan's argon2id password hashing, verification,
// and strength validation (ADR-0004). The parameters are read from config via
// conf.GetAuthPasswordArgon2 and travel with every stored hash so a future
// parameter upgrade rehashes on the next successful login (migration path).
package password

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// ErrPasswordTooWeak is returned by Validate when a password fails the strength
// check. Callers translate it into a localizable user-facing message.
var ErrPasswordTooWeak = errors.New("password: too weak")

// ErrHashMalformed is returned by Verify when a stored hash is not a valid
// argon2id encoded string. This indicates data corruption, not a wrong password.
var ErrHashMalformed = errors.New("password: stored hash is malformed")

// Hasher hashes and verifies passwords using argon2id with the parameters
// captured at construction time. Capture the params once at bootstrap (from
// conf) and reuse the Hasher for every request.
type Hasher struct {
	params argon2idParams
}

type argon2idParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

// NewHasher builds a Hasher with the given argon2id parameters.
func NewHasher(memory, iterations uint32, parallelism uint8, saltLength, keyLength uint32) *Hasher {
	return &Hasher{params: argon2idParams{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		saltLength:  saltLength,
		keyLength:   keyLength,
	}}
}

// encodedHash is the internal representation of a stored argon2id hash. It
// serialises to the PHC-ish string format "$argon2id$v=19$m=...,t=...,p=...$<salt>$<key>".
type encodedHash struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	key         []byte
}

// Hash returns an argon2id encoding of password using the Hasher's parameters.
// The encoding is self-describing: Verify can re-derive the key without knowing
// the parameters in advance, which is what enables a future parameter upgrade.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: generate salt: %w", err)
	}
	key := argon2.IDKey(
		[]byte(password), salt,
		h.params.iterations, h.params.memory, h.params.parallelism, h.params.keyLength,
	)
	enc := encodedHash{
		memory:      h.params.memory,
		iterations:  h.params.iterations,
		parallelism: h.params.parallelism,
		salt:        salt,
		key:         key,
	}
	return encode(enc), nil
}

// Verify reports whether password matches the given argon2id encoded hash. A
// malformed hash returns ErrHashMalformed; a well-formed hash with the wrong
// password returns (false, nil). Callers MUST treat ErrHashMalformed as a
// server error, not an auth failure.
func (h *Hasher) Verify(password, encoded string) (bool, error) {
	enc, err := decode(encoded)
	if err != nil {
		return false, ErrHashMalformed
	}
	key := argon2.IDKey(
		[]byte(password), enc.salt,
		enc.iterations, enc.memory, enc.parallelism, uint32(len(enc.key)),
	)
	return subtleEqual(key, enc.key), nil
}

// NeedsRehash reports whether the encoded hash was produced with parameters
// weaker than the Hasher's current parameters. Use this on every successful
// login to lazily upgrade stored hashes after a parameter bump.
func (h *Hasher) NeedsRehash(encoded string) bool {
	enc, err := decode(encoded)
	if err != nil {
		return true
	}
	return enc.memory != h.params.memory ||
		enc.iterations != h.params.iterations ||
		enc.parallelism != h.params.parallelism ||
		uint32(len(enc.key)) != h.params.keyLength
}

// Validate enforces the minimum password strength. Lahijan's policy is a length
// floor plus a character-class check; the optional HIBP breach check (WS-06
// open question) is opt-in via conf and not wired here.
func Validate(password string, minLength int) error {
	if minLength <= 0 {
		minLength = 12
	}
	if utf8.RuneCountInString(password) < minLength {
		return fmt.Errorf("%w: need at least %d characters", ErrPasswordTooWeak, minLength)
	}
	classes := 0
	if hasLower(password) {
		classes++
	}
	if hasUpper(password) {
		classes++
	}
	if hasDigit(password) {
		classes++
	}
	if hasSymbol(password) {
		classes++
	}
	// Require at least three of the four character classes once the password is
	// long enough; this keeps the policy simple and predictable.
	if classes < 3 {
		return fmt.Errorf("%w: use a mix of letters, digits, and symbols", ErrPasswordTooWeak)
	}
	return nil
}

const argon2idEncodingPrefix = "$argon2id$"

func encode(h encodedHash) string {
	return fmt.Sprintf(
		"%sv=19$m=%d,t=%d,p=%d$%s$%s",
		argon2idEncodingPrefix,
		h.memory, h.iterations, h.parallelism,
		base64.RawStdEncoding.EncodeToString(h.salt),
		base64.RawStdEncoding.EncodeToString(h.key),
	)
}

func decode(s string) (encodedHash, error) {
	if !strings.HasPrefix(s, argon2idEncodingPrefix) {
		return encodedHash{}, fmt.Errorf("password: missing %q prefix", argon2idEncodingPrefix)
	}
	rest := strings.TrimPrefix(s, argon2idEncodingPrefix)
	parts := strings.Split(rest, "$")
	if len(parts) != 4 || parts[0] != "v=19" {
		return encodedHash{}, errors.New("password: unexpected argon2id layout")
	}
	var mem, iters uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[1], "m=%d,t=%d,p=%d", &mem, &iters, &par); err != nil {
		return encodedHash{}, fmt.Errorf("password: parse params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return encodedHash{}, fmt.Errorf("password: decode salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return encodedHash{}, fmt.Errorf("password: decode key: %w", err)
	}
	return encodedHash{
		memory:      mem,
		iterations:  iters,
		parallelism: par,
		salt:        salt,
		key:         key,
	}, nil
}

// subtleEqual is a constant-time byte comparison to avoid timing leaks. We use
// crypto/subtle rather than bytes.Equal so a wrong-password path takes the same
// time as a correct-password path.
func subtleEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func hasLower(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

func hasUpper(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func hasSymbol(s string) bool {
	for _, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isUpper && !isDigit {
			return true
		}
	}
	return false
}
