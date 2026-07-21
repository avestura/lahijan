// Package program: stripe_provider.go wires the Stripe payment gateway
// driver (WS-27, ADR-0034) into process bootstrap. Mirrors the
// buildXxxDeps pattern used by the auth + jobs + wasm + incus + powerdns
// subsystems. The provider is built when conf.billing.stripe.enabled is
// true; otherwise this file returns a nil driver and the payment
// service degrades to 501.
package program

import (
	"errors"
	"net/http"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
)

// buildStripeProvider wires the Stripe driver from config. Returns an
// error when required fields are missing. The ping-at-startup is
// deferred to the first call (Stripe has no useful "ping" beyond the
// first real API call).
func buildStripeProvider() (*stripe.Provider, error) {
	if !conf.GetBillingStripeEnabled() {
		return nil, errors.New("build_stripe_provider: billing.stripe.enabled is false")
	}
	secret := conf.GetBillingStripeSecretKey()
	if secret == "" {
		return nil, errors.New("billing.stripe.secretKey must be set when billing.stripe.enabled is true")
	}
	timeout := time.Duration(conf.GetBillingStripeRequestTimeoutSeconds()) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	provider, err := stripe.NewClient(stripe.Config{
		HTTPClient:     &http.Client{Timeout: timeout},
		BaseURL:        conf.GetBillingStripeAPIBaseURL(),
		SecretKey:      secret,
		APIVersion:     conf.GetBillingStripeAPIVersion(),
		WebhookSecret:  conf.GetBillingStripeWebhookSecret(),
		RequestTimeout: timeout,
	})
	if err != nil {
		return nil, errors.Join(errors.New("build stripe client"), err)
	}
	return provider, nil
}

// buildBillingCrypto wires the AES-GCM envelope used to encrypt
// Stripe Customer ids at rest. Reuses the same process-wide key the
// auth subsystem uses for IdP tokens (ADR-0018); a future WS can add a
// separate key for billing if the security review wants isolation.
func buildBillingCrypto() (*secrets.Crypto, error) {
	raw := conf.GetAuthSecretsEncryptionKey()
	if raw == "" {
		return nil, errors.New("auth.secrets.encryptionKey must be set when billing.stripe.enabled is true (the customer-id envelope reuses the auth key)")
	}
	crypto, err := secrets.NewCrypto([]byte(raw))
	if err != nil {
		return nil, errors.Join(errors.New("build billing crypto envelope"), err)
	}
	return crypto, nil
}

// stripeLiveModeLabel returns "live" or "test" based on the secret
// key prefix. Used for the startup log line so operators can verify
// which mode they're in.
func stripeLiveModeLabel(secretKey string) string {
	const livePrefix = "sk_live_"
	const liveRestricted = "rk_live_"
	if len(secretKey) >= len(livePrefix) {
		if secretKey[:len(livePrefix)] == livePrefix || secretKey[:len(liveRestricted)] == liveRestricted {
			return "live"
		}
	}
	return "test"
}
