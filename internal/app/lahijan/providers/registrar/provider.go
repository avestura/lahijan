// Package registrar: provider.go declares the Provider interface every
// registrar driver implements. Mirrors the providers.Provider shape
// (WS-11) plus the registrar-specific surface (search / register /
// renew / transfer / DSRecords / GetDomain).
//
// Per ADR-0035 there is exactly one in-tree implementation today
// (OpenSRS); the interface leaves room for additional registrars
// (ResellerClub, Namecheap, ...). The service layer
// (internal/app/lahijan/registrar/) talks to a single Provider instance
// at runtime — swapping registrars is a deployment-time decision, not
// a runtime one.
package registrar

import "context"

// Provider is the seam every registrar driver implements. Each method
// takes a context (for cancellation + tracing) and a typed request; the
// response is always a value type (not a pointer) so the caller can
// copy freely.
//
// Methods are safe for concurrent use. Per-call state lives only on
// the call stack.
type Provider interface {
	// Name returns the internal registrar identifier (e.g. "opensrs").
	// NEVER surfaces to end users (per pillar 1 they see "domain
	// registration" in the UI).
	Name() string

	// Ping probes the registrar's health endpoint. A nil return means
	// the registrar is reachable + the API key is valid. Used by
	// program.Start to log registrar health at startup.
	Ping(ctx context.Context) error

	// Capabilities returns the per-registrar feature flags. Used by
	// the service layer to short-circuit paths the registrar does not
	// support (e.g. SetDSRecords).
	Capabilities() Capabilities

	// CheckDomain asks the registrar whether the domain is available
	// for registration + the per-period prices.
	CheckDomain(ctx context.Context, req CheckDomainRequest) (*CheckDomainResponse, error)

	// RegisterDomain places a new registration order with the registrar.
	// Returns ErrAlreadyExists when the caller already owns the domain.
	RegisterDomain(ctx context.Context, req RegisterDomainRequest) (*RegisterDomainResponse, error)

	// RenewDomain extends an existing registration by PeriodYears.
	RenewDomain(ctx context.Context, req RenewDomainRequest) (*RenewDomainResponse, error)

	// TransferDomain initiates an EPP transfer from another registrar.
	// Returns ErrBadRequest when the auth code is wrong + ErrConflict
	// when the domain is locked.
	TransferDomain(ctx context.Context, req TransferDomainRequest) (*TransferDomainResponse, error)

	// GetDomain returns the live state the registrar reports for the
	// domain (vs. the cached state in dns_domains).
	GetDomain(ctx context.Context, req GetDomainRequest) (*GetDomainResponse, error)

	// SetDSRecords publishes the DNSSEC DS set at the parent zone.
	// Optional — see Capabilities.SupportsDNSSEC. Returns
	// ErrBadRequest when the DS set is malformed.
	SetDSRecords(ctx context.Context, req SetDSRecordsRequest) (*SetDSRecordsResponse, error)
}

// NoopProvider is a Provider whose every method returns ErrDisabled.
// Used at bootstrap when the deployer has not configured a registrar
// (the consuming module degrades to 501 not_implemented).
type NoopProvider struct{}

// Name returns "noop".
func (NoopProvider) Name() string { return "noop" }

// Ping returns ErrDisabled.
func (NoopProvider) Ping(_ context.Context) error { return ErrDisabled }

// Capabilities returns a zero-value Capabilities.
func (NoopProvider) Capabilities() Capabilities { return Capabilities{} }

// CheckDomain returns ErrDisabled.
func (NoopProvider) CheckDomain(_ context.Context, _ CheckDomainRequest) (*CheckDomainResponse, error) {
	return nil, ErrDisabled
}

// RegisterDomain returns ErrDisabled.
func (NoopProvider) RegisterDomain(_ context.Context, _ RegisterDomainRequest) (*RegisterDomainResponse, error) {
	return nil, ErrDisabled
}

// RenewDomain returns ErrDisabled.
func (NoopProvider) RenewDomain(_ context.Context, _ RenewDomainRequest) (*RenewDomainResponse, error) {
	return nil, ErrDisabled
}

// TransferDomain returns ErrDisabled.
func (NoopProvider) TransferDomain(_ context.Context, _ TransferDomainRequest) (*TransferDomainResponse, error) {
	return nil, ErrDisabled
}

// GetDomain returns ErrDisabled.
func (NoopProvider) GetDomain(_ context.Context, _ GetDomainRequest) (*GetDomainResponse, error) {
	return nil, ErrDisabled
}

// SetDSRecords returns ErrDisabled.
func (NoopProvider) SetDSRecords(_ context.Context, _ SetDSRecordsRequest) (*SetDSRecordsResponse, error) {
	return nil, ErrDisabled
}
