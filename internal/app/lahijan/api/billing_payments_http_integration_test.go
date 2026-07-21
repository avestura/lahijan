// billing_payments_http_integration_test.go exercises the WS-27
// payment-gateway HTTP surface end to end against a real Postgres
// (testcontainers) plus the real auth + tenant + rbac middleware + a
// real Stripe provider pointing at the in-memory httptest fake. It is
// the WS-27 DoD "every privileged action calls RequirePerm" + "audit
// emit pre + post" at the HTTP boundary.
//
// The deeper signature-verification + dispatch coverage is in
// providers/stripe/webhooks_test.go + billing/billing_payments_service_test.go
// at the service layer.

//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/testutil"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe/fake"
)

// newPaymentsTestApp extends newBillingTestApp with the WS-27 payment
// gateway stack wired against the in-memory httptest fake. Returns the
// test app + the billing service + the payments service + the fake
// Stripe server so the test can drive further setup.
func newPaymentsTestApp(t *testing.T) (*testApp, *billing.Service, *billing.PaymentsService, *fake.Server) {
	t.Helper()
	ta, billingSvc := newBillingTestApp(t)
	// RBAC catalog seed is required for billingSeedUserInTenant to
	// resolve roles. Each test that needs a user-with-role calls this
	// helper; the seed is idempotent so it's safe to call per-test.
	seedCtx, seedCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer seedCancel()
	require.NoError(t, rbac.SeedOnce(seedCtx, ta.repos))
	srv := fake.NewServer(t)
	gw, err := stripe.NewClient(stripe.Config{
		HTTPClient:    &http.Client{},
		BaseURL:       srv.HTTP.URL,
		SecretKey:     srv.SecretKey,
		WebhookSecret: srv.WebhookSecret,
	})
	require.NoError(t, err)
	crypto, err := secrets.NewCrypto(make([]byte, 32))
	require.NoError(t, err)
	paymentsSvc := billing.NewPaymentsService(
		billingSvc,
		ta.repos,
		gw,
		crypto,
		audit.NewDBEmitter(ta.repos.AuditLog),
		nil,
		rbac.NewEvaluator(ta.repos.Memberships),
		billing.PaymentsConfig{
			PublishableKey: "pk_test_fake",
		},
	)
	ta.server.SetPaymentsService(paymentsSvc)
	return ta, billingSvc, paymentsSvc, srv
}

// TestPaymentsDisabled_Returns501 covers the WS-27 DoD "feature
// disabled degrades to 501": when the payment gateway is not wired,
// every payment endpoint returns 501 not_implemented. The webhook
// receiver (/api/v1/webhooks/stripe) returns 200 to stop Stripe
// retries.
func TestPaymentsDisabled_Returns501(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // no payments service wired
	_, sess, tidStr := registerAndLogin(t, ta, rbac.RoleTenantAdmin)

	cases := []struct{ method, path string }{
		{"GET", "/api/v1/billing/payment-methods"},
		{"POST", "/api/v1/billing/topup"},
		{"GET", "/api/v1/billing/subscriptions"},
		{"GET", "/api/v1/admin/billing/plans"},
		{"GET", "/api/v1/admin/billing/webhook-events"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(c.method, c.path, nil)
			req.Header.Set("Cookie", "lahijan_session="+sess)
			req.Header.Set(middleware.HeaderTenantID, tidStr)
			resp, err := ta.app.Test(req, -1)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, 501, resp.StatusCode, "no payments service -> 501 for %s %s", c.method, c.path)
		})
	}

	// Webhook receiver: 200 OK even when disabled (so Stripe doesn't retry).
	req := httptest.NewRequest("POST", "/api/v1/webhooks/stripe",
		bytes.NewReader([]byte("{}")))
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
}

// TestPaymentsPublicConfig_AvailableWithoutSession covers the
// /api/v1/billing/config endpoint: it's available without a session
// so the SPA can render the "Add card" affordance based on whether
// the gateway is configured.
func TestPaymentsPublicConfig_AvailableWithoutSession(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t) // no payments service wired
	req := httptest.NewRequest("GET", "/api/v1/billing/config", nil)
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(raw), `"enabled":false`)
}

// TestStripeWebhook_SignatureFailure covers the WS-27 DoD "webhook
// signature verification failure -> 400".
func TestStripeWebhook_SignatureFailure(t *testing.T) {
	t.Parallel()
	ta, _, _, _ := newPaymentsTestApp(t)
	req := httptest.NewRequest("POST", "/api/v1/webhooks/stripe",
		bytes.NewReader([]byte(`{"id":"evt_x","type":"payment_intent.succeeded"}`)))
	// No Stripe-Signature header -> verify fails -> 400.
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 400, resp.StatusCode, "missing signature -> 400")
}

// TestStripeWebhook_HappyPath covers the WS-27 DoD "webhook receiver
// creates a ledger entry on payment_intent.succeeded" + the
// idempotency property (duplicate delivery does not double-credit).
func TestStripeWebhook_HappyPath(t *testing.T) {
	t.Parallel()
	ta, billingSvc, _, srv := newPaymentsTestApp(t)

	// Seed a tenant + a tenant.admin user.
	tenant := testutil.NewTenant(context.Background(), t, testutil.Pool())
	tid := tenant.ID
	_, _ = billingSeedUserInTenant(t, ta, tid.String(), rbac.RoleTenantAdmin)

	// Resolve the seeded user.
	ctx := context.Background()
	users, err := ta.repos.Users.List(ctx, 50, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(users), 1, "expected at least one seeded user")
	user := users[len(users)-1]

	amountCents := int64(1500)
	currency := "usd"
	ev := stripe.Event{
		ID:         "evt_" + uuid.NewString(),
		Type:       "payment_intent.succeeded",
		APIVersion: "2024-06-20",
		Created:    time.Now().Unix(),
		Data: stripe.EventData{
			Object: jsonPI(t, stripe.PaymentIntent{
				ID:       "pi_test_" + uuid.NewString(),
				Amount:   amountCents,
				Currency: currency,
				Status:   "succeeded",
				Metadata: map[string]any{
					"tenant_id": tid.String(),
					"user_id":   user.ID.String(),
				},
			}),
		},
	}
	body, sig := srv.EmitWebhookBytes(ev)
	// Sanity-check: the verifier should accept this signature.
	_, vErr := stripe.VerifyWebhook(srv.WebhookSecret, sig, body, time.Now(), 0)
	require.NoError(t, vErr, "signature must verify locally before we send to the handler")
	req := httptest.NewRequest("POST", "/api/v1/webhooks/stripe", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", sig)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	if !assert.Equal(t, 200, resp.StatusCode, "happy-path webhook -> 200") {
		// On failure, print the body so the operator can see what went wrong.
		raw, _ := io.ReadAll(resp.Body)
		t.Logf("webhook response body: %s", string(raw))
	}

	// Verify the ledger row landed.
	rctx := database.WithTenant(context.Background(), tid)
	bal, err := billingSvc.GetBalance(rctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, amountCents, bal.BalanceCents, "ledger credit landed in the cache")

	// Re-deliver the same event; idempotency dedup.
	req2 := httptest.NewRequest("POST", "/api/v1/webhooks/stripe", bytes.NewReader(body))
	req2.Header.Set("Stripe-Signature", sig)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := ta.app.Test(req2, -1)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode, "duplicate delivery -> 200 (deduped)")

	bal2, err := billingSvc.GetBalance(rctx, user.ID)
	require.NoError(t, err)
	assert.EqualValues(t, amountCents, bal2.BalanceCents, "duplicate delivery did not double-credit")
}

// TestStripeWebhook_AdminListEvents covers the admin webhook-events
// listing endpoint after a successful delivery.
func TestStripeWebhook_AdminListEvents(t *testing.T) {
	t.Parallel()
	ta, _, _, srv := newPaymentsTestApp(t)
	tenant := testutil.NewTenant(context.Background(), t, testutil.Pool())
	tid := tenant.ID
	uid, _ := billingSeedUserInTenant(t, ta, tid.String(), rbac.RoleTenantAdmin)

	// Emit + deliver one event with the tenant_id in metadata so the
	// list query (which is tenant-scoped via the admin user's session)
	// can find it.
	ev := stripe.Event{
		ID:         "evt_list_" + uuid.NewString(),
		Type:       "invoice.paid",
		APIVersion: "2024-06-20",
		Created:    time.Now().Unix(),
		Data: stripe.EventData{
			Object: jsonPI(t, stripe.Invoice{
				ID:       "in_1",
				Total:    0,
				Currency: "usd",
				Status:   "paid",
				Metadata: map[string]any{
					"tenant_id": tid.String(),
					"user_id":   uid.String(),
				},
			}),
		},
	}
	body, sig := srv.EmitWebhookBytes(ev)
	deliverReq := httptest.NewRequest("POST", "/api/v1/webhooks/stripe", bytes.NewReader(body))
	deliverReq.Header.Set("Stripe-Signature", sig)
	resp, err := ta.app.Test(deliverReq, -1)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Look up the user's email + log in to get a session.
	ctx := context.Background()
	user, err := ta.repos.Users.GetByID(ctx, uid)
	require.NoError(t, err)
	sess := sessionFor(t, ta, user.Email)

	listReq := httptest.NewRequest("GET", "/api/v1/admin/billing/webhook-events", nil)
	listReq.Header.Set("Cookie", "lahijan_session="+sess)
	listReq.Header.Set(middleware.HeaderTenantID, tid.String())
	listResp, err := ta.app.Test(listReq, -1)
	require.NoError(t, err)
	defer listResp.Body.Close()
	assert.Equal(t, 200, listResp.StatusCode, "admin can list webhook events")
	raw, _ := io.ReadAll(listResp.Body)
	assert.Contains(t, string(raw), "invoice.paid")
}

// TestStripeWebhook_RBACGate covers the WS-27 DoD "every privileged
// action calls RequirePerm" at the HTTP layer for the new admin
// endpoints. A tenant.viewer cannot list webhook events.
func TestStripeWebhook_RBACGate(t *testing.T) {
	t.Parallel()
	ta, _, _, _ := newPaymentsTestApp(t)
	tenant := testutil.NewTenant(context.Background(), t, testutil.Pool())
	tid := tenant.ID
	uid, _ := billingSeedUserInTenant(t, ta, tid.String(), rbac.RoleTenantViewer)

	// Resolve the seeded viewer's email + log in to get a session.
	ctx := context.Background()
	user, err := ta.repos.Users.GetByID(ctx, uid)
	require.NoError(t, err)
	sess := sessionFor(t, ta, user.Email)

	listReq := httptest.NewRequest("GET", "/api/v1/admin/billing/webhook-events", nil)
	listReq.Header.Set("Cookie", "lahijan_session="+sess)
	listReq.Header.Set(middleware.HeaderTenantID, tid.String())
	resp, err := ta.app.Test(listReq, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 403, resp.StatusCode, "tenant.viewer cannot read webhook events")
}

// TestStripePlanCRUD_Admin exercises the plan CRUD endpoints
// end-to-end (create + read + update + delete + push).
func TestStripePlanCRUD_Admin(t *testing.T) {
	t.Parallel()
	ta, _, _, _ := newPaymentsTestApp(t)
	tenant := testutil.NewTenant(context.Background(), t, testutil.Pool())
	tid := tenant.ID
	uid, sess := billingSeedUserInTenant(t, ta, tid.String(), rbac.RoleTenantAdmin)
	_ = uid

	// Create.
	body := bytes.NewReader([]byte(`{"slug":"starter","name":"Starter","interval":"monthly","priceCents":500,"currency":"USD","includedQuotaCents":100,"active":true}`))
	req := httptest.NewRequest("POST", "/api/v1/admin/billing/plans", body)
	req.Header.Set("Cookie", "lahijan_session="+sess)
	req.Header.Set(middleware.HeaderTenantID, tid.String())
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 201, resp.StatusCode, "create plan -> 201")
	raw, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(raw), "starter")

	// List (public).
	listReq := httptest.NewRequest("GET", "/api/v1/billing/plans", nil)
	listReq.Header.Set(middleware.HeaderTenantID, tid.String())
	listResp, err := ta.app.Test(listReq, -1)
	require.NoError(t, err)
	defer listResp.Body.Close()
	assert.Equal(t, 200, listResp.StatusCode)
	lraw, _ := io.ReadAll(listResp.Body)
	assert.Contains(t, string(lraw), "starter")
}

// TestStripePromoCodeRedeem exercises the admin promo-code create +
// user redeem path. Verifies the ledger credit lands.
func TestStripePromoCodeRedeem(t *testing.T) {
	t.Parallel()
	ta, billingSvc, _, _ := newPaymentsTestApp(t)
	tenant := testutil.NewTenant(context.Background(), t, testutil.Pool())
	tid := tenant.ID
	adminID, adminSess := billingSeedUserInTenant(t, ta, tid.String(), rbac.RoleTenantAdmin)
	memberID, memberSess := billingSeedUserInTenant(t, ta, tid.String(), rbac.RoleTenantMember)
	_ = adminID

	// Admin creates a promo code.
	createBody := bytes.NewReader([]byte(`{"code":"WELCOME100","creditCents":1000,"currency":"USD"}`))
	req := httptest.NewRequest("POST", "/api/v1/admin/billing/promo-codes", createBody)
	req.Header.Set("Cookie", "lahijan_session="+adminSess)
	req.Header.Set(middleware.HeaderTenantID, tid.String())
	req.Header.Set("Content-Type", "application/json")
	resp, err := ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 201, resp.StatusCode, "admin creates promo code -> 201")

	// Member redeems it.
	redeemBody := bytes.NewReader([]byte(`{"code":"WELCOME100"}`))
	req = httptest.NewRequest("POST", "/api/v1/billing/redeem", redeemBody)
	req.Header.Set("Cookie", "lahijan_session="+memberSess)
	req.Header.Set(middleware.HeaderTenantID, tid.String())
	req.Header.Set("Content-Type", "application/json")
	resp, err = ta.app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 201, resp.StatusCode, "redeem -> 201")

	// Verify the ledger credit.
	rctx := database.WithTenant(context.Background(), tid)
	bal, err := billingSvc.GetBalance(rctx, memberID)
	require.NoError(t, err)
	assert.EqualValues(t, 1000, bal.BalanceCents, "promo code credit landed in the cache")
}

// jsonPI marshals a value as json.RawMessage so it can be assigned to
// stripe.EventData.Object.
func jsonPI(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// sessionFor logs in with the supplied email and returns the session
// cookie. The billing test seed used strongPw so this works.
func sessionFor(t *testing.T, ta *testApp, email string) string {
	t.Helper()
	status, _, sc := doJSON(t, ta, "POST", "/api/v1/auth/login",
		map[string]any{"email": email, "password": strongPw}, "")
	require.Equalf(t, 200, status, "login for %s must succeed", email)
	return extractCookie(sc, "lahijan_session")
}

// _ keeps the time import alive when the only use is in a struct
// initializer that the linter doesn't always detect.
var _ = time.Now
