// Package registrar: types.go carries the request/response types every
// Provider implementation must support. The shapes are intentionally
// narrow — Lahijan does not surface every registrar field. Each field
// carries a comment that documents the canonical shape so a new
// implementation can match it without reading the others.
package registrar

import "time"

// DomainStatus is the lifecycle status the registrar reports. The set
// mirrors what every supported registrar exposes; the registrar service
// translates registrar-specific statuses into these.
type DomainStatus string

const (
	// StatusAvailable means the domain is queryable but not owned.
	StatusAvailable DomainStatus = "available"
	// StatusRegistered means the caller owns the domain.
	StatusRegistered DomainStatus = "registered"
	// StatusPending means a register / renew / transfer is in flight.
	StatusPending DomainStatus = "pending"
	// StatusTransferred means the domain was transferred in from another
	// registrar.
	StatusTransferred DomainStatus = "transferred"
	// StatusExpired means the registration lapsed; the caller no longer
	// controls the domain.
	StatusExpired DomainStatus = "expired"
	// StatusUnavailable means the domain is taken by someone else (the
	// search result returned a non-empty owner).
	StatusUnavailable DomainStatus = "unavailable"
)

// CheckDomainRequest is the input to Provider.CheckDomain.
type CheckDomainRequest struct {
	// Domain is the canonical domain name WITHOUT a trailing dot
	// (e.g. "example.com"). The driver normalises per-registrar.
	Domain string
}

// CheckDomainResponse is the output of Provider.CheckDomain. The
// Pricing field carries the per-period retail price the registrar
// charges Lahijan (Lahijan's margin is applied by the service layer).
type CheckDomainResponse struct {
	// Domain echoes the canonical domain name (no trailing dot).
	Domain string

	// Available is true when the domain can be registered through this
	// registrar. False for taken / reserved / premium-only.
	Available bool

	// Status is the lifecycle status the registrar reported. Typically
	// StatusAvailable or StatusUnavailable.
	Status DomainStatus

	// Pricing carries the per-period prices the registrar charges. The
	// service layer picks the entry matching the caller's requested
	// period; the margin is applied on top.
	Pricing []DomainPricing

	// Reason is an optional human-readable note from the registrar
	// (e.g. "premium domain", "reserved"). Empty when the registrar did
	// not provide one.
	Reason string
}

// DomainPricing carries the per-period price for a single registration
// period. The PeriodYears matches the registrar's pricing tier (1 / 2 /
// 5 / 10 years typically).
type DomainPricing struct {
	// PeriodYears is the registration period this price applies to.
	PeriodYears int32

	// PriceCents is the per-period retail price the registrar charges
	// Lahijan, in integer cents. The service layer applies Lahijan's
	// margin on top.
	PriceCents int64

	// Currency is the ISO 4217 code the registrar bills Lahijan in.
	// Always non-empty when PriceCents > 0.
	Currency string
}

// ContactProfile is the registrant + admin + tech + billing contact
// bundle every registrar requires. The fields are the subset every
// supported registrar accepts; per-registrar drivers translate this
// into their own shape.
type ContactProfile struct {
	// OwnerFirstname / OwnerLastname are the legal name of the
	// registrant. Required.
	OwnerFirstname string
	OwnerLastname  string

	// OwnerOrganization is the optional legal entity name. Empty for
	// individual registrations.
	OwnerOrganization string

	// OwnerEmail is the registrant's email address. Required; the
	// registrar sends a verification email here on successful
	// registration.
	OwnerEmail string

	// OwnerPhone is the registrant's phone number in E.164 format.
	// Required.
	OwnerPhone string

	// Address lines. Address1 is required; Address2 is optional.
	Address1 string
	Address2 string

	// City / State / Zip / CountryCode (ISO 3166-1 alpha-2). Required.
	City       string
	State      string
	Zip        string
	CountryCode string
}

// RegisterDomainRequest is the input to Provider.RegisterDomain.
type RegisterDomainRequest struct {
	// Domain is the canonical domain name WITHOUT a trailing dot.
	Domain string

	// PeriodYears is the registration period in years (1..10).
	PeriodYears int32

	// Contact is the registrant contact bundle.
	Contact ContactProfile

	// AutoRenew is the registrar-side auto-renew toggle. When true the
	// registrar charges Lahijan automatically before expiry; the
	// service layer syncs the toggle with the dns_domains row's
	// is_auto_renew.
	AutoRenew bool

	// WHOISPrivacy is the registrar-side WHOIS privacy toggle. When
	// true the registrar replaces the contact with a privacy proxy.
	WHOISPrivacy bool
}

// RegisterDomainResponse is the output of Provider.RegisterDomain.
type RegisterDomainResponse struct {
	// OrderID is the registrar's order identifier. Cross-referenced in
	// dns_domains.registrar_order_id.
	OrderID string

	// Domain echoes the canonical domain name (no trailing dot).
	Domain string

	// Status is the lifecycle status the registrar reported immediately
	// after registration. Typically StatusPending (the registrar waits
	// for payment + registry confirmation) or StatusRegistered (when
	// the registrar confirms synchronously).
	Status DomainStatus

	// ExpiresAt is the registration expiry timestamp. NULL-equivalent
	// when the registrar has not confirmed yet (status=pending).
	ExpiresAt *time.Time

	// PriceCents is the price the registrar charged Lahijan for this
	// specific order, in integer cents. Currency matches.
	PriceCents int64

	// Currency is the ISO 4217 code the registrar billed Lahijan in.
	Currency string
}

// RenewDomainRequest is the input to Provider.RenewDomain.
type RenewDomainRequest struct {
	// Domain is the canonical domain name WITHOUT a trailing dot.
	Domain string

	// OrderID is the registrar's original order identifier (preserved
	// across renewals).
	OrderID string

	// PeriodYears is the renewal period in years (1..10).
	PeriodYears int32
}

// RenewDomainResponse mirrors RegisterDomainResponse for the renew path.
type RenewDomainResponse struct {
	OrderID    string
	Domain     string
	Status     DomainStatus
	ExpiresAt  *time.Time
	PriceCents int64
	Currency   string
}

// TransferDomainRequest is the input to Provider.TransferDomain.
type TransferDomainRequest struct {
	// Domain is the canonical domain name WITHOUT a trailing dot.
	Domain string

	// AuthCode is the EPP auth code the losing registrar shared with
	// the registrant. Required for every gTLD / ccTLD that follows the
	// EPP transfer flow.
	AuthCode string

	// PeriodYears is the renewal period the transfer adds (typically 1).
	PeriodYears int32

	// Contact is the registrant contact bundle.
	Contact ContactProfile
}

// TransferDomainResponse mirrors RegisterDomainResponse for the
// transfer path.
type TransferDomainResponse struct {
	OrderID    string
	Domain     string
	Status     DomainStatus
	ExpiresAt  *time.Time
	PriceCents int64
	Currency   string
}

// GetDomainRequest is the input to Provider.GetDomain.
type GetDomainRequest struct {
	// Domain is the canonical domain name WITHOUT a trailing dot.
	Domain string
}

// GetDomainResponse is the output of Provider.GetDomain. Carries the
// live state from the registrar (vs. the cached state in dns_domains).
type GetDomainResponse struct {
	Domain      string
	OrderID     string
	Status      DomainStatus
	ExpiresAt   *time.Time
	AutoRenew   bool
	Locked      bool // registrar-side transfer lock
	PrivacyOn   bool // registrar-side WHOIS privacy
	Nameservers []string
}

// DSRecord is a single DNSSEC DS record at the parent zone. The
// registrar publishes these so recursive resolvers can validate the
// child zone's signatures.
type DSRecord struct {
	// KeyTag is the DNSSEC key tag.
	KeyTag int32

	// Algorithm is the DNSSEC algorithm number (8 = RSASHA256, 13 =
	// ECDSAP256SHA256, ...).
	Algorithm int32

	// DigestType is the DS digest type (2 = SHA-256).
	DigestType int32

	// Digest is the hex-encoded DS digest.
	Digest string
}

// SetDSRecordsRequest is the input to Provider.SetDSRecords.
type SetDSRecordsRequest struct {
	// Domain is the canonical domain name WITHOUT a trailing dot.
	Domain string

	// Records is the complete DS set to publish at the parent. An empty
	// slice clears the DS set (DNSSEC disable).
	Records []DSRecord
}

// SetDSRecordsResponse carries the registrar's confirmation. The
// propagation is asynchronous; callers should poll GetDomain if they
// need to confirm.
type SetDSRecordsResponse struct {
	Domain     string
	Applied    bool
	RecordCount int32
}

// Capabilities carries the registrar's per-feature flags. Used by the
// service layer to short-circuit paths the registrar does not support
// (e.g. SetDSRecords is optional for registrars that only support
// DNSSEC via auto-DS).
type Capabilities struct {
	// ProviderName is the registrar's internal identifier
	// ("opensrs", "resellerclub", ...).
	ProviderName string

	// SupportsDNSSEC is true when the registrar exposes a SetDSRecords
	// equivalent. False for registrars that only support auto-DS via
	// the registry.
	SupportsDNSSEC bool

	// SupportsTransfers is true when the registrar supports the EPP
	// transfer-in flow. False for registrars that only support new
	// registrations.
	SupportsTransfers bool

	// SupportsWHOISPrivacy is true when the registrar offers a WHOIS
	// privacy proxy.
	SupportsWHOISPrivacy bool

	// SupportsAutoRenew is true when the registrar can charge Lahijan
	// automatically before expiry. False for registrars that require
	// explicit renew calls.
	SupportsAutoRenew bool
}
