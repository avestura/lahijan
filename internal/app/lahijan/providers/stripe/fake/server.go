// Package fake provides an httptest-based fake of the Stripe REST API.
// The fake is intentionally in-memory and per-test scoped: every test
// that needs a Stripe backend spins one up via NewServer, points a real
// *stripe.Provider at it via stripe.NewClient, and tears it down at the
// end of the test.
//
// The fake implements the subset of the Stripe REST surface Lahijan's
// driver uses: customers, payment_methods, payment_intents,
// setup_intents, products + prices, subscriptions, and webhook
// signature verification helpers. Stripe's real API delegates data to
// its backend; the fake keeps it in maps.
//
// Concurrency: the fake serializes writes behind a sync.Mutex. Reads
// are goroutine-safe.
package fake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
)

// Server is an in-memory fake of the Stripe API. Construct via
// NewServer.
type Server struct {
	// HTTP is the underlying httptest.Server. Tests dial it via the
	// URL returned by HTTP.URL when constructing a real *stripe.Provider.
	HTTP *httptest.Server

	// SecretKey is the key the fake expects on every request in the
	// Authorization: Bearer header. When non-empty, requests without
	// (or with a mismatched) key are rejected with 401.
	SecretKey string

	// WebhookSecret is the `whsec_...` secret the fake uses to sign
	// webhook deliveries emitted via EmitWebhookBytes. Tests pass the
	// same value to stripe.VerifyWebhook when verifying.
	WebhookSecret string

	// LiveMode is reported back on objects' `livemode` field. false
	// by default (test mode).
	LiveMode bool

	mu             sync.Mutex
	customers      map[string]*fakeCustomer
	paymentMethods map[string]*stripe.PaymentMethod
	paymentIntents map[string]*stripe.PaymentIntent
	setupIntents   map[string]*stripe.SetupIntent
	products       map[string]*stripe.Product
	prices         map[string]*stripe.Price
	subscriptions  map[string]*stripe.Subscription
	idCounter      atomic.Int64

	// idempotency stores Idempotency-Key header -> JSON response body.
	// A repeat POST with the same key returns the cached response
	// without re-executing the handler, mirroring Stripe's behaviour.
	idempotency map[string]cachedResponse

	// emitHook is called by every mutating handler after the change
	// commits. Tests use it to assert the driver's call landed.
	emitHook func(method, path string)
}

// cachedResponse is a cached idempotent response. The body is the raw
// JSON the original handler wrote; a replay returns it verbatim.
type cachedResponse struct {
	status int
	body   []byte
}

// fakeCustomer is the per-customer in-memory state.
type fakeCustomer struct {
	customer       stripe.Customer
	paymentMethods []string // pm_ ids
}

// NewServer returns a started fake Stripe server. The server is alive;
// the caller is responsible for calling HTTP.Close at the end of the
// test (NewServer wires t.Cleanup automatically).
//
// Defaults: SecretKey="sk_test_fake", WebhookSecret="whsec_fake",
// LiveMode=false.
func NewServer(t testing.TB) *Server {
	t.Helper()
	s := newServer()
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(func() { s.HTTP.Close() })
	return s
}

// NewServerStandalone returns a started fake Stripe server without
// wiring testing.TB.Cleanup. The caller MUST call s.HTTP.Close() when
// done. Used by long-running test harnesses that drive a real Provider
// from outside a *testing.T.
func NewServerStandalone() *Server {
	s := newServer()
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handler))
	return s
}

func newServer() *Server {
	return &Server{
		SecretKey:      "sk_test_fake",
		WebhookSecret:  "whsec_fake",
		customers:      map[string]*fakeCustomer{},
		paymentMethods: map[string]*stripe.PaymentMethod{},
		paymentIntents: map[string]*stripe.PaymentIntent{},
		setupIntents:   map[string]*stripe.SetupIntent{},
		products:       map[string]*stripe.Product{},
		prices:         map[string]*stripe.Price{},
		subscriptions:  map[string]*stripe.Subscription{},
		idempotency:    map[string]cachedResponse{},
	}
}

// SetEmitHook wires a callback the fake invokes after every successful
// mutation. Tests use it to assert the driver's call landed.
func (s *Server) SetEmitHook(fn func(method, path string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitHook = fn
}

// nextID returns a fresh sequential id with the supplied prefix.
func (s *Server) nextID(prefix string) string {
	n := s.idCounter.Add(1)
	return fmt.Sprintf("%s_%d", prefix, n)
}

// handler dispatches every request to the area-specific handler. It
// also handles idempotency-key replay short-circuit + the auth check.
func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		writeErr(w, http.StatusNotFound, "resource_missing", "unknown_path", r.URL.Path)
		return
	}
	if s.SecretKey != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+s.SecretKey {
			writeErr(w, http.StatusUnauthorized, "invalid_request_error", "invalid_key", "Invalid API Key provided")
			return
		}
	}
	// Idempotency-Key replay: GET is exempt (it's safe to repeat).
	if r.Method != http.MethodGet {
		idemKey := r.Header.Get("Idempotency-Key")
		if idemKey != "" {
			s.mu.Lock()
			cached, ok := s.idempotency[idemKey]
			s.mu.Unlock()
			if ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(cached.status)
				_, _ = w.Write(cached.body)
				return
			}
		}
	}
	// Capture the response for caching when the handler finishes.
	rec := &captureWriter{header: http.Header{}, status: http.StatusOK, buf: nil}
	idemKey := r.Header.Get("Idempotency-Key")
	s.dispatch(rec, r)
	// Forward to the real writer.
	for k, vs := range rec.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.status)
	_, _ = w.Write(rec.buf)
	// Cache the response if a non-GET with an idempotency key succeeded.
	if idemKey != "" && r.Method != http.MethodGet && rec.status >= 200 && rec.status < 300 {
		s.mu.Lock()
		s.idempotency[idemKey] = cachedResponse{status: rec.status, body: rec.buf}
		s.mu.Unlock()
	}
}

// captureWriter is a tiny http.ResponseWriter that buffers the
// handler's output so the dispatcher can forward it to the real writer
// after the handler finishes (and cache it for idempotency replays).
type captureWriter struct {
	header http.Header
	status int
	buf    []byte
}

func (c *captureWriter) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *captureWriter) WriteHeader(status int) { c.status = status }

func (c *captureWriter) Write(b []byte) (int, error) {
	c.buf = append(c.buf, b...)
	return len(b), nil
}

// dispatch routes the request to the area-specific handler.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/customers"):
		s.handleCustomers(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/payment_methods"):
		s.handlePaymentMethods(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/payment_intents"):
		s.handlePaymentIntents(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/setup_intents"):
		s.handleSetupIntents(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/products"):
		s.handleProducts(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/prices"):
		s.handlePrices(w, r)
	case strings.HasPrefix(r.URL.Path, "/v1/subscriptions"):
		s.handleSubscriptions(w, r)
	default:
		writeErr(w, http.StatusNotFound, "resource_missing", "unknown_path", r.URL.Path)
	}
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, typ, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"type":    typ,
			"code":    code,
			"message": msg,
		},
	})
}

func readForm(r *http.Request) (map[string][]string, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	return map[string][]string(r.PostForm), nil
}

func formGet(form map[string][]string, key string) string {
	if vs, ok := form[key]; ok && len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// formMetadata extracts the metadata[k] => v map from the form.
func formMetadata(form map[string][]string) map[string]any {
	out := map[string]any{}
	for k, vs := range form {
		if !strings.HasPrefix(k, "metadata[") || !strings.HasSuffix(k, "]") {
			continue
		}
		key := k[len("metadata[") : len(k)-1]
		if len(vs) > 0 {
			out[key] = vs[0]
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Customers.
// ---------------------------------------------------------------------------

func (s *Server) handleCustomers(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/customers")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodPost:
		form, err := readForm(r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request_error", "parse_error", err.Error())
			return
		}
		id := s.nextID("cus")
		c := stripe.Customer{
			ID:          id,
			Email:       formGet(form, "email"),
			Name:        formGet(form, "name"),
			Description: formGet(form, "description"),
			Metadata:    formMetadata(form),
		}
		s.mu.Lock()
		s.customers[id] = &fakeCustomer{customer: c}
		hook := s.emitHook
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, c)
		if hook != nil {
			hook("POST", r.URL.Path)
		}
	case rest == "" && r.Method == http.MethodGet:
		// List.
		email := r.URL.Query().Get("email")
		out := struct {
			Object  string            `json:"object"`
			Data    []stripe.Customer `json:"data"`
			HasMore bool              `json:"has_more"`
			URL     string            `json:"url"`
		}{Object: "list"}
		s.mu.Lock()
		for _, fc := range s.customers {
			if email != "" && fc.customer.Email != email {
				continue
			}
			out.Data = append(out.Data, fc.customer)
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
	case rest != "" && r.Method == http.MethodGet:
		s.mu.Lock()
		fc, ok := s.customers[rest]
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such customer: "+rest)
			return
		}
		writeJSON(w, http.StatusOK, fc.customer)
	case rest != "" && r.Method == http.MethodPost:
		// Update (set default_payment_method).
		form, _ := readForm(r)
		dpm := formGet(form, "invoice_settings[default_payment_method]")
		s.mu.Lock()
		fc, ok := s.customers[rest]
		if !ok {
			s.mu.Unlock()
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such customer: "+rest)
			return
		}
		if dpm != "" {
			fc.customer.DefaultPaymentMethod = dpm
		}
		c := fc.customer
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, c)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", r.Method)
	}
}

// ---------------------------------------------------------------------------
// Payment methods.
// ---------------------------------------------------------------------------

func (s *Server) handlePaymentMethods(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/payment_methods")
	rest = strings.TrimPrefix(rest, "/")
	parts := strings.Split(rest, "/")
	if rest == "" {
		// GET list by customer.
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", r.Method)
			return
		}
		customerID := r.URL.Query().Get("customer")
		out := struct {
			Object  string               `json:"object"`
			Data    []stripe.PaymentMethod `json:"data"`
			HasMore bool                 `json:"has_more"`
		}{Object: "list"}
		s.mu.Lock()
		for _, pm := range s.paymentMethods {
			if customerID != "" && pm.Customer != customerID {
				continue
			}
			out.Data = append(out.Data, *pm)
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return
	}
	pmID := parts[0]
	if len(parts) >= 2 && parts[1] == "attach" {
		// POST /v1/payment_methods/{pm}/attach.
		form, _ := readForm(r)
		cus := formGet(form, "customer")
		s.mu.Lock()
		pm, ok := s.paymentMethods[pmID]
		if !ok {
			s.mu.Unlock()
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such PaymentMethod: "+pmID)
			return
		}
		pm.Customer = cus
		if fc, ok := s.customers[cus]; ok {
			fc.paymentMethods = append(fc.paymentMethods, pmID)
		}
		out := *pm
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if len(parts) >= 2 && parts[1] == "detach" {
		s.mu.Lock()
		pm, ok := s.paymentMethods[pmID]
		if !ok {
			s.mu.Unlock()
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such PaymentMethod: "+pmID)
			return
		}
		pm.Customer = ""
		out := *pm
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return
	}
	// GET /v1/payment_methods/{pm}.
	s.mu.Lock()
	pm, ok := s.paymentMethods[pmID]
	s.mu.Unlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such PaymentMethod: "+pmID)
		return
	}
	writeJSON(w, http.StatusOK, pm)
}

// AttachPaymentMethod is a test helper that mints a fake PaymentMethod
// + attaches it to the supplied customer. Returns the pm_id. Used by
// the webhook + payment tests that need a card on file without going
// through the SPA flow.
func (s *Server) AttachPaymentMethod(customerID, brand, last4, fingerprint string) string {
	pmID := s.nextID("pm")
	pm := &stripe.PaymentMethod{
		ID:       pmID,
		Type:     "card",
		Customer: customerID,
		Card: stripe.PaymentMethodCard{
			Brand:       brand,
			Last4:       last4,
			Fingerprint: fingerprint,
			ExpMonth:    12,
			ExpYear:     2030,
		},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paymentMethods[pmID] = pm
	if fc, ok := s.customers[customerID]; ok {
		fc.paymentMethods = append(fc.paymentMethods, pmID)
	}
	return pmID
}

// ---------------------------------------------------------------------------
// Payment intents.
// ---------------------------------------------------------------------------

func (s *Server) handlePaymentIntents(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/payment_intents")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" && r.Method == http.MethodPost {
		form, _ := readForm(r)
		id := s.nextID("pi")
		amount, _ := strconv.ParseInt(formGet(form, "amount"), 10, 64)
		currency := formGet(form, "currency")
		if currency == "" {
			currency = "usd"
		}
		pi := &stripe.PaymentIntent{
			ID:            id,
			Amount:        amount,
			Currency:      currency,
			Status:        "requires_confirmation",
			ClientSecret:  id + "_secret_" + s.nextID(""),
			Customer:      formGet(form, "customer"),
			PaymentMethod: formGet(form, "payment_method"),
			Metadata:      formMetadata(form),
		}
		s.mu.Lock()
		s.paymentIntents[id] = pi
		hook := s.emitHook
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, pi)
		if hook != nil {
			hook("POST", r.URL.Path)
		}
		return
	}
	if rest == "" {
		writeErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", r.Method)
		return
	}
	s.mu.Lock()
	pi, ok := s.paymentIntents[rest]
	s.mu.Unlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such PaymentIntent: "+rest)
		return
	}
	writeJSON(w, http.StatusOK, pi)
}

// ---------------------------------------------------------------------------
// Setup intents.
// ---------------------------------------------------------------------------

func (s *Server) handleSetupIntents(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/setup_intents")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" && r.Method == http.MethodPost {
		form, _ := readForm(r)
		id := s.nextID("seti")
		si := &stripe.SetupIntent{
			ID:            id,
			Status:        "requires_confirmation",
			ClientSecret:  id + "_secret_" + s.nextID(""),
			Customer:      formGet(form, "customer"),
			PaymentMethod: formGet(form, "payment_method"),
			Metadata:      formMetadata(form),
		}
		if formGet(form, "confirm") == "true" && si.PaymentMethod != "" {
			si.Status = "succeeded"
		}
		s.mu.Lock()
		s.setupIntents[id] = si
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, si)
		return
	}
	if rest == "" {
		writeErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", r.Method)
		return
	}
	s.mu.Lock()
	si, ok := s.setupIntents[rest]
	s.mu.Unlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such SetupIntent: "+rest)
		return
	}
	writeJSON(w, http.StatusOK, si)
}

// ---------------------------------------------------------------------------
// Products + prices.
// ---------------------------------------------------------------------------

func (s *Server) handleProducts(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/products")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" && r.Method == http.MethodGet {
		// List (also serves as the ping probe).
		out := struct {
			Object  string            `json:"object"`
			Data    []stripe.Product  `json:"data"`
			HasMore bool              `json:"has_more"`
			URL     string            `json:"url"`
		}{Object: "list"}
		s.mu.Lock()
		for _, p := range s.products {
			out.Data = append(out.Data, *p)
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if rest == "" && r.Method == http.MethodPost {
		form, _ := readForm(r)
		id := s.nextID("prod")
		p := &stripe.Product{
			ID:          id,
			Name:        formGet(form, "name"),
			Description: formGet(form, "description"),
			Active:      true,
			Metadata:    formMetadata(form),
		}
		s.mu.Lock()
		s.products[id] = p
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, p)
		return
	}
	writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "unknown products path: "+r.URL.Path)
}

func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/prices")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" && r.Method == http.MethodPost {
		form, _ := readForm(r)
		id := s.nextID("price")
		amount, _ := strconv.ParseInt(formGet(form, "unit_amount"), 10, 64)
		price := &stripe.Price{
			ID:         id,
			Product:    formGet(form, "product"),
			Active:     true,
			Currency:   formGet(form, "currency"),
			UnitAmount: amount,
			Type:       "recurring",
			Metadata:   formMetadata(form),
			Recurring: &stripe.RecurringInfo{
				Interval:      formGet(form, "recurring[interval]"),
				IntervalCount: 1,
				UsageType:     "licensed",
			},
		}
		s.mu.Lock()
		s.prices[id] = price
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, price)
		return
	}
	if rest != "" && r.Method == http.MethodGet {
		s.mu.Lock()
		price, ok := s.prices[rest]
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such Price: "+rest)
			return
		}
		writeJSON(w, http.StatusOK, price)
		return
	}
	writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "unknown prices path: "+r.URL.Path)
}

// ---------------------------------------------------------------------------
// Subscriptions.
// ---------------------------------------------------------------------------

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" && r.Method == http.MethodPost {
		form, _ := readForm(r)
		id := s.nextID("sub")
		priceID := formGet(form, "items[0][price]")
		customerID := formGet(form, "customer")
		now := time.Now().Unix()
		periodEnd := now + 30*24*3600 // 30 days
		sub := &stripe.Subscription{
			ID:                 id,
			Status:             "active",
			Customer:           customerID,
			Metadata:           formMetadata(form),
			CurrentPeriodStart: now,
			CurrentPeriodEnd:   periodEnd,
		}
		sub.Items.Data = []stripe.SubscriptionItem{{
			ID:           s.nextID("si"),
			Price:        priceID,
			Quantity:     1,
			Subscription: id,
		}}
		s.mu.Lock()
		s.subscriptions[id] = sub
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, sub)
		return
	}
	if rest == "" && r.Method == http.MethodGet {
		// List for customer.
		customerID := r.URL.Query().Get("customer")
		out := struct {
			Object  string                `json:"object"`
			Data    []stripe.Subscription `json:"data"`
			HasMore bool                  `json:"has_more"`
		}{Object: "list"}
		s.mu.Lock()
		for _, sub := range s.subscriptions {
			if customerID != "" && sub.Customer != customerID {
				continue
			}
			out.Data = append(out.Data, *sub)
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if rest != "" && r.Method == http.MethodDelete {
		s.mu.Lock()
		sub, ok := s.subscriptions[rest]
		if !ok {
			s.mu.Unlock()
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such Subscription: "+rest)
			return
		}
		sub.Status = "canceled"
		out := *sub
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
		return
	}
	if rest != "" && r.Method == http.MethodGet {
		s.mu.Lock()
		sub, ok := s.subscriptions[rest]
		s.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "No such Subscription: "+rest)
			return
		}
		writeJSON(w, http.StatusOK, sub)
		return
	}
	writeErr(w, http.StatusNotFound, "resource_missing", "resource_missing", "unknown subscriptions path: "+r.URL.Path)
}

// ---------------------------------------------------------------------------
// Webhook delivery helper.
// ---------------------------------------------------------------------------

// EmitWebhookBytes returns the body + signature header a test would
// receive from Stripe for the supplied event. Used to drive the
// webhook receiver in-process.
func (s *Server) EmitWebhookBytes(event stripe.Event) (body []byte, sigHeader string) {
	body, _ = json.Marshal(event)
	ts := time.Now().Unix()
	payloadToSign := []byte(strconv.FormatInt(ts, 10) + ".")
	payloadToSign = append(payloadToSign, body...)
	mac := hmac.New(sha256.New, []byte(s.WebhookSecret))
	mac.Write(payloadToSign)
	sig := hex.EncodeToString(mac.Sum(nil))
	return body, fmt.Sprintf("t=%d,v1=%s", ts, sig)
}

// SetLiveMode flips the LiveMode flag the server reports on object
// responses. Tests that exercise the production-vs-test gating use this.
func (s *Server) SetLiveMode(live bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LiveMode = live
}

// CustomerCount returns the number of customers in the fake (for
// test assertions).
func (s *Server) CustomerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.customers)
}
