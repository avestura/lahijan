// Package database: dns_zones_repo.go wraps the sqlc-generated dns_zones
// queries (WS-12). Every query is tenant-scoped via WithTenant at the
// repository seam; callers cannot pass a tenant id directly. The
// canonical_id column is the PDNS-assigned zone id; the DNS service
// (WS-15) consults this table to translate a tenant context into the
// canonical id before calling the PowerDNS driver.
//
// The cross-tenant GetByCanonicalGlobal (admin-only) path is the one
// exception: it's used by the DNS service's "is this canonical id already
// claimed by another tenant?" check at zone-create time.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// DNSZoneKind mirrors the dns_zones.kind CHECK constraint values.
const (
	DNSZoneKindNative = "Native"
	DNSZoneKindMaster = "Master"
	DNSZoneKindSlave  = "Slave"
)

// DNSZonesRepository is the persistence boundary for the dns_zones table.
type DNSZonesRepository struct {
	q *gen.Queries
}

// NewDNSZonesRepository wraps the given sqlc queries.
func NewDNSZonesRepository(q *gen.Queries) *DNSZonesRepository {
	return &DNSZonesRepository{q: q}
}

// CreateDNSZoneParams carries the user-controlled fields of a new dns_zones
// row. TenantID is taken from the request context, NOT from the caller.
type CreateDNSZoneParams struct {
	CanonicalID string
	Name        string
	Kind        string
	Description string
}

// Create inserts a new dns_zones row scoped to the tenant in ctx.
func (r *DNSZonesRepository) Create(
	ctx context.Context,
	arg CreateDNSZoneParams,
) (gen.DnsZone, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsZone{}, err
	}
	kind := arg.Kind
	if kind == "" {
		kind = DNSZoneKindNative
	}
	return r.q.CreateDNSZone(ctx, gen.CreateDNSZoneParams{
		TenantID:    tenantID,
		CanonicalID: arg.CanonicalID,
		Name:        arg.Name,
		Kind:        kind,
		Description: arg.Description,
	})
}

// Get returns the dns_zones row with id within the tenant in ctx.
func (r *DNSZonesRepository) Get(ctx context.Context, id uuid.UUID) (gen.DnsZone, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsZone{}, err
	}
	return r.q.GetDNSZoneByID(ctx, gen.GetDNSZoneByIDParams{TenantID: tenantID, ID: id})
}

// GetByCanonical returns the dns_zones row for canonicalID within the tenant
// in ctx. Used by the DNS service on every privileged call to enforce
// tenant isolation at the repository seam — a tenant cannot operate on a
// zone they do not own.
func (r *DNSZonesRepository) GetByCanonical(
	ctx context.Context,
	canonicalID string,
) (gen.DnsZone, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsZone{}, err
	}
	return r.q.GetDNSZoneByCanonicalForTenant(ctx, gen.GetDNSZoneByCanonicalForTenantParams{
		TenantID:    tenantID,
		CanonicalID: canonicalID,
	})
}

// GetByCanonicalGlobal is the admin-only cross-tenant lookup. Returns the
// dns_zones row for canonicalID regardless of which tenant owns it. Used by
// the DNS service at zone-create time to short-circuit "another tenant
// already owns this zone".
func (r *DNSZonesRepository) GetByCanonicalGlobal(
	ctx context.Context,
	canonicalID string,
) (gen.DnsZone, error) {
	return r.q.GetDNSZoneByCanonical(ctx, canonicalID)
}

// GetByIDGlobal is the admin-only cross-tenant lookup by primary key.
// Used by the WS-30 PTR publisher (program/compute_ip_ptrs.go) to find
// the operator-owned reverse zone by id without knowing which tenant
// owns it.
func (r *DNSZonesRepository) GetByIDGlobal(
	ctx context.Context,
	id uuid.UUID,
) (gen.DnsZone, error) {
	return r.q.GetDNSZoneByIDGlobal(ctx, id)
}

// List returns a page of dns_zones rows within the tenant in ctx.
func (r *DNSZonesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.DnsZone, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListDNSZones(ctx, gen.ListDNSZonesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of dns_zones rows within the tenant in ctx.
func (r *DNSZonesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountDNSZones(ctx, tenantID)
}

// SetCachedDNSSEC flips the cached is_dnssec_enabled flag for the zone.
// Called by the DNS service after a successful EnableDNSSEC / DisableDNSSEC
// against PDNS so the admin UI does not need a PDNS round-trip to render.
func (r *DNSZonesRepository) SetCachedDNSSEC(
	ctx context.Context,
	id uuid.UUID,
	enabled bool,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSZoneDNSSECCached(ctx, gen.SetDNSZoneDNSSECCachedParams{
		TenantID:        tenantID,
		ID:              id,
		IsDnssecEnabled: enabled,
	})
}

// SetCachedAXFR flips the cached is_axfr_enabled flag for the zone.
func (r *DNSZonesRepository) SetCachedAXFR(
	ctx context.Context,
	id uuid.UUID,
	enabled bool,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetDNSZoneAXFRCached(ctx, gen.SetDNSZoneAXFRCachedParams{
		TenantID:      tenantID,
		ID:            id,
		IsAxfrEnabled: enabled,
	})
}

// SetDescription updates the description of a zone.
func (r *DNSZonesRepository) SetDescription(
	ctx context.Context,
	id uuid.UUID,
	description string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.UpdateDNSZoneDescription(ctx, gen.UpdateDNSZoneDescriptionParams{
		TenantID: tenantID, ID: id, Description: description,
	})
}

// SetKind updates the kind of a zone (Native / Master / Slave). The CHECK
// constraint rejects anything else at the DB layer.
func (r *DNSZonesRepository) SetKind(
	ctx context.Context,
	id uuid.UUID,
	kind string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.UpdateDNSZoneKind(ctx, gen.UpdateDNSZoneKindParams{
		TenantID: tenantID, ID: id, Kind: kind,
	})
}

// Delete removes the dns_zones row within the tenant in ctx.
func (r *DNSZonesRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteDNSZone(ctx, gen.DeleteDNSZoneParams{TenantID: tenantID, ID: id})
}
