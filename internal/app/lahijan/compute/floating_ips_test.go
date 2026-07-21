// Package compute: floating_ips_test.go covers the pure helpers in
// floating_ips.go (validateCanonicalDNSName, ensureTrailingDot) +
// descriptionOrNil from ip_pools.go. The full lifecycle paths are
// covered by the integration tests under //go:build integration.
package compute

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureTrailingDot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no dot", "host.example.com", "host.example.com."},
		{"already dotted", "host.example.com.", "host.example.com."},
		{"single label", "host", "host."},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ensureTrailingDot(tc.in))
		})
	}
}

func TestValidateCanonicalDNSName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", true},
		{"no trailing dot", "host.example.com", true},
		{"uppercase", "Host.Example.Com.", true},
		{"valid", "host.example.com.", false},
		{"valid single label", "host.", false},
		{"double dot", "host..example.com.", true},
		{"empty label at end", "host.example.com..", true},
		{"label too long", labelLen64() + ".example.com.", true},
		{"valid long-ish", "a.b.c.d.e.f.g.h.example.com.", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateCanonicalDNSName(tc.in)
			if tc.wantErr {
				assert.Error(t, err, "expected error for %q", tc.in)
			} else {
				assert.NoError(t, err, "expected no error for %q", tc.in)
			}
		})
	}
}

func TestDescriptionOrNil(t *testing.T) {
	t.Parallel()
	t.Run("empty returns nil", func(t *testing.T) {
		t.Parallel()
		got := descriptionOrNil("")
		assert.Nil(t, got)
	})
	t.Run("non-empty returns pointer", func(t *testing.T) {
		t.Parallel()
		got := descriptionOrNil("production pool")
		require.NotNil(t, got)
		assert.Equal(t, "production pool", *got)
	})
}

// labelLen64 returns a DNS label of 64 chars (1 over the RFC limit).
func labelLen64() string {
	out := make([]byte, 64)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}
