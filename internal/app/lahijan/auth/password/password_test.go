package password

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHasher builds a Hasher with intentionally weak params so the unit
// tests run fast. Production code builds the Hasher from conf.GetAuthPasswordArgon2.
func newTestHasher() *Hasher {
	// memory=4 KiB keeps argon2 happy while staying well under test budgets.
	return NewHasher(4, 1, 1, 8, 16)
}

func TestHashAndVerify_RoundTrip(t *testing.T) {
	t.Parallel()
	h := newTestHasher()
	enc, err := h.Hash("correct horse battery staple")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(enc, "$argon2id$v=19$m=4,t=1,p=1$"))

	ok, err := h.Verify("correct horse battery staple", enc)
	require.NoError(t, err)
	assert.True(t, ok, "the password we just hashed must verify")
}

func TestVerify_WrongPassword(t *testing.T) {
	t.Parallel()
	h := newTestHasher()
	enc, err := h.Hash("hunter2hunter2!") // length+classes valid but irrelevant here
	require.NoError(t, err)

	ok, err := h.Verify("wrong password", enc)
	require.NoError(t, err)
	assert.False(t, ok, "a different password must not verify")
}

func TestVerify_DistinctSalts(t *testing.T) {
	t.Parallel()
	h := newTestHasher()
	a, err := h.Hash("samepassword1!")
	require.NoError(t, err)
	b, err := h.Hash("samepassword1!")
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "hashing the same password twice must differ (random salt)")

	ok, err := h.Verify("samepassword1!", b)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerify_MalformedHash(t *testing.T) {
	t.Parallel()
	h := newTestHasher()
	_, err := h.Verify("x", "not-a-real-hash")
	assert.ErrorIs(t, err, ErrHashMalformed)

	_, err = h.Verify("x", "$argon2id$v=19$m=1,t=1,p=1$_deadbeef$")
	assert.ErrorIs(t, err, ErrHashMalformed)
}

func TestNeedsRehash_TrueForWeakerParams(t *testing.T) {
	t.Parallel()
	weak := NewHasher(4, 1, 1, 8, 16)
	strong := NewHasher(8192, 3, 2, 16, 32)

	enc, err := weak.Hash("Tr0ub4dour&3-man")
	require.NoError(t, err)

	assert.True(t, strong.NeedsRehash(enc), "a hash made with weaker params must need rehashing")
	assert.False(t, weak.NeedsRehash(enc), "a hash made with the same params must not need rehashing")
}

func TestValidate_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		pw      string
		minLen  int
		wantErr bool
	}{
		{"strong enough", "Tr0ub4dour&3man", 12, false},
		{"too short", "Ab1!", 12, true},
		{"length ok but few classes", "alllowercase!!", 12, true},
		{"only two classes", "Lowercase12", 12, true},
		{"four classes", "Abcdef1!xyz", 8, false},
	}
	for _, tc := range cases {
		err := Validate(tc.pw, tc.minLen)
		if tc.wantErr {
			assert.ErrorIs(t, err, ErrPasswordTooWeak, tc.name)
		} else {
			assert.NoError(t, err, tc.name)
		}
	}
}

func TestValidate_DefaultsToReasonableFloor(t *testing.T) {
	t.Parallel()
	// minLength of 0 or negative must not disable the check.
	assert.ErrorIs(t, Validate("short", 0), ErrPasswordTooWeak)
}
