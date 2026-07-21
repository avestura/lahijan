// Package stripe_test exercises the stripe.Provider surface against
// the in-memory httptest fake. Every test follows the same shape:
// build a fake server, point a real Provider at it, drive the public
// methods, assert on the returned values + the fake's observable
// state. t.Parallel() is safe — the fake is per-test.
//
// Lives in package stripe_test (not stripe) so it can import the
// stripe/fake helper without forming an import cycle.
package stripe_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe/fake"
)

func TestProvider_Name(t *testing.T) {
	t.Parallel()
	p := mustNewProvider(t, fake.NewServer(t))
	assert.Equal(t, "stripe", p.Name())
}

func TestProvider_Ping_OK(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)
	require.NoError(t, p.Ping(context.Background()))
}

func TestProvider_Ping_Unreachable(t *testing.T) {
	t.Parallel()
	p, err := stripe.NewClient(stripe.Config{
		HTTPClient: &http.Client{Timeout: 500 * time.Millisecond},
		BaseURL:    "http://127.0.0.1:1", // unroutable
		SecretKey:  "sk_test_...",
	})
	require.NoError(t, err)
	err = p.Ping(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, stripe.ErrUnreachable)
}

func TestProvider_Ping_BadKey(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p, err := stripe.NewClient(stripe.Config{
		HTTPClient: &http.Client{},
		BaseURL:    srv.HTTP.URL,
		SecretKey:  "sk_test_wrong",
	})
	require.NoError(t, err)
	err = p.Ping(context.Background())
	require.Error(t, err)
	// Should be a real API error (401 unauthenticated), NOT a network
	// unreachable wrap.
	assert.ErrorIs(t, err, stripe.ErrUnauthenticated)
	assert.NotErrorIs(t, err, stripe.ErrUnreachable)
}

func TestProvider_CreateCustomer(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	got, err := p.CreateCustomer(context.Background(), stripe.CreateCustomerRequest{
		Email:       "u1@example.test",
		Name:        "User One",
		Description: "test customer",
		Metadata:    map[string]any{"tenant_id": "t-abc", "user_id": "u-1"},
	}, "idem-1")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got.ID, "cus_"))
	assert.Equal(t, "u1@example.test", got.Email)
	assert.Equal(t, "User One", got.Name)
	assert.Equal(t, "t-abc", got.Metadata["tenant_id"])

	// Idempotency: replaying the same key returns the same customer.
	got2, err := p.CreateCustomer(context.Background(), stripe.CreateCustomerRequest{
		Email: "u1@example.test",
	}, "idem-1")
	require.NoError(t, err)
	assert.Equal(t, got.ID, got2.ID)
}

func TestProvider_FindCustomerByEmail(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	_, err := p.CreateCustomer(context.Background(), stripe.CreateCustomerRequest{
		Email: "findme@example.test",
	}, "")
	require.NoError(t, err)

	got, err := p.FindCustomerByEmail(context.Background(), "findme@example.test")
	require.NoError(t, err)
	assert.Equal(t, "findme@example.test", got.Email)

	_, err = p.FindCustomerByEmail(context.Background(), "missing@example.test")
	require.ErrorIs(t, err, stripe.ErrNotFound)
}

func TestProvider_AttachDetachPaymentMethod(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	cus, err := p.CreateCustomer(context.Background(), stripe.CreateCustomerRequest{
		Email: "pmm@example.test",
	}, "")
	require.NoError(t, err)

	pmID := srv.AttachPaymentMethod(cus.ID, "visa", "4242", "fp-1")
	got, err := p.GetPaymentMethod(context.Background(), pmID)
	require.NoError(t, err)
	assert.Equal(t, "visa", got.Card.Brand)
	assert.Equal(t, "4242", got.Card.Last4)

	// Detach.
	require.NoError(t, p.DetachPaymentMethod(context.Background(), pmID))
	got, err = p.GetPaymentMethod(context.Background(), pmID)
	require.NoError(t, err)
	assert.Empty(t, got.Customer, "detach should clear customer")
}

func TestProvider_CreatePaymentIntent(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	cus, err := p.CreateCustomer(context.Background(), stripe.CreateCustomerRequest{
		Email: "pi@example.test",
	}, "")
	require.NoError(t, err)

	pi, err := p.CreatePaymentIntent(context.Background(), stripe.CreatePaymentIntentRequest{
		Amount:   1500,
		Currency: "usd",
		Customer: cus.ID,
		Metadata: map[string]any{"tenant_id": "t-abc", "user_id": "u-1"},
	}, "idem-pi-1")
	require.NoError(t, err)
	assert.NotEmpty(t, pi.ID)
	assert.NotEmpty(t, pi.ClientSecret)
	assert.Equal(t, int64(1500), pi.Amount)
	assert.Equal(t, cus.ID, pi.Customer)
	assert.Equal(t, "t-abc", pi.Metadata["tenant_id"])
}

func TestProvider_CreateSetupIntent(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	si, err := p.CreateSetupIntent(context.Background(), stripe.CreateSetupIntentRequest{
		Customer: "",
	}, "")
	require.NoError(t, err)
	assert.NotEmpty(t, si.ID)
	assert.NotEmpty(t, si.ClientSecret)
	assert.Equal(t, "requires_confirmation", si.Status)
}

func TestProvider_CreateProductAndPrice(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	prod, err := p.CreateProduct(context.Background(), stripe.CreateProductRequest{
		Name:        "Pro Plan",
		Description: "Pro tier monthly",
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "Pro Plan", prod.Name)

	price, err := p.CreatePrice(context.Background(), stripe.CreatePriceRequest{
		Currency:      "usd",
		UnitAmount:    2000,
		Product:       prod.ID,
		Interval:      "month",
		IntervalCount: 1,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, int64(2000), price.UnitAmount)
	assert.Equal(t, "month", price.Recurring.Interval)

	got, err := p.GetPrice(context.Background(), price.ID)
	require.NoError(t, err)
	assert.Equal(t, price.ID, got.ID)
}

func TestProvider_CreateAndCancelSubscription(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	cus, err := p.CreateCustomer(context.Background(), stripe.CreateCustomerRequest{
		Email: "sub@example.test",
	}, "")
	require.NoError(t, err)
	prod, err := p.CreateProduct(context.Background(), stripe.CreateProductRequest{
		Name: "Pro",
	}, "")
	require.NoError(t, err)
	price, err := p.CreatePrice(context.Background(), stripe.CreatePriceRequest{
		Currency:   "usd",
		UnitAmount: 2500,
		Product:    prod.ID,
		Interval:   "month",
	}, "")
	require.NoError(t, err)

	sub, err := p.CreateSubscription(context.Background(), stripe.CreateSubscriptionRequest{
		Customer: cus.ID,
		Price:    price.ID,
	}, "")
	require.NoError(t, err)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, cus.ID, sub.Customer)

	// Cancel at period end.
	canceled, err := p.CancelSubscription(context.Background(), sub.ID, true)
	require.NoError(t, err)
	assert.Equal(t, "canceled", canceled.Status)

	// List for customer.
	list, err := p.ListSubscriptionsForCustomer(context.Background(), cus.ID, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, sub.ID, list[0].ID)
}

func TestProvider_BadRequest_ErrorMapping(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	p := mustNewProvider(t, srv)

	// GET on a non-existent resource.
	_, err := p.GetPaymentIntent(context.Background(), "pi_missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, stripe.ErrNotFound)
}

func TestProvider_AuthorizationHeaderRequired(t *testing.T) {
	t.Parallel()
	srv := fake.NewServer(t)
	// Provider with the wrong key — every call should return 401.
	p, err := stripe.NewClient(stripe.Config{
		HTTPClient: &http.Client{},
		BaseURL:    srv.HTTP.URL,
		SecretKey:  "sk_test_wrong",
	})
	require.NoError(t, err)
	_, err = p.GetPaymentIntent(context.Background(), "pi_1")
	require.Error(t, err)
	assert.ErrorIs(t, err, stripe.ErrUnauthenticated)
}

func TestProvider_NewClient_Validation(t *testing.T) {
	t.Parallel()
	_, err := stripe.NewClient(stripe.Config{SecretKey: "x"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "HTTPClient"))

	_, err = stripe.NewClient(stripe.Config{HTTPClient: &http.Client{}})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "SecretKey"))
}

func TestProvider_Capabilities(t *testing.T) {
	t.Parallel()
	p := mustNewProvider(t, fake.NewServer(t))
	caps := p.Capabilities()
	assert.NotEmpty(t, caps.APIVersion)
	assert.False(t, caps.LiveMode, "sk_test_* should report test mode")
	assert.False(t, caps.WebhooksEnabled, "no webhook secret at construction")

	p2, err := stripe.NewClient(stripe.Config{
		HTTPClient:    &http.Client{},
		BaseURL:       fake.NewServer(t).HTTP.URL,
		SecretKey:     "sk_live_xyz",
		WebhookSecret: "whsec_x",
	})
	require.NoError(t, err)
	caps2 := p2.Capabilities()
	assert.True(t, caps2.LiveMode)
	assert.True(t, caps2.WebhooksEnabled)
}

// mustNewProvider is the test helper that wraps NewClient + asserts
// success. The returned Provider points at the supplied fake server.
func mustNewProvider(t *testing.T, srv *fake.Server) *stripe.Provider {
	t.Helper()
	p, err := stripe.NewClient(stripe.Config{
		HTTPClient: &http.Client{},
		BaseURL:    srv.HTTP.URL,
		SecretKey:  srv.SecretKey,
	})
	require.NoError(t, err)
	return p
}
