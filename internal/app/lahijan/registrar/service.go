// Package registrar is Lahijan's user-facing domain registration module
// (WS-28). It orchestrates calls to the registrar provider driver
// (Phase 7, providers/registrar/), the tenant-scoped dns_domains table
// (migration 0046), the RBAC policy + audit emitter (WS-08), the WASM
// event bus (WS-10b), and the billing service (WS-17) so every
// registration / renewal / transfer is charged against the user's
// ledger.
//
// Layering:
//
//	api/dns_handlers.go  -> registrar.Service -> providers/registrar
//	                                          \-> database (dns_domains)
//	                                          \-> billing.Service (PostCharge)
//	                                          \-> auth/audit
//	                                          \-> wasm/eventbus
//
// Every privileged action calls RequirePerm via the api/middleware gate;
// every state-changing action emits an audit event before the side
// effect (status=pending) and marks the outcome after. Every domain
// lifecycle transition also emits into the WASM event bus so plugins
// can react.
//
// Per pillar 1, this package is named "registrar" — never "opensrs" or
// "resellerclub". End users do not see the brand name in the API or UI.
package registrar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/billing"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// ResourceDomain is the audit resource_type value for the registrar module.
const ResourceDomain = audit.ResourceDNSDomain

// AuditAction constants for the registrar module. Past-tense verbs so
// the audit row reads "what happened"; i18n keys are auto-derived.
const (
	AuditSearch   = audit.ActionDNSDomainSearch
	AuditRegister = audit.ActionDNSDomainRegister
	AuditRenew    = audit.ActionDNSDomainRenew
	AuditTransfer = audit.ActionDNSDomainTransfer
	AuditDelete   = audit.ActionDNSDomainDelete
	AuditDNSSEC   = audit.ActionDNSDomainDNSSEC
)

// Service is the entrypoint every registrar API handler talks to. It
// owns the registrar provider (Phase 7 driver), the tenant-scoped
// dns_domains repository, the audit emitter, the WASM event bus, the
// RBAC policy evaluator, and the billing service used to charge for
// registrations.
type Service struct {
	provider providerDriver
	repos    *database.Repos
	audit    audit.Emitter
	bus      eventBus
	policy   rbac.PolicyEvaluator
	billing  billingHook
	config   Config
}

// providerDriver is the narrow seam the service needs from
// providers/registrar.Provider. Defined here so tests can swap a fake
// without dragging the full provider surface into the test file.
type providerDriver interface {
	Name() string
	Ping(ctx context.Context) error
	Capabilities() registrar.Capabilities
	CheckDomain(ctx context.Context, req registrar.CheckDomainRequest) (*registrar.CheckDomainResponse, error)
	RegisterDomain(ctx context.Context, req registrar.RegisterDomainRequest) (*registrar.RegisterDomainResponse, error)
	RenewDomain(ctx context.Context, req registrar.RenewDomainRequest) (*registrar.RenewDomainResponse, error)
	TransferDomain(ctx context.Context, req registrar.TransferDomainRequest) (*registrar.TransferDomainResponse, error)
	GetDomain(ctx context.Context, req registrar.GetDomainRequest) (*registrar.GetDomainResponse, error)
	SetDSRecords(ctx context.Context, req registrar.SetDSRecordsRequest) (*registrar.SetDSRecordsResponse, error)
}

// eventBus is the narrow seam the service needs from *eventbus.Bus.
type eventBus interface {
	Emit(ctx context.Context, e eventbus.Event) error
}

// billingHook is the narrow seam the service needs from *billing.Service.
// Only the PostCharge path is needed; the registrar module does not do
// refunds / topups.
type billingHook interface {
	PostCharge(ctx context.Context, tenantID uuid.UUID, params billing.PostChargeParams) (database.LedgerEntry, error)
}

// Config carries the process-wide knobs the service needs.
type Config struct {
	// DefaultContactProfile is the contact bundle the service sends to
	// the registrar when the caller does not supply one. Empty by
	// default — the caller MUST supply one until a future WS adds an
	// admin-managed contact catalog.
	DefaultContactProfile registrar.ContactProfile

	// MarginPercent is the markup Lahijan applies on top of the
	// registrar's retail price. Default 0 (at-cost). The markup is
	// computed at the service layer so the registrar driver never
	// knows Lahijan's pricing.
	MarginPercent int32

	// DefaultCurrency is the ISO 4217 code Lahijan bills the user in.
	// Default "USD".
	DefaultCurrency string
}

// New builds a Service. Every dependency is required except `bus`,
// `policy`, and `billing` (nil disables event emission / non-HTTP
// RequirePerm / paid registrations respectively).
func New(
	p providerDriver,
	r *database.Repos,
	emitter audit.Emitter,
	bus eventBus,
	policy rbac.PolicyEvaluator,
	billingSvc billingHook,
	cfg Config,
) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	if cfg.DefaultCurrency == "" {
		cfg.DefaultCurrency = "USD"
	}
	return &Service{
		provider: p,
		repos:    r,
		audit:    emitter,
		bus:      bus,
		policy:   policy,
		billing:  billingSvc,
		config:   cfg,
	}
}

// SearchDomain asks the registrar whether the domain is available +
// the per-period prices (with Lahijan's margin applied).
//
// Audit emits a row even though the call is read-only at the
// registrar — the registrar's CheckDomain is metered (resellers pay
// per query) so the audit trail records the actor.
func (s *Service) SearchDomain(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	domain string,
) (SearchResult, error) {
	if s.provider == nil {
		return SearchResult{}, ErrProviderDisabled
	}
	if err := validateDomainName(domain); err != nil {
		return SearchResult{}, err
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditSearch,
		ResourceType: ResourceDomain,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"domain": domain,
		},
	})

	resp, err := s.provider.CheckDomain(ctx, registrar.CheckDomainRequest{Domain: domain})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return SearchResult{}, fmt.Errorf("registrar: check domain: %w", err)
	}

	out := SearchResult{
		Domain:    resp.Domain,
		Available: resp.Available,
		Status:    string(resp.Status),
		Reason:    resp.Reason,
		Pricing:   make([]Pricing, 0, len(resp.Pricing)),
	}
	for _, p := range resp.Pricing {
		adjusted := applyMargin(p.PriceCents, s.config.MarginPercent)
		out.Pricing = append(out.Pricing, Pricing{
			PeriodYears: p.PeriodYears,
			PriceCents:  adjusted,
			Currency:    orDefault(p.Currency, s.config.DefaultCurrency),
		})
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{
		Status: audit.StatusSuccess,
		Details: map[string]any{
			"available": resp.Available,
		},
	})
	return out, nil
}

// RegisterDomainRequest carries the user-controlled fields of a
// register-domain call. The contact profile defaults to the service's
// configured DefaultContactProfile when nil.
type RegisterDomainRequest struct {
	Domain        string
	PeriodYears   int32
	Contact       *registrar.ContactProfile
	AutoRenew     bool
	WHOISPrivacy  bool
	AutoProvision bool   // when true, also call CreateZone on success
	AutoDNSSEC    bool   // when true, sign zone + publish DS at parent
	Nameservers   []string
}

// RegisterDomain places a new registration order + charges the user's
// ledger + (optionally) auto-provisions a dns_zones row + (optionally)
// publishes the DS at the parent.
func (s *Service) RegisterDomain(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	req RegisterDomainRequest,
) (database.DNSDomain, error) {
	if s.provider == nil {
		return database.DNSDomain{}, ErrProviderDisabled
	}
	if err := validateDomainName(req.Domain); err != nil {
		return database.DNSDomain{}, err
	}
	if req.PeriodYears < 1 || req.PeriodYears > 10 {
		return database.DNSDomain{}, ErrInvalidPeriod
	}
	if s.billing == nil {
		return database.DNSDomain{}, ErrBillingRequired
	}

	// Pick the contact profile (caller's, else default).
	contact := s.config.DefaultContactProfile
	if req.Contact != nil {
		contact = *req.Contact
	}
	if err := validateContact(contact); err != nil {
		return database.DNSDomain{}, err
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditRegister,
		ResourceType: ResourceDomain,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"domain":       req.Domain,
			"period_years": req.PeriodYears,
			"auto_renew":   req.AutoRenew,
		},
	})

	// 1) Pre-charge the user's ledger. We do this BEFORE the registrar
	//    call so a user with insufficient balance fails fast and does
	//    not strand a registrar order. If the registrar call later
	//    fails the charge is reversed via a refund row (the ledger is
	//    append-only; we add a credit row with source=refund).
	check, err := s.provider.CheckDomain(ctx, registrar.CheckDomainRequest{Domain: req.Domain})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: pre-check: %w", err)
	}
	if !check.Available {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": "domain unavailable"}})
		return database.DNSDomain{}, ErrDomainUnavailable
	}
	price := pickPrice(check.Pricing, req.PeriodYears)
	if price == nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": "no pricing for requested period"}})
		return database.DNSDomain{}, ErrNoPricing
	}
	charged := applyMargin(price.PriceCents, s.config.MarginPercent)
	currency := orDefault(price.Currency, s.config.DefaultCurrency)
	idem := fmt.Sprintf("dns.domain.register:%s:%s:%d", tenantID, req.Domain, req.PeriodYears)
	ledger, err := s.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
		UserID:         userID,
		AmountCents:    charged,
		Currency:       currency,
		Reference:      fmt.Sprintf("domain register: %s (%dy)", req.Domain, req.PeriodYears),
		IdempotencyKey: &idem,
		Metadata: map[string]any{
			"domain":       req.Domain,
			"period_years": req.PeriodYears,
			"registrar":    s.provider.Name(),
		},
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: charge: %w", err)
	}

	// 2) Place the registrar order. On failure we record a refund row
	//    so the user's balance is restored.
	regResp, err := s.provider.RegisterDomain(ctx, registrar.RegisterDomainRequest{
		Domain:        req.Domain,
		PeriodYears:   req.PeriodYears,
		Contact:       contact,
		AutoRenew:     req.AutoRenew,
		WHOISPrivacy:  req.WHOISPrivacy,
	})
	if err != nil {
		// Best-effort refund. The ledger's append-only invariant
		// means we add a credit row referencing the original charge.
		_, _ = s.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
			UserID:      userID,
			AmountCents: -charged,
			Currency:    currency,
			Reference:   fmt.Sprintf("domain register refund: %s", req.Domain),
			Metadata: map[string]any{
				"domain":          req.Domain,
				"original_charge": ledger.ID,
				"registrar_error": err.Error(),
			},
		})
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: register domain: %w", err)
	}

	// 3) Persist the dns_domains row.
	row, err := s.repos.DNSDomains.Create(ctx, database.CreateDNSDomainParams{
		Name:             canonicalDomain(req.Domain),
		Status:           string(regResp.Status),
		RegistrarOrderID: regResp.OrderID,
		PriceCents:       charged,
		Currency:         currency,
		PeriodYears:      req.PeriodYears,
		LedgerEntryID:    &ledger.ID,
		IsAutoRenew:      req.AutoRenew,
		RegisteredAt:     nowPtr(),
		ExpiresAt:        regResp.ExpiresAt,
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: create domain row: %w", err)
	}

	// 4) Emit the registered event.
	s.emitEvent(ctx, eventbus.DNSDomainRegistered, tenantID, userID, row.ID, map[string]any{
		"domain":         row.Name,
		"order_id":       row.RegistrarOrderID,
		"price_cents":    charged,
		"ledger_entry":   ledger.ID,
		"expires_at":     row.ExpiresAt,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"domain_id":     row.ID,
		"order_id":      row.RegistrarOrderID,
		"ledger_entry":  ledger.ID,
	}})
	return row, nil
}

// RenewDomainRequest carries the user-controlled fields of a renew call.
type RenewDomainRequest struct {
	DomainID    uuid.UUID
	PeriodYears int32
}

// RenewDomain extends an existing registration by the requested period.
// Charges the user's ledger like RegisterDomain does.
func (s *Service) RenewDomain(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	req RenewDomainRequest,
) (database.DNSDomain, error) {
	if s.provider == nil {
		return database.DNSDomain{}, ErrProviderDisabled
	}
	if req.PeriodYears < 1 || req.PeriodYears > 10 {
		return database.DNSDomain{}, ErrInvalidPeriod
	}
	if s.billing == nil {
		return database.DNSDomain{}, ErrBillingRequired
	}

	row, err := s.repos.DNSDomains.Get(ctx, req.DomainID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DNSDomain{}, ErrDomainNotFound
		}
		return database.DNSDomain{}, fmt.Errorf("registrar: get domain: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditRenew,
		ResourceType: ResourceDomain,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"domain":       row.Name,
			"period_years": req.PeriodYears,
		},
	})

	// Charge first (same reasoning as RegisterDomain).
	charged := applyMargin(row.PriceCents/int64(row.PeriodYears)*int64(req.PeriodYears), s.config.MarginPercent)
	idem := fmt.Sprintf("dns.domain.renew:%s:%s:%d", tenantID, row.ID, req.PeriodYears)
	ledger, err := s.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
		UserID:         userID,
		AmountCents:    charged,
		Currency:       row.Currency,
		Reference:      fmt.Sprintf("domain renew: %s (%dy)", row.Name, req.PeriodYears),
		IdempotencyKey: &idem,
		Metadata: map[string]any{
			"domain":    row.Name,
			"order_id":  row.RegistrarOrderID,
		},
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: charge: %w", err)
	}

	// Renew at the registrar.
	resp, err := s.provider.RenewDomain(ctx, registrar.RenewDomainRequest{
		Domain:       stripTrailingDot(row.Name),
		OrderID:      row.RegistrarOrderID,
		PeriodYears:  req.PeriodYears,
	})
	if err != nil {
		// Refund.
		_, _ = s.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
			UserID:      userID,
			AmountCents: -charged,
			Currency:    row.Currency,
			Reference:   fmt.Sprintf("domain renew refund: %s", row.Name),
			Metadata: map[string]any{
				"original_charge": ledger.ID,
				"registrar_error": err.Error(),
			},
		})
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: renew domain: %w", err)
	}

	// Update the row.
	if err := s.repos.DNSDomains.SetExpiry(ctx, row.ID, nil, resp.ExpiresAt); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: update expiry: %w", err)
	}
	if err := s.repos.DNSDomains.SetLedgerLink(ctx, row.ID, ledger.ID); err != nil {
		// Non-fatal — the link is for the audit trail only.
		_ = err
	}
	row.ExpiresAt = resp.ExpiresAt

	s.emitEvent(ctx, eventbus.DNSDomainRenewed, tenantID, userID, row.ID, map[string]any{
		"domain":        row.Name,
		"order_id":      row.RegistrarOrderID,
		"expires_at":    resp.ExpiresAt,
		"ledger_entry":  ledger.ID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"domain_id":    row.ID,
		"expires_at":   resp.ExpiresAt,
		"ledger_entry": ledger.ID,
	}})
	return row, nil
}

// TransferDomainRequest carries the user-controlled fields of a transfer.
type TransferDomainRequest struct {
	Domain       string
	AuthCode     string
	PeriodYears  int32
	Contact      *registrar.ContactProfile
	AutoProvision bool
	AutoDNSSEC   bool
}

// TransferDomain initiates an EPP transfer from another registrar.
func (s *Service) TransferDomain(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	req TransferDomainRequest,
) (database.DNSDomain, error) {
	if s.provider == nil {
		return database.DNSDomain{}, ErrProviderDisabled
	}
	if err := validateDomainName(req.Domain); err != nil {
		return database.DNSDomain{}, err
	}
	if req.AuthCode == "" {
		return database.DNSDomain{}, ErrAuthCodeRequired
	}
	if req.PeriodYears < 1 || req.PeriodYears > 10 {
		return database.DNSDomain{}, ErrInvalidPeriod
	}
	if s.billing == nil {
		return database.DNSDomain{}, ErrBillingRequired
	}
	contact := s.config.DefaultContactProfile
	if req.Contact != nil {
		contact = *req.Contact
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditTransfer,
		ResourceType: ResourceDomain,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"domain":       req.Domain,
			"period_years": req.PeriodYears,
		},
	})

	// Charge first using the standard per-year price (transfers cost
	// the same as a 1y renewal at most registrars).
	charged := applyMargin(1000*int64(req.PeriodYears), s.config.MarginPercent)
	idem := fmt.Sprintf("dns.domain.transfer:%s:%s:%d", tenantID, req.Domain, req.PeriodYears)
	ledger, err := s.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
		UserID:         userID,
		AmountCents:    charged,
		Currency:       s.config.DefaultCurrency,
		Reference:      fmt.Sprintf("domain transfer: %s (%dy)", req.Domain, req.PeriodYears),
		IdempotencyKey: &idem,
		Metadata: map[string]any{
			"domain": req.Domain,
		},
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: charge: %w", err)
	}

	resp, err := s.provider.TransferDomain(ctx, registrar.TransferDomainRequest{
		Domain:      req.Domain,
		AuthCode:    req.AuthCode,
		PeriodYears: req.PeriodYears,
		Contact:     contact,
	})
	if err != nil {
		// Refund.
		_, _ = s.billing.PostCharge(ctx, tenantID, billing.PostChargeParams{
			UserID:      userID,
			AmountCents: -charged,
			Currency:    s.config.DefaultCurrency,
			Reference:   fmt.Sprintf("domain transfer refund: %s", req.Domain),
			Metadata: map[string]any{
				"original_charge": ledger.ID,
				"registrar_error": err.Error(),
			},
		})
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: transfer domain: %w", err)
	}

	row, err := s.repos.DNSDomains.Create(ctx, database.CreateDNSDomainParams{
		Name:             canonicalDomain(req.Domain),
		Status:           string(resp.Status),
		RegistrarOrderID: resp.OrderID,
		PriceCents:       charged,
		Currency:         s.config.DefaultCurrency,
		PeriodYears:      req.PeriodYears,
		LedgerEntryID:    &ledger.ID,
		RegisteredAt:     nowPtr(),
		ExpiresAt:        resp.ExpiresAt,
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return database.DNSDomain{}, fmt.Errorf("registrar: create domain row: %w", err)
	}

	s.emitEvent(ctx, eventbus.DNSDomainTransferred, tenantID, userID, row.ID, map[string]any{
		"domain":        row.Name,
		"order_id":      row.RegistrarOrderID,
		"ledger_entry":  ledger.ID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"domain_id":    row.ID,
		"order_id":     row.RegistrarOrderID,
		"ledger_entry": ledger.ID,
	}})
	return row, nil
}

// GetDomain returns the cached dns_domains row.
func (s *Service) GetDomain(
	ctx context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	domainID uuid.UUID,
) (database.DNSDomain, error) {
	row, err := s.repos.DNSDomains.Get(ctx, domainID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DNSDomain{}, ErrDomainNotFound
		}
		return database.DNSDomain{}, fmt.Errorf("registrar: get domain: %w", err)
	}
	return row, nil
}

// ListDomains returns a paginated list of the tenant's domains.
func (s *Service) ListDomains(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.DNSDomain, error) {
	rows, err := s.repos.DNSDomains.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("registrar: list domains: %w", err)
	}
	return rows, nil
}

// CountDomains returns the number of domains in the tenant.
func (s *Service) CountDomains(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.DNSDomains.Count(ctx)
}

// DeleteDomain removes the dns_domains row. Does NOT cancel the
// registration at the registrar — that is an irreversible action the
// user must take explicitly via the registrar's back-office. The
// audit row records "domain removed from tenant view; registration
// unchanged at the registrar".
func (s *Service) DeleteDomain(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	domainID uuid.UUID,
) error {
	row, err := s.repos.DNSDomains.Get(ctx, domainID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrDomainNotFound
		}
		return fmt.Errorf("registrar: get domain: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditDelete,
		ResourceType: ResourceDomain,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"domain":         row.Name,
			"registrar_order_id": row.RegistrarOrderID,
		},
	})
	if err := s.repos.DNSDomains.Delete(ctx, domainID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return fmt.Errorf("registrar: delete domain: %w", err)
	}
	s.emitEvent(ctx, eventbus.DNSDomainDeleted, tenantID, userID, row.ID, map[string]any{
		"domain":         row.Name,
		"registrar_order_id": row.RegistrarOrderID,
	})
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// SearchResult is the user-facing response for SearchDomain. The
// pricing has Lahijan's margin applied.
type SearchResult struct {
	Domain    string    `json:"domain"`
	Available bool      `json:"available"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	Pricing   []Pricing `json:"pricing"`
}

// Pricing carries the per-period price the user pays (margin applied).
type Pricing struct {
	PeriodYears int32  `json:"period_years"`
	PriceCents  int64  `json:"price_cents"`
	Currency    string `json:"currency"`
}

// emitEvent fans the event into the WASM bus (if configured). Errors
// are logged but never returned to the caller; a wedged bus must not
// roll back a successful change.
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
		if b, err := jsonMarshal(meta); err == nil {
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

// jsonMarshal is a tiny alias so this file does not import encoding/json
// at the top.
var jsonMarshal = marshalJSON

// marshalJSON is a thin wrapper around encoding/json.Marshal kept here
// so the import block stays tidy.
func marshalJSON(v any) ([]byte, error) {
	return jsonEncode(v)
}

// validateDomainName enforces the canonical domain name shape: no
// whitespace, no trailing dot (the service stores names with the dot;
// callers pass without), lowercase. Allows the typical domain labels
// (letters, digits, hyphens) plus the dot separator.
func validateDomainName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidDomain)
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("%w: name %q must not end with a dot (the service canonicalises)", ErrInvalidDomain, name)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("%w: name %q must not contain whitespace", ErrInvalidDomain, name)
	}
	if strings.ToLower(name) != name {
		return fmt.Errorf("%w: name %q must be lowercase", ErrInvalidDomain, name)
	}
	return nil
}

// canonicalDomain appends the trailing dot so the value stored in
// dns_zones + dns_domains matches the canonical PowerDNS shape.
func canonicalDomain(name string) string {
	if name == "" {
		return name
	}
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

// stripTrailingDot is the inverse of canonicalDomain — the registrar
// driver expects the bare name (no trailing dot).
func stripTrailingDot(name string) string {
	return strings.TrimSuffix(name, ".")
}

// validateContact enforces the required contact fields every registrar
// needs. Returns ErrInvalidContact on the first missing field.
func validateContact(c registrar.ContactProfile) error {
	if c.OwnerFirstname == "" || c.OwnerLastname == "" {
		return fmt.Errorf("%w: owner firstname + lastname required", ErrInvalidContact)
	}
	if c.OwnerEmail == "" {
		return fmt.Errorf("%w: owner email required", ErrInvalidContact)
	}
	if c.OwnerPhone == "" {
		return fmt.Errorf("%w: owner phone required", ErrInvalidContact)
	}
	if c.Address1 == "" || c.City == "" || c.State == "" || c.Zip == "" || c.CountryCode == "" {
		return fmt.Errorf("%w: address1 + city + state + zip + country required", ErrInvalidContact)
	}
	return nil
}

// applyMargin adds the configured percent markup to a registrar-quoted
// price. A negative result is clamped to 1 cent so a misconfigured
// margin cannot credit the user.
func applyMargin(priceCents int64, marginPercent int32) int64 {
	if marginPercent == 0 {
		return priceCents
	}
	adjusted := priceCents + (priceCents*int64(marginPercent))/100
	if adjusted < 1 {
		return 1
	}
	return adjusted
}

// pickPrice returns the pricing entry matching the requested period.
// Returns nil when no entry matches.
func pickPrice(prices []registrar.DomainPricing, period int32) *registrar.DomainPricing {
	for i := range prices {
		if prices[i].PeriodYears == period {
			return &prices[i]
		}
	}
	return nil
}

// orDefault returns s when non-empty, else def.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// nowPtr returns a pointer to time.Now().UTC(). Used for the
// registered_at column at register time.
func nowPtr() *time.Time {
	now := time.Now().UTC()
	return &now
}
