// Package totp implements Lahijan's RFC 6238 TOTP secret generation,
// provisioning URI rendering, and ±1 step verification (WS-07c).
//
// The package is the TOTP layer only — it does NOT touch the database and
// does NOT issue sessions. The MFA orchestrator (auth/mfa.Service) calls
// into Generate / ProvisioningURI / Validate; persistence is the
// database.TOTPSecretsRepository's job, and AES-GCM encryption of the raw
// secret at rest is auth/secrets.Crypto's job.
//
// The library used under the hood is github.com/pquerna/otp (Apache-2.0;
// see ADR-0021) — the de-facto Go implementation of RFC 6238 + RFC 4226.
// This package wraps it so callers never import pquerna/otp directly,
// keeping the WS-07c surface replaceable.
package totp

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Config carries the deployer-controlled TOTP parameters. Defaults follow
// the RFC 6238 + authenticator-app conventions: SHA-1 (the de-facto
// universal default every authenticator app expects), 30-second period,
// 6 digits, 20-byte (160-bit) secret. The deployer can override digits
// (6 or 8) and period (15..60) for unusual deployments, but the secret
// size and algorithm are locked because every authenticator app the user
// might pick assumes SHA-1 + 20-byte.
type Config struct {
	// Issuer is the human-readable platform name shown in the
	// authenticator app alongside the account name (e.g. "Lahijan").
	Issuer string
	// Digits is 6 (default) or 8.
	Digits int
	// Period is the step size in seconds (default 30; range 15..60).
	Period int
	// Skew is the number of periods before/after the current step that
	// the validator accepts (default 1 = ±30s with the default period).
	// Skew > 1 weakens the second factor; do not raise without reason.
	Skew uint
}

// DefaultConfig returns the RFC + authenticator-app-friendly defaults.
// Callers SHOULD override Issuer from conf.auth.mfa.totp.issuer (or fall
// back to the platform name); the other fields should stay at defaults.
func DefaultConfig() Config {
	return Config{
		Issuer: "Lahijan",
		Digits: 6,
		Period: 30,
		Skew:   1,
	}
}

// ErrInvalidCode is returned by Validate when the supplied 6-digit code
// does not validate against the secret within the configured skew window.
var ErrInvalidCode = errors.New("totp: invalid or expired code")

// Secret holds a freshly-generated raw base32 secret plus the provisioning
// URI the dashboard renders as a QR code for the user to scan.
type Secret struct {
	// Raw is the base32-encoded TOTP secret, e.g. "JBSWY3DPEHPK3PXP".
	// The MFA service encrypts this via auth/secrets.Crypto before
	// persisting; the raw value is shown to the user only via the
	// provisioning URI / manual entry fallback.
	Raw string
	// ProvisioningURI is the otpauth:// URL the dashboard encodes as a
	// QR code. Carries issuer, account, secret, algorithm, digits, period.
	ProvisioningURI string
}

// Generate produces a fresh TOTP secret + its otpauth:// provisioning URI
// for the given account name. The account name is typically the user's
// email; it appears in the authenticator app below the issuer.
func Generate(c Config, accountName string) (Secret, error) {
	if accountName == "" {
		return Secret{}, errors.New("totp: accountName is required")
	}
	opts := totp.GenerateOpts{
		Issuer:      c.Issuer,
		AccountName: accountName,
		Algorithm:   otp.AlgorithmSHA1,
	}
	if opts.Issuer == "" {
		opts.Issuer = "Lahijan"
	}
	if c.Digits == 8 {
		opts.Digits = otp.DigitsEight
	} else {
		opts.Digits = otp.DigitsSix
	}
	switch {
	case c.Period >= 15 && c.Period <= 60:
		opts.Period = uint(c.Period)
	case c.Period == 0:
		// default; lib picks 30.
	default:
		return Secret{}, fmt.Errorf("totp: period %d out of range (15..60)", c.Period)
	}
	key, err := totp.Generate(opts)
	if err != nil {
		return Secret{}, fmt.Errorf("totp: generate: %w", err)
	}
	return Secret{Raw: key.Secret(), ProvisioningURI: key.URL()}, nil
}

// Validate reports whether the supplied 6-digit code is valid for the
// given raw secret at the current time, within the configured skew window.
// An empty secret or malformed code yields ErrInvalidCode; the caller maps
// this to the localised "invalid or expired code" envelope.
//
// NOTE: the secret passed in here is the RAW base32 secret — the caller is
// responsible for AES-GCM-decrypting the stored ciphertext first. Passing
// the ciphertext directly will always fail validation.
func Validate(c Config, secret, code string) error {
	if secret == "" || code == "" {
		return ErrInvalidCode
	}
	skew := c.Skew
	if skew == 0 {
		skew = 1
	}
	digits := otp.DigitsSix
	if c.Digits == 8 {
		digits = otp.DigitsEight
	}
	period := uint(30)
	if c.Period >= 15 && c.Period <= 60 {
		period = uint(c.Period)
	}
	ok, err := totp.ValidateCustom(code, secret, nowFunc(), totp.ValidateOpts{
		Period:    period,
		Skew:      skew,
		Digits:    digits,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return errors.Join(ErrInvalidCode, err)
	}
	if !ok {
		return ErrInvalidCode
	}
	return nil
}

// nowFunc is the time-source seam for Validate. Production calls time.Now();
// tests can swap this for a frozen clock to assert against fixed codes.
var nowFunc = time.Now

// EncodeURI escapes the path segment of an otpauth:// URL the way the W3C
// authenticator apps expect. Helper for tests that build provisioning URIs
// by hand; production code uses Generate above which already produces a
// spec-compliant URL via pquerna/otp.
func EncodeURI(s string) string { return url.PathEscape(s) }
