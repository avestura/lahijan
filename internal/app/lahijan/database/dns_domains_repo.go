// Package database: dns_domains_repo.go wraps the sqlc-generated
// dns_domains queries (WS-28). Every query is tenant-scoped via WithTenant
// at the repository seam; callers cannot pass a tenant id directly (the
// one exception is GetByOrderGlobal — admin-only, used by the registrar
// service to deduplicate orders cross-tenant).
//
// The repository mirrors the dns_zones_repo.go shape (WS-12): the
// gen.Queries methods are the source of truth, and this wrapper adds
// typed CreateDNSDomainParams that elides the tenant id (which is taken
// from the request context).
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// DNSDomainStatus mirrors the dns_domains.status CHECK constraint values.
// Lifecycle: available -> pending -> (registered | transferred) -> expired.
const (
	DNSDomainStatusAvailable   = "available"
	DNSDomainStatusPending     = "pending"
	DNSDomainStatusRegistered  = "registered"
	DNSDomainStatusTransferred = "transferred"
	DNSDomainStatusExpired     = "expired"
)

// DNSDomainsRepository is the persistence boundary for the dns_domains
// table. Every read/write is tenant-scoped via WithTenant at the
// repository seam.
type DNSDomainsRepository struct {
	q *gen.Queries
}

// NewDNSDomainsRepository wraps the given sqlc queries.
func NewDNSDomainsRepository(q *gen.Queries) *DNSDomainsRepository {
	return &DNSDomainsRepository{q: q}
}

// CreateDNSDomainParams carries the user-controlled fields of a new
// dns_domains row. TenantID is taken from the request context, NOT from
// the caller. Status defaults to "available" when empty.
type CreateDNSDomainParams struct {
	Name             string
	Status           string
	RegistrarOrderID string
	ContactProfileID string
	ZoneID           *uuid.UUID
	PriceCents       int64
	Currency         string
	PeriodYears      int32
	LedgerEntryID    *uuid.UUID
	IsDNSSECEnabled  bool
	IsAutoRenew      bool
	RegisteredAt     *time.Time
	ExpiresAt        *time.Time
}

// Create inserts a new dns_domains row scoped to the tenant in ctx.
func (r *DNSDomainsRepository) Create(
	ctx context.Context,
	arg CreateDNSDomainParams,
) (gen.DnsDomain, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsDomain{}, err
	}
	status := arg.Status
	if status == "" {
		status = DNSDomainStatusAvailable
	}
	period := arg.PeriodYears
	if period == 0 {
		period = 1
	}
	currency := arg.Currency
	if currency == "" {
		currency = "USD"
	}
	return r.q.CreateDNSDomain(ctx, gen.CreateDNSDomainParams{
		TenantID:         tenantID,
		Name:             arg.Name,
		Status:           status,
		RegistrarOrderID: arg.RegistrarOrderID,
		ContactProfileID: arg.ContactProfileID,
		ZoneID:           arg.ZoneID,
		PriceCents:       arg.PriceCents,
		Currency:         currency,
		PeriodYears:      period,
		LedgerEntryID:    arg.LedgerEntryID,
		IsDnssecEnabled:  arg.IsDNSSECEnabled,
		IsAutoRenew:      arg.IsAutoRenew,
		RegisteredAt:     arg.RegisteredAt,
		ExpiresAt:        arg.ExpiresAt,
	})
}

// Get returns the dns_domains row with id within the tenant in ctx.
func (r *DNSDomainsRepository) Get(ctx context.Context, id uuid.UUID) (gen.DnsDomain, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsDomain{}, err
	}
	return r.q.GetDNSDomainByID(ctx, gen.GetDNSDomainByIDParams{TenantID: tenantID, ID: id})
}

// GetByName returns the dns_domains row for name within the tenant in ctx.
// Used by the registrar service on every privileged call to enforce tenant
// isolation at the repository seam.
func (r *DNSDomainsRepository) GetByName(
	ctx context.Context,
	name string,
) (gen.DnsDomain, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsDomain{}, err
	}
	return r.q.GetDNSDomainByName(ctx, gen.GetDNSDomainByNameParams{
		TenantID: tenantID, Name: name,
	})
}

// GetByOrderGlobal is the admin-only cross-tenant lookup. Returns the
// dns_domains row for registrarOrderID regardless of which tenant owns
// it. Used by the registrar service at registration time so two tenants
// cannot double-claim the same order id.
func (r *DNSDomainsRepository) GetByOrderGlobal(
	ctx context.Context,
	registrarOrderID string,
) (gen.DnsDomain, error) {
	return r.q.GetDNSDomainByOrderGlobal(ctx, registrarOrderID)
}

// List returns a page of dns_domains rows within the tenant in ctx.
func (r *DNSDomainsRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.DnsDomain, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListDNSDomains(ctx, gen.ListDNSDomainsParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// ListExpiringBefore returns every domain in the tenant whose
// expires_at is before the supplied timestamp. Used by the future
// renewal job.
func (r *DNSDomainsRepository) ListExpiringBefore(
	ctx context.Context,
	before time.Time,
) ([]gen.DnsDomain, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListDNSDomainsExpiringBefore(ctx, gen.ListDNSDomainsExpiringBeforeParams{
		TenantID: tenantID, ExpiresAt: &before,
	})
}

// Count returns the number of dns_domains rows within the tenant in ctx.
func (r *DNSDomainsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountDNSDomains(ctx, tenantID)
}

// SetStatus flips the lifecycle status. Called by the registrar service
// after a register / renew / transfer / expire transition.
func (r *DNSDomainsRepository) SetStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSDomainStatus(ctx, gen.SetDNSDomainStatusParams{
		TenantID: tenantID, ID: id, Status: status,
	})
}

// SetOrderID records the registrar's order id + transitions the status
// (typically "pending" -> "registered"). Called by the registrar service
// after a successful register / transfer.
func (r *DNSDomainsRepository) SetOrderID(
	ctx context.Context,
	id uuid.UUID,
	orderID, status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSDomainOrderID(ctx, gen.SetDNSDomainOrderIDParams{
		TenantID: tenantID, ID: id, RegistrarOrderID: orderID, Status: status,
	})
}

// SetZoneLink links the row to the auto-provisioned dns_zones row.
func (r *DNSDomainsRepository) SetZoneLink(
	ctx context.Context,
	id, zoneID uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	zone := zoneID
	return r.q.SetDNSDomainZoneLink(ctx, gen.SetDNSDomainZoneLinkParams{
		TenantID: tenantID, ID: id, ZoneID: &zone,
	})
}

// SetLedgerLink records the ledger entry id that paid for the
// registration / renewal.
func (r *DNSDomainsRepository) SetLedgerLink(
	ctx context.Context,
	id uuid.UUID,
	ledgerID uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	ledger := ledgerID
	return r.q.SetDNSDomainLedgerLink(ctx, gen.SetDNSDomainLedgerLinkParams{
		TenantID: tenantID, ID: id, LedgerEntryID: &ledger,
	})
}

// SetExpiry updates the registered_at + expires_at timestamps after a
// successful register / renew. registeredAt is only set when non-nil
// (renew keeps the original registration date).
func (r *DNSDomainsRepository) SetExpiry(
	ctx context.Context,
	id uuid.UUID,
	registeredAt, expiresAt *time.Time,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSDomainExpiry(ctx, gen.SetDNSDomainExpiryParams{
		TenantID: tenantID, ID: id, RegisteredAt: registeredAt, ExpiresAt: expiresAt,
	})
}

// SetAutoRenew flips the cached auto-renew flag.
func (r *DNSDomainsRepository) SetAutoRenew(
	ctx context.Context,
	id uuid.UUID,
	autoRenew bool,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSDomainAutoRenew(ctx, gen.SetDNSDomainAutoRenewParams{
		TenantID: tenantID, ID: id, IsAutoRenew: autoRenew,
	})
}

// SetCachedDNSSEC flips the cached is_dnssec_enabled flag. Called by
// the registrar service after a successful publish-DS + sign-zone
// composition.
func (r *DNSDomainsRepository) SetCachedDNSSEC(
	ctx context.Context,
	id uuid.UUID,
	enabled bool,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSDomainDNSSECCached(ctx, gen.SetDNSDomainDNSSECCachedParams{
		TenantID: tenantID, ID: id, IsDnssecEnabled: enabled,
	})
}

// Delete removes the dns_domains row within the tenant in ctx.
func (r *DNSDomainsRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteDNSDomain(ctx, gen.DeleteDNSDomainParams{TenantID: tenantID, ID: id})
}
