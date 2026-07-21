// Package stripe: types.go holds the JSON request/response types for
// the Stripe REST API surface Lahijan uses. The types are intentionally
// minimal — only the fields the consuming service reads or writes are
// modeled. Unknown fields are ignored (default `json.Unmarshal` behaviour).
//
// Stripe's REST API uses snake_case keys; the Go structs use the same
// keys via `json:"..."` tags so the field map is obvious. The Go field
// names are PascalCase per Go convention.
package stripe

import "encoding/json"

// ===========================================================================
// Common: address, money amounts.
// ===========================================================================

// Amount is Stripe's integer-minor-unit amount (e.g. 100 = $1.00 USD).
// Always paired with a "currency" field on the parent object.
type Amount = int64

// ===========================================================================
// Customer.
// ===========================================================================

// Customer is the Lahijan-side view of a Stripe Customer object. The
// platform creates one Customer per (tenant, user) pair on first
// payment-method attach.
type Customer struct {
	ID          string         `json:"id"`
	Email       string         `json:"email,omitempty"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	// DefaultPaymentMethod is the id of the customer's default
	// invoice-payment PaymentMethod ("pm_..."). Empty when none is set.
	DefaultPaymentMethod string `json:"default_payment_method,omitempty"`
}

// CreateCustomerRequest is the body of POST /v1/customers. Email +
// Name + Description are the standard invoice fields; Metadata is
// where Lahijan stores the tenant_id + user_id so the webhook handler
// can resolve them from the event payload.
type CreateCustomerRequest struct {
	Email       string
	Name        string
	Description string
	Metadata    map[string]any
}

// CustomerListParams is the query string for GET /v1/customers.
type CustomerListParams struct {
	Email string
	Limit int
}

// ===========================================================================
// PaymentMethod.
// ===========================================================================

// PaymentMethodCard is the card sub-object on a PaymentMethod of type
// "card". We cache brand + last4 + fingerprint + expiry at attach time
// so the dashboard can render without a Stripe round-trip.
type PaymentMethodCard struct {
	Brand       string `json:"brand"`
	Last4       string `json:"last4"`
	Fingerprint string `json:"fingerprint"`
	ExpMonth    int    `json:"exp_month"`
	ExpYear     int    `json:"exp_year"`
	// Funding is "credit" | "debit" | "prepaid" | "unknown".
	Funding string `json:"funding,omitempty"`
	// Country is the ISO 3166 alpha-2 country code of the card issuer.
	Country string `json:"country,omitempty"`
}

// PaymentMethod is the Lahijan-side view of a Stripe PaymentMethod
// object. Only the "card" type is modeled; other types (us_bank_account,
// sepa_debit, ...) are deferred.
type PaymentMethod struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"` // "card"
	Card     PaymentMethodCard `json:"card,omitempty"`
	Customer string            `json:"customer,omitempty"`
	// LiveMode is false in test mode (Stripe returns the value to
	// disambiguate test vs live objects).
	LiveMode bool `json:"livemode,omitempty"`
}

// AttachPaymentMethodRequest is the body of POST
// /v1/payment_methods/{pm_id}/attach.
type AttachPaymentMethodRequest struct {
	Customer string
}

// ===========================================================================
// PaymentIntent.
// ===========================================================================

// PaymentIntent is the Lahijan-side view of a Stripe PaymentIntent
// object. The SPA confirms the intent via Stripe.js using the
// ClientSecret; Lahijan never sees the card data.
type PaymentIntent struct {
	ID            string         `json:"id"`
	Amount        Amount         `json:"amount"`
	Currency      string         `json:"currency"`
	Status        string         `json:"status"`
	ClientSecret  string         `json:"client_secret"`
	Customer      string         `json:"customer,omitempty"`
	PaymentMethod string         `json:"payment_method,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	// LatestCharge is the id of the most recent Charge created for this
	// PaymentIntent. Useful for ledger reference.
	LatestCharge string `json:"latest_charge,omitempty"`
}

// CreatePaymentIntentRequest is the body of POST /v1/payment_intents.
type CreatePaymentIntentRequest struct {
	Amount        Amount
	Currency      string
	Customer      string
	PaymentMethod string
	Metadata      map[string]any
	// Description is a human-readable note Stripe shows on the dashboard.
	Description string
	// ReceiptEmail is where Stripe sends the receipt. Defaults to the
	// customer's email when omitted.
	ReceiptEmail string
	// StatementDescriptorSuffix appears on the user's card statement.
	// Max 22 chars; Stripe truncates.
	StatementDescriptorSuffix string
}

// ===========================================================================
// SetupIntent.
// ===========================================================================

// SetupIntent is the Lahijan-side view of a Stripe SetupIntent object.
// Used to collect a card without an immediate charge (so the user can
// pay on demand later).
type SetupIntent struct {
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	ClientSecret  string         `json:"client_secret"`
	Customer      string         `json:"customer,omitempty"`
	PaymentMethod string         `json:"payment_method,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// CreateSetupIntentRequest is the body of POST /v1/setup_intents.
type CreateSetupIntentRequest struct {
	Customer      string
	PaymentMethod string
	Metadata      map[string]any
	Description   string
}

// ===========================================================================
// Product + Price (subscription plans).
// ===========================================================================

// Product is the Lahijan-side view of a Stripe Product object. Each
// billing_plan maps to one Stripe Product.
type Product struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Active      bool           `json:"active"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Price is the Lahijan-side view of a Stripe Price object. Each
// billing_plan maps to one Stripe Price (the recurring price).
type Price struct {
	ID         string         `json:"id"`
	Product    string         `json:"product"` // Product id (or object expanded)
	Active     bool           `json:"active"`
	Currency   string         `json:"currency"`
	UnitAmount Amount         `json:"unit_amount"`
	Type       string         `json:"type"` // "recurring"
	Recurring  *RecurringInfo `json:"recurring,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// RecurringInfo is the recurring sub-object on a Price.
type RecurringInfo struct {
	Interval string `json:"interval"` // "month" | "year"
	// IntervalCount is 1 by default; >1 for "every N months/years".
	IntervalCount int    `json:"interval_count"`
	UsageType     string `json:"usage_type"` // "licensed"
}

// CreateProductRequest is the body of POST /v1/products.
type CreateProductRequest struct {
	Name        string
	Description string
	Metadata    map[string]any
}

// CreatePriceRequest is the body of POST /v1/prices.
type CreatePriceRequest struct {
	Currency      string
	UnitAmount    Amount
	Product       string
	Interval      string // "month" | "year"
	IntervalCount int
	Metadata      map[string]any
}

// ===========================================================================
// Subscription.
// ===========================================================================

// Subscription is the Lahijan-side view of a Stripe Subscription object.
type Subscription struct {
	ID                 string         `json:"id"`
	Status             string         `json:"status"`
	Customer           string         `json:"customer"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CurrentPeriodStart int64          `json:"current_period_start"`
	CurrentPeriodEnd   int64          `json:"current_period_end"`
	CancelAt           int64          `json:"cancel_at,omitempty"`
	CanceledAt         int64          `json:"canceled_at,omitempty"`
	Items              struct {
		Object string             `json:"object"`
		Data   []SubscriptionItem `json:"data"`
	} `json:"items"`
}

// SubscriptionItem is one line item in a Subscription.
type SubscriptionItem struct {
	ID           string `json:"id"`
	Price        string `json:"price"`
	Quantity     int64  `json:"quantity"`
	Subscription string `json:"subscription"`
}

// CreateSubscriptionRequest is the body of POST /v1/subscriptions.
type CreateSubscriptionRequest struct {
	Customer string
	Price    string
	// DefaultPaymentMethod overrides the customer's default for this
	// subscription's invoices.
	DefaultPaymentMethod string
	Metadata             map[string]any
}

// ===========================================================================
// Invoice (events only — Lahijan does not generate invoices itself;
// Stripe does).
// ===========================================================================

// Invoice is the Lahijan-side view of a Stripe Invoice object. Used by
// the webhook handler to compute the credit applied on `invoice.paid`.
type Invoice struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"` // "paid" | "open" | ...
	Customer     string         `json:"customer"`
	Subscription string         `json:"subscription,omitempty"`
	Total        Amount         `json:"total"`
	Currency     string         `json:"currency"`
	Paid         bool           `json:"paid"`
	Charge       string         `json:"charge,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// ===========================================================================
// Charge (for disputes).
// ===========================================================================

// Charge is the Lahijan-side view of a Stripe Charge object. Used by
// the dispute webhook handler.
type Charge struct {
	ID            string         `json:"id"`
	Amount        Amount         `json:"amount"`
	Currency      string         `json:"currency"`
	Customer      string         `json:"customer,omitempty"`
	PaymentMethod string         `json:"payment_method,omitempty"`
	Status        string         `json:"status"` // "succeeded" | "failed" | ...
	Metadata      map[string]any `json:"metadata,omitempty"`
	// Disputed is non-empty when a dispute has been opened.
	Dispute *Dispute `json:"dispute,omitempty"`
}

// Dispute is the Lahijan-side view of a Stripe Dispute object.
type Dispute struct {
	ID       string `json:"id"`
	Amount   Amount `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"` // "won" | "lost" | "challenge_needed" | ...
	Reason   string `json:"reason"`
	Charge   string `json:"charge"`
}

// ===========================================================================
// Webhook events.
// ===========================================================================

// Event is the Stripe webhook event envelope. The webhook handler
// dispatches on Type; the per-event-type handlers re-decode Data.Raw
// into the right concrete object (PaymentIntent, Invoice, ...).
type Event struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	APIVersion string    `json:"api_version"`
	Created    int64     `json:"created"`
	Livemode   bool      `json:"livemode"`
	Data       EventData `json:"data"`
	// Request is the idempotency envelope for events triggered by a
	// write API call (vs Stripe-internal triggers). Empty for
	// Stripe-initiated events.
	Request *struct {
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	} `json:"request,omitempty"`
}

// EventData wraps the event payload.
type EventData struct {
	// Object is the raw JSON of the resource the event is about. The
	// per-type handlers re-Unmarshal into the right concrete type.
	Object json.RawMessage `json:"object"`
	// PreviousAttributes carries the before-state for *.updated events.
	// Nil for non-update events.
	PreviousAttributes map[string]any `json:"previous_attributes,omitempty"`
}

// ===========================================================================
// Errors.
// ===========================================================================

// APIError is the Stripe REST error envelope. The Stripe API returns
// this JSON body on every non-2xx response: a small object with type +
// code + message + (optionally) param + a per-error-type details blob.
//
// We only model the fields Lahijan needs at the boundary: the type, the
// code, the message, and the HTTP status code (set by the caller from
// the *http.Response).
type APIError struct {
	// StatusCode is the HTTP status code from the response.
	StatusCode int    `json:"-"`
	Type       string `json:"type"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message"`
	Param      string `json:"param,omitempty"`
	// RequestID is Stripe's request log id (useful for support).
	RequestID string `json:"request_id,omitempty"`
}

// Error implements error.
func (e *APIError) Error() string {
	if e == nil {
		return "stripe: <nil>"
	}
	if e.Message == "" {
		return "stripe: http " + itoa(e.StatusCode)
	}
	return "stripe: http " + itoa(e.StatusCode) + ": " + e.Message
}

// itoa is a tiny strconv.Itoa alternative to avoid pulling strconv into
// this minimal package. Used only for error formatting.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
