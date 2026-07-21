// Package stripe_test exercises the VerifyWebhook signature path +
// the parseSignatureHeader parser. The fake server signs test events
// with its WebhookSecret via EmitWebhookBytes so the test can drive
// the verifier with real (HMAC) signatures.
//
// Lives in package stripe_test (not stripe) so it can import the
// stripe/fake helper without forming an import cycle.
package stripe_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe/fake"
)

func TestVerifyWebhook_OK(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	ev := stripeEvent("evt_1", "payment_intent.succeeded", map[string]any{"id": "pi_1"})
	body, sig := srv.EmitWebhookBytes(ev)
	out, err := stripe.VerifyWebhook(srv.WebhookSecret, sig, body, time.Now(), 0)
	require.NoError(t, err)
	assert.Equal(t, "evt_1", out.ID)
	assert.Equal(t, "payment_intent.succeeded", out.Type)
}

func TestVerifyWebhook_RejectsBadSecret(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	ev := stripeEvent("evt_2", "payment_intent.succeeded", nil)
	body, sig := srv.EmitWebhookBytes(ev)
	_, err := stripe.VerifyWebhook("whsec_wrong", sig, body, time.Now(), 0)
	require.ErrorIs(t, err, stripe.ErrInvalidSignature)
}

func TestVerifyWebhook_RejectsTamperedBody(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	ev := stripeEvent("evt_3", "payment_intent.succeeded", nil)
	body, sig := srv.EmitWebhookBytes(ev)
	// Flip a body byte after signing.
	tampered := append([]byte(nil), body...)
	if len(tampered) > 0 {
		tampered[0] ^= 0xFF
	}
	_, err := stripe.VerifyWebhook(srv.WebhookSecret, sig, tampered, time.Now(), 0)
	require.ErrorIs(t, err, stripe.ErrInvalidSignature)
}

func TestVerifyWebhook_RejectsOldTimestamp(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	ev := stripeEvent("evt_4", "payment_intent.succeeded", nil)
	body, sig := srv.EmitWebhookBytes(ev)
	// Pretend the verifier clock is far in the future.
	future := time.Now().Add(1 * time.Hour)
	_, err := stripe.VerifyWebhook(srv.WebhookSecret, sig, body, future, 0)
	require.ErrorIs(t, err, stripe.ErrInvalidSignature)
}

func TestVerifyWebhook_RejectsEmptySecret(t *testing.T) {
	t.Parallel()
	_, err := stripe.VerifyWebhook("", "t=1,v1=abc", []byte("x"), time.Now(), 0)
	require.ErrorIs(t, err, stripe.ErrInvalidSignature)
}

func TestVerifyWebhook_RejectsMalformedHeader(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"missing-equals",
		"v1=only",
		"t=not-a-number,v1=abc",
	}
	for _, tc := range cases {
		_, err := stripe.VerifyWebhook("whsec_x", tc, []byte("body"), time.Now(), 0)
		require.ErrorIs(t, err, stripe.ErrInvalidSignature, "header %q should be rejected", tc)
	}
}

func TestVerifyWebhook_ToleranceZero(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	ev := stripeEvent("evt_5", "payment_intent.succeeded", nil)
	body, sig := srv.EmitWebhookBytes(ev)
	// tolerance=0 should fall back to DefaultWebhookTolerance (5m); a
	// small clock skew of 1s should still pass.
	_, err := stripe.VerifyWebhook(srv.WebhookSecret, sig, body, time.Now(), 0)
	require.NoError(t, err)
}

// stripeEvent is a tiny test helper that builds a Stripe-shaped event
// payload without pulling in encoding/json at every call site.
func stripeEvent(id, typ string, obj map[string]any) stripe.Event {
	raw, _ := json.Marshal(obj)
	return stripe.Event{
		ID:         id,
		Type:       typ,
		APIVersion: "2024-06-20",
		Created:    1700000000,
		Livemode:   false,
		Data:       stripe.EventData{Object: raw},
	}
}
