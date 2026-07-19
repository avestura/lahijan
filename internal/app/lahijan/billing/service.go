// Package billing: service.go is the entrypoint every billing API handler
// and every metering job talks to. It owns the price catalog + ledger +
// balance cache + usage stream + receipts repositories, the audit
// emitter, the WASM event bus, the enforcement driver, and the metering
// collectors. Every method takes a context carrying the tenant id (set
// by the tenant middleware) and the user id (set by the auth middleware);
// the service enforces RBAC at the api/middleware layer for HTTP requests
// and via rbac.Require for non-HTTP entry points.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// Resource type + audit action constants for the billing module.
// Past-tense verbs for actions so the audit row reads "what happened";
// i18n keys are auto-derived (dots -> underscores).
const (
	ResourceLedgerEntry = audit.ResourceLedgerEntry
	ResourcePrice       = audit.ResourcePrice
	ResourceReceipt     = audit.ResourceReceipt
	ResourceBalance     = audit.ResourceBalance

	AuditTopup        = audit.ActionBillingTopup
	AuditRefund       = audit.ActionBillingRefund
	AuditPriceUpsert  = audit.ActionBillingPriceUpsert
	AuditPriceExpire  = audit.ActionBillingPriceExpire
	AuditReceiptGen   = audit.ActionBillingReceiptGen
	AuditForceRebuild = audit.ActionBillingForceRebuild
)

// Ledger entry type + source constants. The schema CHECK + the trigger
// do not enforce these (free-form TEXT today), so the service layer is
// the source of truth.
const (
	LedgerTypeCredit = "credit"
	LedgerTypeDebit  = "debit"

	SourceTopup      = "topup"
	SourceCharge     = "charge"
	SourceRefund     = "refund"
	SourceAdjustment = "adjustment"
)

// Receipt status constants. The receipt generator flips a row from
// "pending" to "ready" once the PDF body is attached.
const (
	ReceiptStatusPending = "pending"
	ReceiptStatusReady   = "ready"
)

// DefaultCurrency is the single currency Lahijan MVP bills in (per WS-17
// Open Questions item 1). Multi-currency is Phase 7.
const DefaultCurrency = "USD"

// DefaultGracePeriod is how long a user's instances keep running after
// their balance hits zero before the enforcement job stops them. Per
// WS-17 Open Questions item 3.
const DefaultGracePeriod = 24 * time.Hour

// Service is the entrypoint for every billing operation. Construct one
// at bootstrap (program.Start) and share it across handlers + jobs.
type Service struct {
	repos    *database.Repos
	audit    audit.Emitter
	bus      eventBus
	policy   rbac.PolicyEvaluator
	enforcer Enforcer
	meter    Meter
	config   Config
}

// eventBus is the narrow seam the service needs from *eventbus.Bus.
type eventBus interface {
	Emit(ctx context.Context, e eventbus.Event) error
}

// Enforcer is the seam the balance watcher uses to stop a user's
// resources when their balance hits zero + the grace period expires. The
// concrete implementation lives in the compute module (WS-14); WS-17
// owns the interface so the dependency runs one way (compute -> billing
// for balance checks; billing -> compute for enforcement).
type Enforcer interface {
	// StopAllForUser stops every long-running resource (typically
	// compute instances) the user owns within the tenant. The billing
	// service calls this when the user's zero-balance grace period
	// expires. Errors are logged but do not roll back the enforcement
	// decision (the cache row stays at balance <= 0).
	StopAllForUser(ctx context.Context, tenantID, userID uuid.UUID, reason string) error
}

// NoopEnforcer is the zero-value enforcer: it logs nothing and stops
// nothing. Used when the compute module is disabled (dev / test) and as
// the default for tests that exercise the ledger + balance paths
// without standing up the compute service.
type NoopEnforcer struct{}

// StopAllForUser implements Enforcer by returning nil immediately.
func (NoopEnforcer) StopAllForUser(_ context.Context, _, _ uuid.UUID, _ string) error {
	return nil
}

// Meter is the seam the metering collector uses to pull current usage
// from each provider. The concrete implementation is composed of the
// Incus / PowerDNS / SeaweedFS provider pullers; WS-17 ships a minimal
// meter that records per-minute uptime for running instances + the
// cached bucket sizes (storage) so the rollup -> ledger pipeline is
// exercised end-to-end. Real per-resource meters (CPU%, RAM, requests)
// land in a follow-up.
type Meter interface {
	// Collect reads the current usage snapshot for the tenant and
	// appends one usage_events row per (user, resource) pair. The
	// idempotency key is per (tenant, user, resource, minute) so a
	// duplicate run does not double-count. Returns the number of
	// rows inserted (zero is a normal "nothing changed since last
	// minute" result).
	Collect(ctx context.Context, tenantID uuid.UUID, minute time.Time) (int, error)
}

// NoopMeter is the zero-value meter: it logs nothing and writes no
// usage_events rows. Used when the providers are disabled.
type NoopMeter struct{}

// Collect implements Meter by returning 0, nil.
func (NoopMeter) Collect(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

// Config carries the process-wide knobs the service needs.
type Config struct {
	// Currency is the ISO 4217 code Lahijan bills in. Default "USD".
	// Multi-currency is Phase 7 (per WS-17 Open Questions item 1).
	Currency string

	// GracePeriod is how long a user's instances keep running after
	// their balance hits zero before the enforcement job stops them.
	// Default 24h (per WS-17 Open Questions item 3). Zero means
	// "default 24h".
	GracePeriod time.Duration

	// LowBalanceThreshold is the balance at which the service emits a
	// billing.balance.low event so plugins + the UI can warn the user.
	// Default 0 — the service only emits the low-balance event when
	// the balance crosses zero. Set to a positive value to warn early.
	LowBalanceThreshold int64

	// ReceiptCadence is the default period a receipt covers. Monthly
	// (per WS-17 Open Questions item 2). Daily / weekly are also
	// supported; users can override per-call via the receipt API.
	ReceiptCadence ReceiptCadence

	// DefaultEffectiveFrom is the timestamp the price catalog uses
	// when the caller does not pass one. Typically time.Now().UTC();
	// tests override.
	DefaultEffectiveFrom time.Time
}

// ReceiptCadence is the period a receipt covers. Monthly by default.
type ReceiptCadence string

const (
	// ReceiptCadenceMonthly covers one calendar month.
	ReceiptCadenceMonthly ReceiptCadence = "monthly"
	// ReceiptCadenceWeekly covers one ISO week (Mon-Sun).
	ReceiptCadenceWeekly ReceiptCadence = "weekly"
	// ReceiptCadenceDaily covers one calendar day.
	ReceiptCadenceDaily ReceiptCadence = "daily"
)

// New builds a Service. Every dependency is required except `bus`,
// `policy`, `enforcer`, and `meter` (nil disables event emission /
// non-HTTP RequirePerm / enforcement / metering respectively).
func New(
	r *database.Repos,
	emitter audit.Emitter,
	bus eventBus,
	policy rbac.PolicyEvaluator,
	enforcer Enforcer,
	meter Meter,
	cfg Config,
) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	if enforcer == nil {
		enforcer = NoopEnforcer{}
	}
	if meter == nil {
		meter = NoopMeter{}
	}
	if cfg.Currency == "" {
		cfg.Currency = DefaultCurrency
	}
	if cfg.GracePeriod <= 0 {
		cfg.GracePeriod = DefaultGracePeriod
	}
	if cfg.ReceiptCadence == "" {
		cfg.ReceiptCadence = ReceiptCadenceMonthly
	}
	if cfg.DefaultEffectiveFrom.IsZero() {
		cfg.DefaultEffectiveFrom = time.Now().UTC()
	}
	return &Service{
		repos:    r,
		audit:    emitter,
		bus:      bus,
		policy:   policy,
		enforcer: enforcer,
		meter:    meter,
		config:   cfg,
	}
}

// emitEvent is a thin wrapper that drops the event on the floor when no
// bus is configured (dev / unit tests). Mirrors compute / dns / storage
// services' emitEvent.
func (s *Service) emitEvent(
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

// validateAmount rejects non-positive amounts at the service boundary.
// All callers (topup, refund, charge) require a strictly positive amount
// in integer centimals.
func validateAmount(amountCents int64) error {
	if amountCents <= 0 {
		return ErrInvalidAmount
	}
	return nil
}

// validateResource rejects empty / whitespace-only resource_type / unit
// at the service boundary.
func validateResource(resourceType, unit string) error {
	if resourceType == "" || unit == "" {
		return ErrInvalidResource
	}
	return nil
}

// validateCurrency enforces a 3-letter ISO 4217 shape. Multi-currency is
// Phase 7 but the shape is checked from day 1 so the migration is additive.
func validateCurrency(code string) error {
	if len(code) != 3 {
		return ErrInvalidCurrency
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return ErrInvalidCurrency
		}
	}
	return nil
}

// currencyOrDefault returns code when non-empty, else DefaultCurrency.
func (s *Service) currencyOrDefault(code string) string {
	if code == "" {
		return s.config.Currency
	}
	return code
}

// ErrNoTenantInContext is re-exported so callers in this package get a
// compile-time reminder that the tenant scope is required. Wraps the
// database's sentinel so callers can errors.Is against either.
var ErrNoTenantInContext = database.ErrNoTenantInContext

// auditEmit is a small helper that wraps the emit call so the call site
// reads "audit before side effect" without the boilerplate. The
// returned uuid is the audit row id (uuid.Nil on error); the caller
// pairs it with auditMarkOutcome in a defer.
func (s *Service) auditEmit(
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
		// Emit failed; the audit row does not exist so MarkOutcome
		// has nothing to update. Log + continue so the privileged
		// action still runs (audit must never block the action).
		_ = errors.Join(err, fmt.Errorf("billing: audit emit %s", action))
		return uuid.Nil
	}
	return id
}

// auditMarkOutcome is the matching helper for auditEmit. nil auditID
// (the emit failed) is a no-op.
func (s *Service) auditMarkOutcome(
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
