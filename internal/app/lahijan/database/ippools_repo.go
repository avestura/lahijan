// Package database: ippools_repo.go wraps the sqlc-generated ip_pools +
// ip_pool_ranges queries (WS-30, ADR-0037). Both tables are GLOBAL
// (operator-owned); the repository does NOT tenant-scope these queries
// because the pool is not a per-tenant resource. The HTTP layer gates
// every pool method behind RequirePerm(compute.ip_pool.manage) so only
// platform admins reach them.
//
// Tenants allocate addresses FROM a pool via the floating_ips table
// (floatingips_repo.go); that side is tenant-scoped.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// IPPoolsRepository is the persistence boundary for the ip_pools table.
// Global — the repository does not tenant-scope queries.
type IPPoolsRepository struct {
	q *gen.Queries
}

// NewIPPoolsRepository wraps the given sqlc queries.
func NewIPPoolsRepository(q *gen.Queries) *IPPoolsRepository {
	return &IPPoolsRepository{q: q}
}

// CreateIPPoolParams carries the user-controlled fields of a new ip_pools
// row. IsActive defaults to TRUE at the service layer; the repo passes
// the explicit value through.
type CreateIPPoolParams struct {
	Name        string
	Description *string
	PtrZoneID   *uuid.UUID
	IsActive    bool
}

// Create inserts a new ip_pools row. Returns the new row.
func (r *IPPoolsRepository) Create(ctx context.Context, arg CreateIPPoolParams) (gen.IpPool, error) {
	return r.q.CreateIPPool(ctx, gen.CreateIPPoolParams{
		Name:        arg.Name,
		Description: arg.Description,
		PtrZoneID:   arg.PtrZoneID,
		IsActive:    arg.IsActive,
	})
}

// Get returns the non-deleted pool with the given id.
func (r *IPPoolsRepository) Get(ctx context.Context, id uuid.UUID) (gen.IpPool, error) {
	return r.q.GetIPPoolByID(ctx, id)
}

// GetByName returns the non-deleted pool with the given operator-chosen name.
func (r *IPPoolsRepository) GetByName(ctx context.Context, name string) (gen.IpPool, error) {
	return r.q.GetIPPoolByName(ctx, name)
}

// List returns a page of non-deleted pools, oldest first (the operator UI
// shows the oldest pool at the top so a stable order survives pagination).
func (r *IPPoolsRepository) List(ctx context.Context, limit, offset int32) ([]gen.IpPool, error) {
	return r.q.ListIPPools(ctx, gen.ListIPPoolsParams{Limit: limit, Offset: offset})
}

// Count returns the number of non-deleted pools.
func (r *IPPoolsRepository) Count(ctx context.Context) (int64, error) {
	return r.q.CountIPPools(ctx)
}

// UpdateIPPoolParams carries the mutable fields of an ip_pools row. The
// name is immutable.
type UpdateIPPoolParams struct {
	ID          uuid.UUID
	Description *string
	PtrZoneID   *uuid.UUID
	IsActive    bool
}

// Update replaces the mutable fields of a pool.
func (r *IPPoolsRepository) Update(ctx context.Context, arg UpdateIPPoolParams) error {
	return r.q.UpdateIPPool(ctx, gen.UpdateIPPoolParams{
		ID:          arg.ID,
		Description: arg.Description,
		PtrZoneID:   arg.PtrZoneID,
		IsActive:    arg.IsActive,
	})
}

// SetActive flips just the is_active flag. Used by the service layer's
// activate / deactivate shortcuts so the audit row metadata records only
// the changed field.
func (r *IPPoolsRepository) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return r.q.SetIPPoolActive(ctx, gen.SetIPPoolActiveParams{ID: id, IsActive: active})
}

// SoftDelete marks the pool deleted_at = now(). The caller is
// responsible for releasing every floating-IP allocation first; the
// ON DELETE RESTRICT on floating_ips.pool_id would otherwise refuse a
// hard delete via SQL.
func (r *IPPoolsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	return r.q.SoftDeleteIPPool(ctx, id)
}

// -------------------------------------------------------------------------
// ip_pool_ranges
// -------------------------------------------------------------------------

// IPPoolRangesRepository is the persistence boundary for the
// ip_pool_ranges table. Global — same rationale as the pool repo.
type IPPoolRangesRepository struct {
	q *gen.Queries
}

// NewIPPoolRangesRepository wraps the given sqlc queries.
func NewIPPoolRangesRepository(q *gen.Queries) *IPPoolRangesRepository {
	return &IPPoolRangesRepository{q: q}
}

// CreateIPPoolRangeParams carries the user-controlled fields of a new
// ip_pool_ranges row. ExcludedAddresses is a slice of IP-string literals
// (e.g. ["203.0.113.0", "203.0.113.255"]) the service layer marshals to
// JSONB before insert.
type CreateIPPoolRangeParams struct {
	PoolID            uuid.UUID
	Cidr              string
	Family            int32
	ExcludedAddresses []string
}

// Create inserts a new ip_pool_ranges row. The ExcludedAddresses slice
// is marshalled to a JSONB array; nil/empty becomes "[]".
func (r *IPPoolRangesRepository) Create(ctx context.Context, arg CreateIPPoolRangeParams) (gen.IpPoolRange, error) {
	excluded, err := marshalExcludedAddresses(arg.ExcludedAddresses)
	if err != nil {
		return gen.IpPoolRange{}, err
	}
	return r.q.CreateIPPoolRange(ctx, gen.CreateIPPoolRangeParams{
		PoolID:            arg.PoolID,
		Cidr:              arg.Cidr,
		Family:            arg.Family,
		ExcludedAddresses: excluded,
	})
}

// Get returns the range with the given id.
func (r *IPPoolRangesRepository) Get(ctx context.Context, id uuid.UUID) (gen.IpPoolRange, error) {
	return r.q.GetIPPoolRangeByID(ctx, id)
}

// GetByPoolCIDR returns the range with the given (pool_id, cidr) natural
// key. Used by the service layer to detect "this CIDR is already in the
// pool" without a separate scan.
func (r *IPPoolRangesRepository) GetByPoolCIDR(ctx context.Context, poolID uuid.UUID, cidr string) (gen.IpPoolRange, error) {
	return r.q.GetIPPoolRangeByPoolCIDR(ctx, gen.GetIPPoolRangeByPoolCIDRParams{
		PoolID: poolID, Cidr: cidr,
	})
}

// List returns a paginated list of ranges in the pool.
func (r *IPPoolRangesRepository) List(ctx context.Context, poolID uuid.UUID, limit, offset int32) ([]gen.IpPoolRange, error) {
	return r.q.ListIPPoolRanges(ctx, gen.ListIPPoolRangesParams{
		PoolID: poolID, Limit: limit, Offset: offset,
	})
}

// ListAllForPool returns every range in the pool without pagination.
// Used by the allocation logic so the service can walk every range in a
// single query when looking for the next free IP.
func (r *IPPoolRangesRepository) ListAllForPool(ctx context.Context, poolID uuid.UUID) ([]gen.IpPoolRange, error) {
	return r.q.ListAllIPPoolRangesForPool(ctx, poolID)
}

// Count returns the number of ranges in the pool.
func (r *IPPoolRangesRepository) Count(ctx context.Context, poolID uuid.UUID) (int64, error) {
	return r.q.CountIPPoolRanges(ctx, poolID)
}

// Delete hard-deletes the range. Idempotent — a missing row surfaces as
// ErrNoRows so the caller can decide whether to treat that as success
// (DELETE is idempotent at the HTTP layer) or as a 404. Existing
// floating_ips allocations inside the range remain valid (they reference
// pool_id, not range_id).
func (r *IPPoolRangesRepository) Delete(ctx context.Context, poolID, id uuid.UUID) error {
	return r.q.DeleteIPPoolRange(ctx, gen.DeleteIPPoolRangeParams{PoolID: poolID, ID: id})
}

// ParseExcludedAddresses unmarshals the JSONB excluded_addresses column
// back into a slice of IP-string literals. Returns an empty slice for a
// nil/empty blob. The caller is expected to netip.ParseAddr each entry.
func ParseExcludedAddresses(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// marshalExcludedAddresses serialises the slice to the JSONB array the
// ip_pool_ranges.excluded_addresses column expects. nil/empty becomes
// the literal "[]" so the column never holds NULL (the migration's
// DEFAULT '[]'::jsonb is a backstop for direct SQL inserts).
func marshalExcludedAddresses(in []string) (json.RawMessage, error) {
	if in == nil {
		in = []string{}
	}
	out, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	return out, nil
}
