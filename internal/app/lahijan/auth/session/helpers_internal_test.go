package session

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a@b.com", normalizeEmail("  A@B.Com \n"))
}

func TestIsValidEmail(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"a@b.com":   true,
		"@b.com":    false,
		"a@":        false,
		"a@b":       false,
		"plain":     false,
		"a@b.c.org": true,
	}
	for in, want := range cases {
		assert.Equal(t, want, isValidEmail(in), in)
	}
}

func TestIsUniqueViolationEmail(t *testing.T) {
	t.Parallel()
	assert.False(t, isUniqueViolationEmail(nil))
	assert.True(t, isUniqueViolationEmail(errors.New("ERROR: duplicate key (SQLSTATE 23505)")))
	assert.True(t, isUniqueViolationEmail(errors.New("violates unique constraint uq_users_email")))
	assert.False(t, isUniqueViolationEmail(errors.New("connection reset")))
}
