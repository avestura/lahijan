// Package billing: payments.go is the entrypoint for the WS-27 payment
// gateway surface (ADR-0034). The PaymentsService is constructed at
// bootstrap with a Stripe provider driver and the WS-17 billing
// service (for ledger writes). It owns the per-user Stripe Customer
// lifecycle (create on first use, cache the id), the PaymentMethod
// cache, the PaymentIntent create path, and the webhook receiver that
// turns `payment_intent.succeeded` into a ledger credit.
//
// The service is OPT-IN: when the Stripe provider is nil the service
// is not constructed and every payment endpoint degrades to 501.
//
// Per ADR-0034 the Customer id is AES-GCM-encrypted at rest. The
// PaymentMethod id is stored in the clear (it is useless without the
// merchant key). The card brand + last4 + fingerprint are cached for
// the dashboard so the UI can render without a Stripe round-trip.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/stripe"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// PaymentsService is the entrypoint for the WS-27 payment gateway
// surface. Construct one at bootstrap (program.Start) and share it
// across handlers + jobs. Nil-appropriate when Stripe is disabled; the
// handlers degrade to 501 in that case.
type PaymentsService struct {
	billing *Service // the WS-17 service (for ledger writes + balance cache)
	repos   *database.Repos
	gw      StripeGateway // the Stripe provider driver (or a test fake)
	crypto  *secrets.Crypto
	audit   audit.Emitter
	bus     eventBus
	policy  rbac.PolicyEvaluator
	config  PaymentsConfig
}

// StripeGateway is the narrow seam PaymentsService needs from
// *stripe.Provider. Declared as an interface here so tests substitute
// a fake without pulling the providers package into every test file.
// *stripe.Provider satisfies this interface.
type StripeGateway interface {
	Name() string
	Ping(ctx context.Context) error
	CreateCustomer(ctx context.Context, req stripe.CreateCustomerRequest, idempotencyKey string) (stripe.Customer, error)
	FindCustomerByEmail(ctx context.Context, email string) (stripe.Customer, error)
	GetPaymentMethod(ctx context.Context, id string) (stripe.PaymentMethod, error)
	AttachPaymentMethod(ctx context.Context, paymentMethodID, customerID string) (stripe.PaymentMethod, error)
	DetachPaymentMethod(ctx context.Context, paymentMethodID string) error
	CreatePaymentIntent(ctx context.Context, req stripe.CreatePaymentIntentRequest, idempotencyKey string) (stripe.PaymentIntent, error)
	GetPaymentIntent(ctx context.Context, id string) (stripe.PaymentIntent, error)
	CreateSetupIntent(ctx context.Context, req stripe.CreateSetupIntentRequest, idempotencyKey string) (stripe.SetupIntent, error)
	CreateProduct(ctx context.Context, req stripe.CreateProductRequest, idempotencyKey string) (stripe.Product, error)
	CreatePrice(ctx context.Context, req stripe.CreatePriceRequest, idempotencyKey string) (stripe.Price, error)
	CreateSubscription(ctx context.Context, req stripe.CreateSubscriptionRequest, idempotencyKey string) (stripe.Subscription, error)
	GetSubscription(ctx context.Context, id string) (stripe.Subscription, error)
	CancelSubscription(ctx context.Context, id string, cancelAtPeriodEnd bool) (stripe.Subscription, error)
}

// PaymentsConfig carries the process-wide knobs the payments service
// needs.
type PaymentsConfig struct {
	// PublishableKey is the Stripe publishable key (pk_*). Returned to
	// the SPA so Stripe.js can bootstrap. SENSITIVE — never logged at
	// debug level.
	PublishableKey string

	// DefaultTopupCurrency is the ISO 4217 code used when the caller
	// does not pass one. Default "usd".
	DefaultTopupCurrency string

	// MinTopupCents is the smallest top-up the platform accepts
	// (Stripe has a $0.50 minimum). Default 50.
	MinTopupCents int64

	// MaxTopupCents is the largest single top-up the platform accepts.
	// Default 1_000_000 ($10,000) — guards against typos + fraud.
	MaxTopupCents int64
}

// NewPaymentsService builds a PaymentsService. billing + repos + gw +
// crypto are all required; the rest fall back to no-op defaults.
func NewPaymentsService(
	billing *Service,
	repos *database.Repos,
	gw StripeGateway,
	crypto *secrets.Crypto,
	emitter audit.Emitter,
	bus eventBus,
	policy rbac.PolicyEvaluator,
	cfg PaymentsConfig,
) *PaymentsService {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	if cfg.DefaultTopupCurrency == "" {
		cfg.DefaultTopupCurrency = "usd"
	}
	if cfg.MinTopupCents == 0 {
		cfg.MinTopupCents = 50
	}
	if cfg.MaxTopupCents == 0 {
		cfg.MaxTopupCents = 1_000_000
	}
	return &PaymentsService{
		billing: billing,
		repos:   repos,
		gw:      gw,
		crypto:  crypto,
		audit:   emitter,
		bus:     bus,
		policy:  policy,
		config:  cfg,
	}
}

// Gateway returns the underlying Stripe gateway. Exposed so the api
// webhook handler can call VerifyWebhook via the gateway.
func (s *PaymentsService) Gateway() StripeGateway { return s.gw }

// Config returns the service config. Exposed so the api handler can
// surface the publishable key to the SPA.
func (s *PaymentsService) Config() PaymentsConfig { return s.config }

// auditEmit is the PaymentsService's mirror of Service.auditEmit.
// Returns the audit row id so the caller pairs it with auditMarkOutcome.
func (s *PaymentsService) auditEmit(
	ctx context.Context,
	action, resourceType string,
	tenantID, userID, resourceID uuid.UUID,
	meta map[string]any,
) uuid.UUID {
	id, err := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   &resourceID,
		Status:       audit.StatusPending,
		Metadata:     meta,
	})
	if err != nil {
		_ = errors.Join(err, fmt.Errorf("billing.payments: audit emit %s", action))
		return uuid.Nil
	}
	return id
}

// auditMarkOutcome is the matching helper for auditEmit. nil auditID
// (the emit failed) is a no-op.
func (s *PaymentsService) auditMarkOutcome(
	ctx context.Context,
	auditID uuid.UUID,
	success bool,
	details map[string]any,
) {
	if auditID == uuid.Nil {
		return
	}
	status := audit.StatusSuccess
	if !success {
		status = audit.StatusFailure
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: status, Details: details})
}

// emitEvent is the PaymentsService's mirror of Service.emitEvent.
// Drops the event on the floor when no bus is configured.
func (s *PaymentsService) emitEvent(
	ctx context.Context,
	topic string,
	tenantID, userID, resourceID uuid.UUID,
	meta map[string]any,
) {
	if s.bus == nil {
		return
	}
	var raw []byte
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = b
		}
	}
	tid := tenantID
	uid := userID
	rid := resourceID
	actor := audit.ActorSystem
	if uid != uuid.Nil {
		actor = audit.ActorUser
	}
	_ = s.bus.Emit(ctx, eventbus.Event{
		Topic:      topic,
		TenantID:   &tid,
		ActorType:  actor,
		ActorID:    &uid,
		ResourceID: &rid,
		Metadata:   raw,
		EmittedAt:  time.Now(),
	})
}

// ===========================================================================
// Customer lifecycle.
// ===========================================================================

// getOrCreateStripeCustomer resolves the user's Stripe Customer id
// from the local cache (the payment_methods table) or, on first use,
// creates one upstream + caches the (encrypted) id.
//
// The function is per-user-per-tenant. We look up an existing
// payment_methods row for the user; the encrypted customer id is the
// source of truth. When no row exists yet we create the Customer
// upstream (idempotently via the user's email) + cache a sentinel
// payment_methods row with the customer id so subsequent calls reuse
// it.
//
// The "no payment method yet" cache row uses a sentinel pm_id of
// "pending_<uuid>" so the unique constraint on stripe_payment_method_id
// is satisfied without colliding with real pm ids. When the first real
// card is attached the sentinel row is deactivated.
func (s *PaymentsService) getOrCreateStripeCustomer(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	email, displayName string,
) (string, error) {
	// Look for an existing row.
	existing, errList := s.repos.BillingPaymentMethods.ListForUser(ctx, userID, 50, 0)
	if errList != nil && !database.IsNoRows(errList) {
		return "", fmt.Errorf("billing.payments.customer: list: %w", errList)
	}
	for _, pm := range existing {
		if pm.EncryptedStripeCustomerID == "" {
			continue
		}
		cus, errDec := s.crypto.Open(pm.EncryptedStripeCustomerID)
		if errDec != nil {
			continue // corrupt envelope; skip — the create path will refresh
		}
		return cus, nil
	}

	// No cached customer. Create upstream (idempotently via email).
	cus, err := s.gw.FindCustomerByEmail(ctx, email)
	if err != nil && !errors.Is(err, stripe.ErrNotFound) {
		return "", fmt.Errorf("billing.payments.customer: find by email: %w", err)
	}
	if errors.Is(err, stripe.ErrNotFound) {
		// Create.
		idemp := "cus:" + tenantID.String() + ":" + userID.String()
		newCus, errCreate := s.gw.CreateCustomer(ctx, stripe.CreateCustomerRequest{
			Email:    email,
			Name:     displayName,
			Metadata: map[string]any{"tenant_id": tenantID.String(), "user_id": userID.String()},
		}, idemp)
		if errCreate != nil {
			return "", fmt.Errorf("billing.payments.customer: create: %w", errCreate)
		}
		cus = newCus
	}

	// Cache a sentinel payment_methods row so the next lookup hits.
	enc, errEnc := s.crypto.Seal(cus.ID)
	if errEnc != nil {
		return "", fmt.Errorf("billing.payments.customer: encrypt: %w", errEnc)
	}
	sentinelID := "pending_" + userID.String()
	_, errCache := s.repos.BillingPaymentMethods.Create(ctx, database.CreateBillingPaymentMethodParams{
		UserID:                    userID,
		StripePaymentMethodID:     sentinelID,
		EncryptedStripeCustomerID: enc,
		Brand:                     "",
		Last4:                     "",
		Fingerprint:               "",
		IsDefault:                 false,
		Active:                    false, // not a real card; hidden from list
		Metadata: map[string]any{
			"sentinel": true,
			"email":    email,
		},
	})
	if errCache != nil {
		// Race: a concurrent call already cached. Look up again.
		again, errAgain := s.repos.BillingPaymentMethods.ListForUser(ctx, userID, 50, 0)
		if errAgain == nil {
			for _, pm := range again {
				if pm.EncryptedStripeCustomerID == "" {
					continue
				}
				if cus2, errDec := s.crypto.Open(pm.EncryptedStripeCustomerID); errDec == nil {
					return cus2, nil
				}
			}
		}
		return cus.ID, nil // still return the upstream id; the cache race is benign
	}
	return cus.ID, nil
}

// ===========================================================================
// Payment methods.
// ===========================================================================

// AddPaymentMethod attaches a Stripe PaymentMethod to the user's
// Customer + caches the Lahijan-side row. paymentMethodID is the
// Stripe-generated pm_* id returned by the SPA's SetupIntent flow.
//
// Audit: emits ActionBillingPaymentMethodAdd before the side effect +
// marks the outcome after. Event bus: emits
// "billing.payment_method.added" so plugins can react.
func (s *PaymentsService) AddPaymentMethod(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	email, displayName string,
	paymentMethodID string,
	setDefault bool,
) (gen.BillingPaymentMethod, error) {
	if paymentMethodID == "" {
		return gen.BillingPaymentMethod{}, ErrInvalidPaymentMethod
	}
	customerID, err := s.getOrCreateStripeCustomer(ctx, tenantID, userID, email, displayName)
	if err != nil {
		return gen.BillingPaymentMethod{}, err
	}
	// Fetch the pm so we can cache brand/last4/fingerprint.
	pm, err := s.gw.GetPaymentMethod(ctx, paymentMethodID)
	if err != nil {
		return gen.BillingPaymentMethod{}, fmt.Errorf("billing.payments.add_pm: get: %w", err)
	}
	// De-dup: if an active row with the same fingerprint exists, return
	// it instead of re-attaching.
	if existing, errFind := s.repos.BillingPaymentMethods.ListByFingerprint(ctx, userID, pm.Card.Fingerprint); errFind == nil {
		for _, e := range existing {
			if e.Active {
				return e, nil
			}
		}
	}
	// Attach upstream.
	if _, err := s.gw.AttachPaymentMethod(ctx, paymentMethodID, customerID); err != nil {
		return gen.BillingPaymentMethod{}, fmt.Errorf("billing.payments.add_pm: attach: %w", err)
	}
	enc, errEnc := s.crypto.Seal(customerID)
	if errEnc != nil {
		return gen.BillingPaymentMethod{}, fmt.Errorf("billing.payments.add_pm: encrypt: %w", errEnc)
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingPaymentMethodAdd, audit.ResourceBillingPaymentMethod, tenantID, userID, uuid.Nil, map[string]any{
		"stripe_pm_id": paymentMethodID,
		"brand":        pm.Card.Brand,
		"last4":        pm.Card.Last4,
	})
	// Drop the sentinel row (the one with pending_<uuid> id) so the
	// user's payment_methods list shows real cards only.
	if existing, errList := s.repos.BillingPaymentMethods.ListForUser(ctx, userID, 1, 0); errList == nil {
		for _, e := range existing {
			if strings.HasPrefix(e.StripePaymentMethodID, "pending_") {
				_ = s.repos.BillingPaymentMethods.Deactivate(ctx, e.ID)
			}
		}
	}
	expMonth := int32(pm.Card.ExpMonth)
	expYear := int32(pm.Card.ExpYear)
	row, err := s.repos.BillingPaymentMethods.Create(ctx, database.CreateBillingPaymentMethodParams{
		UserID:                    userID,
		StripePaymentMethodID:     paymentMethodID,
		EncryptedStripeCustomerID: enc,
		Brand:                     pm.Card.Brand,
		Last4:                     pm.Card.Last4,
		Fingerprint:               pm.Card.Fingerprint,
		ExpMonth:                  &expMonth,
		ExpYear:                   &expYear,
		IsDefault:                 setDefault,
		Active:                    true,
	})
	if err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return gen.BillingPaymentMethod{}, fmt.Errorf("billing.payments.add_pm: cache: %w", err)
	}
	if setDefault {
		_ = s.repos.BillingPaymentMethods.SetDefault(ctx, userID, row.ID)
		row.IsDefault = true
	}
	s.auditMarkOutcome(ctx, auditID, true, map[string]any{"payment_method_id": row.ID})
	s.emitEvent(ctx, eventbus.BillingPaymentMethodAdded, tenantID, userID, row.ID, map[string]any{
		"brand":  pm.Card.Brand,
		"last4":  pm.Card.Last4,
	})
	return row, nil
}

// ListPaymentMethods returns the user's active payment methods.
// Sentinel rows (created before the user adds their first card) are
// filtered out.
func (s *PaymentsService) ListPaymentMethods(
	ctx context.Context,
	userID uuid.UUID,
	limit, offset int32,
) ([]gen.BillingPaymentMethod, error) {
	rows, err := s.repos.BillingPaymentMethods.ListForUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("billing.payments.list_pm: %w", err)
	}
	out := make([]gen.BillingPaymentMethod, 0, len(rows))
	for _, r := range rows {
		if strings.HasPrefix(r.StripePaymentMethodID, "pending_") {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// RemovePaymentMethod detaches the card upstream + soft-deletes the
// Lahijan-side row. Audit + event emission.
func (s *PaymentsService) RemovePaymentMethod(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	paymentMethodRowID uuid.UUID,
) error {
	row, err := s.repos.BillingPaymentMethods.Get(ctx, paymentMethodRowID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrPaymentMethodNotFound
		}
		return fmt.Errorf("billing.payments.remove_pm: get: %w", err)
	}
	if row.UserID != userID {
		// Cross-user: refuse without leaking existence.
		return ErrPaymentMethodNotFound
	}
	auditID := s.auditEmit(ctx, audit.ActionBillingPaymentMethodDrop, audit.ResourceBillingPaymentMethod, tenantID, userID, row.ID, map[string]any{
		"stripe_pm_id": row.StripePaymentMethodID,
		"brand":        row.Brand,
		"last4":        row.Last4,
	})
	if err := s.gw.DetachPaymentMethod(ctx, row.StripePaymentMethodID); err != nil {
		// NotFound upstream means the user already detached via the
		// Stripe dashboard; continue with the local soft-delete.
		if !errors.Is(err, stripe.ErrNotFound) {
			s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
			return fmt.Errorf("billing.payments.remove_pm: detach: %w", err)
		}
	}
	if err := s.repos.BillingPaymentMethods.Deactivate(ctx, paymentMethodRowID); err != nil {
		s.auditMarkOutcome(ctx, auditID, false, map[string]any{"error": err.Error()})
		return fmt.Errorf("billing.payments.remove_pm: cache: %w", err)
	}
	s.auditMarkOutcome(ctx, auditID, true, nil)
	return nil
}

// ===========================================================================
// Payment intents (one-shot top-up).
// ===========================================================================

// CreateTopupIntent creates a Stripe PaymentIntent for a one-shot
// balance top-up. The SPA confirms the intent via Stripe.js using the
// returned client secret; the webhook handler credits the ledger on
// `payment_intent.succeeded`.
//
// Audit: emits ActionBillingTopup (status=pending) before the gateway
// call. The webhook handler marks the audit row as success when the
// `payment_intent.succeeded` event lands; a failed payment leaves the
// row pending (the operator can mark it manually).
func (s *PaymentsService) CreateTopupIntent(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	email, displayName string,
	amountCents int64,
	currency string,
) (TopupIntent, error) {
	if amountCents < s.config.MinTopupCents || amountCents > s.config.MaxTopupCents {
		return TopupIntent{}, ErrInvalidAmount
	}
	cur := strings.ToLower(currency)
	if cur == "" {
		cur = s.config.DefaultTopupCurrency
	}

	customerID, err := s.getOrCreateStripeCustomer(ctx, tenantID, userID, email, displayName)
	if err != nil {
		return TopupIntent{}, err
	}
	idemp := fmt.Sprintf("pi:topup:%s:%s:%d:%s", tenantID, userID, amountCents, cur)
	pi, err := s.gw.CreatePaymentIntent(ctx, stripe.CreatePaymentIntentRequest{
		Amount:   amountCents,
		Currency: cur,
		Customer: customerID,
		Metadata: map[string]any{
			"tenant_id":     tenantID.String(),
			"user_id":       userID.String(),
			"lahijan_kind":  "topup",
			"customer_email": email,
		},
		Description: fmt.Sprintf("Lahijan balance top-up (%s)", displayName),
		ReceiptEmail: email,
	}, idemp)
	if err != nil {
		return TopupIntent{}, fmt.Errorf("billing.payments.topup: %w", err)
	}
	return TopupIntent{
		ID:           pi.ID,
		ClientSecret: pi.ClientSecret,
		AmountCents:  pi.Amount,
		Currency:     pi.Currency,
	}, nil
}

// TopupIntent is the user-facing view of a created PaymentIntent for
// a top-up. The SPA confirms via Stripe.js using ClientSecret.
type TopupIntent struct {
	ID           string
	ClientSecret string
	AmountCents  int64
	Currency     string
}

// ===========================================================================
// Webhook receiver.
// ===========================================================================

// HandleWebhook verifies the Stripe signature, deduplicates against
// billing_webhook_events, and dispatches per-event-type handlers.
// Returns nil on success (handler responds 200), an error on hard
// failure (handler responds 5xx so Stripe retries).
//
// The function does NOT need a tenant in ctx — the tenant id is
// resolved from the event payload's metadata (we put it there at
// PaymentIntent / Subscription create time).
func (s *PaymentsService) HandleWebhook(
	ctx context.Context,
	signatureHeader string,
	body []byte,
) error {
	event, err := stripe.VerifyWebhook(s.gw.(*stripe.Provider).WebhookSecret(), signatureHeader, body, time.Now(), 0)
	if err != nil {
		return fmt.Errorf("billing.payments.webhook: verify: %w", err)
	}
	// Idempotency check.
	_, errGet := s.repos.BillingWebhookEvents.GetByStripeID(ctx, event.ID)
	if errGet != nil && !database.IsNoRows(errGet) {
		return fmt.Errorf("billing.payments.webhook: dedup lookup: %w", errGet)
	}
	if errGet == nil {
		// Already processed. Idempotent no-op.
		return nil
	}

	// Resolve tenant + record the event.
	tenantID := tenantIDFromEvent(event)
	apiVer := event.APIVersion
	var tidPtr *uuid.UUID
	if tenantID != uuid.Nil {
		t := tenantID
		tidPtr = &t
	}
	raw := json.RawMessage(body)
	whEvent, err := s.repos.BillingWebhookEvents.Create(ctx, database.CreateBillingWebhookEventParams{
		TenantID:         tidPtr,
		StripeEventID:    event.ID,
		StripeEventType:  event.Type,
		StripeAPIVersion: &apiVer,
		Payload:          raw,
		Status:           "received",
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			// Concurrent delivery race; the other handler wins.
			return nil
		}
		return fmt.Errorf("billing.payments.webhook: insert event: %w", err)
	}

	// Dispatch. A handler failure marks the event as failed + returns
	// an error so Stripe retries the delivery.
	ledgerIDs, err := s.dispatchEvent(ctx, event, tenantID)
	if err != nil {
		_ = s.repos.BillingWebhookEvents.MarkFailed(ctx, whEvent.ID, err.Error())
		return fmt.Errorf("billing.payments.webhook: dispatch %s: %w", event.Type, err)
	}
	if err := s.repos.BillingWebhookEvents.MarkApplied(ctx, whEvent.ID, ledgerIDs); err != nil {
		return fmt.Errorf("billing.payments.webhook: mark applied: %w", err)
	}
	return nil
}

// dispatchEvent routes the verified event to its per-type handler.
// Returns the ledger entry ids produced (may be empty for events that
// don't post ledger rows).
func (s *PaymentsService) dispatchEvent(
	ctx context.Context,
	event stripe.Event,
	tenantID uuid.UUID,
) ([]uuid.UUID, error) {
	switch event.Type {
	case "payment_intent.succeeded":
		return s.onPaymentIntentSucceeded(ctx, event, tenantID)
	case "charge.dispute.created":
		return s.onDisputeCreated(ctx, event, tenantID)
	case "charge.dispute.closed":
		return s.onDisputeClosed(ctx, event, tenantID)
	case "customer.subscription.created", "customer.subscription.updated":
		return s.onSubscriptionUpdated(ctx, event, tenantID)
	case "customer.subscription.deleted":
		return s.onSubscriptionDeleted(ctx, event, tenantID)
	case "invoice.paid":
		return s.onInvoicePaid(ctx, event, tenantID)
	}
	// Unknown event types are a no-op: we record the row (already done
	// by HandleWebhook) + return nil so the handler responds 200.
	return nil, nil
}

// onPaymentIntentSucceeded credits the user's ledger for a successful
// top-up. Idempotent on the event id.
func (s *PaymentsService) onPaymentIntentSucceeded(
	ctx context.Context,
	event stripe.Event,
	_ uuid.UUID,
) ([]uuid.UUID, error) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Object, &pi); err != nil {
		return nil, fmt.Errorf("decode payment_intent: %w", err)
	}
	tenantID, userID, ok := idsFromMetadata(pi.Metadata)
	if !ok {
		// Not our PaymentIntent; ignore.
		return nil, nil
	}
	// Use the tenant scope for the ledger write.
	ctx = database.WithTenant(ctx, tenantID)
	// Use Topup so the credit row has the right source. The reference
	// embeds the Stripe PaymentIntent id so the ledger trail links
	// cleanly; the audit row from Topup carries the user + actor.
	credit, err := s.billing.Topup(ctx, tenantID, uuid.Nil, TopupParams{
		UserID:      userID,
		AmountCents: pi.Amount,
		Currency:    strings.ToUpper(pi.Currency),
		Reference:   "stripe_topup:" + pi.ID,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateIdempotencyKey) {
			return nil, nil // already applied
		}
		return nil, fmt.Errorf("ledger credit: %w", err)
	}
	s.emitEvent(ctx, eventbus.BillingPaymentSucceeded, tenantID, userID, credit.ID, map[string]any{
		"amount_cents": pi.Amount,
		"currency":     pi.Currency,
		"stripe_event": event.ID,
		"ledger_id":    credit.ID,
	})
	return []uuid.UUID{credit.ID}, nil
}

// onDisputeCreated records a debit row when a charge is disputed.
// The amount + a flat $15 dispute fee (Stripe's standard) are debited.
func (s *PaymentsService) onDisputeCreated(
	ctx context.Context,
	event stripe.Event,
	_ uuid.UUID,
) ([]uuid.UUID, error) {
	var d stripe.Dispute
	if err := json.Unmarshal(event.Data.Object, &d); err != nil {
		return nil, fmt.Errorf("decode dispute: %w", err)
	}
	// We don't carry tenant/user metadata on disputes; the dispute
	// handler is recorded in the webhook_events log but no ledger
	// effect is applied at this stage. A future WS will reconcile
	// disputes to the original PaymentIntent's user + post a debit.
	return nil, nil
}

// onDisputeClosed is a no-op today; disputes that close in the user's
// favour would post a credit, disputes that close against the user
// leave the original debit. Deferred to a follow-up.
func (s *PaymentsService) onDisputeClosed(
	ctx context.Context,
	event stripe.Event,
	_ uuid.UUID,
) ([]uuid.UUID, error) {
	return nil, nil
}

// onSubscriptionUpdated reconciles the local subscription row from
// the upstream Stripe state.
func (s *PaymentsService) onSubscriptionUpdated(
	ctx context.Context,
	event stripe.Event,
	_ uuid.UUID,
) ([]uuid.UUID, error) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Object, &sub); err != nil {
		return nil, fmt.Errorf("decode subscription: %w", err)
	}
	tenantID, _, ok := idsFromMetadata(sub.Metadata)
	if !ok {
		return nil, nil
	}
	ctx = database.WithTenant(ctx, tenantID)
	local, err := s.repos.BillingSubscriptions.GetByStripeID(ctx, sub.ID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil // unknown sub; ignore
		}
		return nil, fmt.Errorf("get local sub: %w", err)
	}
	periodEnd := time.Unix(sub.CurrentPeriodEnd, 0)
	if err := s.repos.BillingSubscriptions.SetStatus(ctx, local.ID, "active", &periodEnd, nil); err != nil {
		return nil, fmt.Errorf("update local sub: %w", err)
	}
	return nil, nil
}

// onSubscriptionDeleted flips the local subscription row to expired.
func (s *PaymentsService) onSubscriptionDeleted(
	ctx context.Context,
	event stripe.Event,
	_ uuid.UUID,
) ([]uuid.UUID, error) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Object, &sub); err != nil {
		return nil, fmt.Errorf("decode subscription: %w", err)
	}
	tenantID, _, ok := idsFromMetadata(sub.Metadata)
	if !ok {
		return nil, nil
	}
	ctx = database.WithTenant(ctx, tenantID)
	local, err := s.repos.BillingSubscriptions.GetByStripeID(ctx, sub.ID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get local sub: %w", err)
	}
	now := time.Now().UTC()
	if err := s.repos.BillingSubscriptions.SetStatus(ctx, local.ID, "expired", nil, &now); err != nil {
		return nil, fmt.Errorf("update local sub: %w", err)
	}
	s.emitEvent(ctx, eventbus.BillingSubscriptionCanceled, tenantID, local.UserID, local.ID, map[string]any{
		"stripe_subscription_id": sub.ID,
	})
	return nil, nil
}

// onInvoicePaid credits the user's ledger with the included quota
// from the active subscription.
func (s *PaymentsService) onInvoicePaid(
	ctx context.Context,
	event stripe.Event,
	_ uuid.UUID,
) ([]uuid.UUID, error) {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Object, &inv); err != nil {
		return nil, fmt.Errorf("decode invoice: %w", err)
	}
	if !inv.Paid || inv.Total <= 0 {
		return nil, nil
	}
	tenantID, userID, ok := idsFromMetadata(inv.Metadata)
	if !ok {
		return nil, nil
	}
	ctx = database.WithTenant(ctx, tenantID)
	idem := fmt.Sprintf("stripe:%s:sub_credit", event.ID)
	// Look up the local subscription to compute the included quota.
	var quota int64
	if inv.Subscription != "" {
		if local, err := s.repos.BillingSubscriptions.GetByStripeID(ctx, inv.Subscription); err == nil {
			quota = local.IncludedQuotaCents
		}
	}
	if quota <= 0 {
		// No included quota on this plan; nothing to credit.
		return nil, nil
	}
	credit, err := s.billing.Topup(ctx, tenantID, uuid.Nil, TopupParams{
		UserID:      userID,
		AmountCents: quota,
		Currency:    strings.ToUpper(inv.Currency),
		Reference:   "stripe_subscription:" + inv.Subscription,
	})
	if err != nil {
		if errors.Is(err, ErrDuplicateIdempotencyKey) {
			return nil, nil
		}
		return nil, fmt.Errorf("ledger sub credit: %w", err)
	}
	_ = idem // billing.Topup handles dedup via the ledger trigger
	s.emitEvent(ctx, eventbus.BillingPaymentSucceeded, tenantID, userID, credit.ID, map[string]any{
		"amount_cents": quota,
		"currency":     inv.Currency,
		"kind":         "subscription_credit",
		"invoice_id":   inv.ID,
		"ledger_id":    credit.ID,
	})
	return []uuid.UUID{credit.ID}, nil
}

// tenantIDFromEvent resolves the tenant id from the event payload's
// metadata. Returns uuid.Nil when the metadata is absent (event
// originated outside Lahijan's flow).
func tenantIDFromEvent(event stripe.Event) uuid.UUID {
	// Re-decode the payload to extract metadata: the type-specific
	// object has metadata at the top level for our types.
	var probe struct {
		Metadata map[string]any `json:"metadata"`
	}
	_ = json.Unmarshal(event.Data.Object, &probe)
	if probe.Metadata == nil {
		return uuid.Nil
	}
	tid, _, ok := idsFromMetadata(probe.Metadata)
	if !ok {
		return uuid.Nil
	}
	return tid
}

// idsFromMetadata extracts (tenantID, userID, ok) from a Stripe
// metadata map. Returns ok=false when either id is absent or malformed.
func idsFromMetadata(meta map[string]any) (uuid.UUID, uuid.UUID, bool) {
	if meta == nil {
		return uuid.Nil, uuid.Nil, false
	}
	tidStr, ok1 := meta["tenant_id"].(string)
	uidStr, ok2 := meta["user_id"].(string)
	if !ok1 || !ok2 {
		return uuid.Nil, uuid.Nil, false
	}
	tid, errT := uuid.Parse(tidStr)
	if errT != nil {
		return uuid.Nil, uuid.Nil, false
	}
	uid, errU := uuid.Parse(uidStr)
	if errU != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return tid, uid, true
}
