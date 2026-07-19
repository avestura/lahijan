// Package database: dns_records_repo.go wraps the sqlc-generated dns_records
// queries (WS-15). Every query is tenant-scoped via WithTenant at the
// repository seam — callers cannot pass a tenant id directly. The zone_id
// is always supplied by the caller (the DNS service resolves it from the
// dns_zones table first); tenant + zone scoping together enforce isolation
// at the repository boundary.
//
// Rows are NOT soft-deleted: deleting an RR removes the row because every
// historical query goes through audit_log instead. This keeps the unique
// constraint honest and the table small.
package database

import (
	"context"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// DNSRecordsRepository is the persistence boundary for the dns_records table.
type DNSRecordsRepository struct {
	q *gen.Queries
}

// NewDNSRecordsRepository wraps the given sqlc queries.
func NewDNSRecordsRepository(q *gen.Queries) *DNSRecordsRepository {
	return &DNSRecordsRepository{q: q}
}

// CreateDNSRecordParams carries the user-controlled fields of a new
// dns_records row. TenantID is taken from the request context, NOT from
// the caller. ZoneID must already be resolved (typically via
// DNSZonesRepository.GetByCanonical) so the FK is satisfied.
type CreateDNSRecordParams struct {
	ZoneID   uuid.UUID
	Name     string
	Type     string
	Content  string
	TTL      int32
	Prio     int32
	Disabled bool
}

// Create inserts a new dns_records row scoped to the tenant in ctx.
func (r *DNSRecordsRepository) Create(
	ctx context.Context,
	arg CreateDNSRecordParams,
) (gen.DnsRecord, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsRecord{}, err
	}
	ttl := arg.TTL
	if ttl == 0 {
		ttl = 3600
	}
	return r.q.CreateDNSRecord(ctx, gen.CreateDNSRecordParams{
		TenantID: tenantID,
		ZoneID:   arg.ZoneID,
		Name:     arg.Name,
		Type:     arg.Type,
		Content:  arg.Content,
		Ttl:      ttl,
		Prio:     arg.Prio,
		Disabled: arg.Disabled,
	})
}

// Get returns the dns_records row with id within the tenant in ctx.
func (r *DNSRecordsRepository) Get(ctx context.Context, id uuid.UUID) (gen.DnsRecord, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsRecord{}, err
	}
	return r.q.GetDNSRecordByID(ctx, gen.GetDNSRecordByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByIdentity returns the dns_records row matching (zone_id, name, type,
// content) within the tenant in ctx. Used by the DNS service to
// short-circuit "this RR already exists" before issuing a PDNS REPLACE.
func (r *DNSRecordsRepository) GetByIdentity(
	ctx context.Context,
	zoneID uuid.UUID,
	name, recordType, content string,
) (gen.DnsRecord, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.DnsRecord{}, err
	}
	return r.q.GetDNSRecordByIdentity(ctx, gen.GetDNSRecordByIdentityParams{
		TenantID: tenantID,
		ZoneID:   zoneID,
		Name:     name,
		Type:     recordType,
		Content:  content,
	})
}

// List returns a page of dns_records rows within the zone and the tenant
// in ctx. Ordered by (name, type) so the UI renders a stable list.
func (r *DNSRecordsRepository) List(
	ctx context.Context,
	zoneID uuid.UUID,
	limit, offset int32,
) ([]gen.DnsRecord, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListDNSRecordsInZone(ctx, gen.ListDNSRecordsInZoneParams{
		TenantID: tenantID, ZoneID: zoneID, Limit: limit, Offset: offset,
	})
}

// CountInZone returns the number of dns_records rows in the zone within
// the tenant in ctx.
func (r *DNSRecordsRepository) CountInZone(
	ctx context.Context,
	zoneID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountDNSRecordsInZone(ctx, gen.CountDNSRecordsInZoneParams{
		TenantID: tenantID, ZoneID: zoneID,
	})
}

// CountForTenant returns the total number of dns_records rows within the
// tenant in ctx. Used by the quota checker.
func (r *DNSRecordsRepository) CountForTenant(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountDNSRecordsForTenant(ctx, tenantID)
}

// UpdateDNSRecordParams carries the mutable fields of a dns_records row.
// Name and Type are immutable — callers wanting a "rename" issue a delete
// + create so the audit trail stays honest.
type UpdateDNSRecordParams struct {
	Content  string
	TTL      int32
	Prio     int32
	Disabled bool
}

// Update replaces content / ttl / prio / disabled for the dns_records row
// with id within the tenant in ctx.
func (r *DNSRecordsRepository) Update(
	ctx context.Context,
	id uuid.UUID,
	arg UpdateDNSRecordParams,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	ttl := arg.TTL
	if ttl == 0 {
		ttl = 3600
	}
	return r.q.UpdateDNSRecord(ctx, gen.UpdateDNSRecordParams{
		TenantID: tenantID,
		ID:       id,
		Content:  arg.Content,
		Ttl:      ttl,
		Prio:     arg.Prio,
		Disabled: arg.Disabled,
	})
}

// Delete removes the dns_records row with id within the tenant in ctx.
func (r *DNSRecordsRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteDNSRecord(ctx, gen.DeleteDNSRecordParams{
		TenantID: tenantID, ID: id,
	})
}

// DeleteAllInZone removes every dns_records row in the zone within the
// tenant in ctx. Used by the zone-delete path so the FK cascade is
// explicit even before the zone row goes away.
func (r *DNSRecordsRepository) DeleteAllInZone(
	ctx context.Context,
	zoneID uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteAllDNSRecordsInZone(ctx, gen.DeleteAllDNSRecordsInZoneParams{
		TenantID: tenantID, ZoneID: zoneID,
	})
}
