// Package totp: totp_test.go covers the wrapping layer over pquerna/otp.
// These are pure unit tests — no DB, no HTTP. The MFA service's
// integration tests (auth/mfa) exercise Validate against the database
// round-trip and the AES-GCM envelope.
package totp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pquerna/otp/totp"
)

func TestGenerate_Defaults(t *testing.T) {
	t.Parallel()
	s, err := Generate(DefaultConfig(), "user@example.test")
	require.NoError(t, err)
	assert.NotEmpty(t, s.Raw, "secret must be non-empty")
	assert.NotEmpty(t, s.ProvisioningURI, "uri must be non-empty")
	assert.Contains(t, s.ProvisioningURI, "otpauth://totp/")
	assert.Contains(t, s.ProvisioningURI, "Lahijan")
	assert.Contains(t, s.ProvisioningURI, "Lahijan:user@example.test")
	assert.Contains(t, s.ProvisioningURI, "digits=6")
	assert.Contains(t, s.ProvisioningURI, "period=30")
}

func TestGenerate_DigitsEight(t *testing.T) {
	t.Parallel()
	c := DefaultConfig()
	c.Digits = 8
	s, err := Generate(c, "octo@example.test")
	require.NoError(t, err)
	assert.Contains(t, s.ProvisioningURI, "digits=8")
}

func TestGenerate_BadPeriod(t *testing.T) {
	t.Parallel()
	c := DefaultConfig()
	c.Period = 90 // out of range
	_, err := Generate(c, "user@example.test")
	require.Error(t, err)
}

func TestGenerate_EmptyAccount(t *testing.T) {
	t.Parallel()
	_, err := Generate(DefaultConfig(), "")
	require.Error(t, err)
}

func TestValidate_HappyRoundTrip(t *testing.T) {
	t.Parallel()
	s, err := Generate(DefaultConfig(), "user@example.test")
	require.NoError(t, err)
	// pquerna/otp exposes a helper to compute the current code for a
	// secret — used here to mirror what the user's authenticator app
	// would produce.
	code, err := totp.GenerateCode(s.Raw, time.Now())
	require.NoError(t, err)
	require.NoError(t, Validate(DefaultConfig(), s.Raw, code))
}

func TestValidate_RejectsWrongCode(t *testing.T) {
	t.Parallel()
	s, err := Generate(DefaultConfig(), "user@example.test")
	require.NoError(t, err)
	err = Validate(DefaultConfig(), s.Raw, "000000")
	require.ErrorIs(t, err, ErrInvalidCode)
}

func TestValidate_RejectsEmptyInputs(t *testing.T) {
	t.Parallel()
	require.ErrorIs(t, Validate(DefaultConfig(), "", "123456"), ErrInvalidCode)
	require.ErrorIs(t, Validate(DefaultConfig(), "JBSWY3DPEHPK3PXP", ""), ErrInvalidCode)
}

func TestValidate_RespectsSkewWindow(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Period = 30
	cfg.Skew = 1 // ±30s
	s, err := Generate(cfg, "user@example.test")
	require.NoError(t, err)
	// Compute the code from one period ago (now - 30s) — skew=1 must
	// accept it. pquerna/otp's ValidateCustom internally uses time.Now()
	// with the symmetric (±skew) window, so a code from t-1 period
	// validates when skew >= 1.
	past := time.Now().Add(-30 * time.Second)
	code, err := totp.GenerateCode(s.Raw, past)
	require.NoError(t, err)
	require.NoError(t, Validate(cfg, s.Raw, code), "code from t-1 period must validate within skew=1")
}
