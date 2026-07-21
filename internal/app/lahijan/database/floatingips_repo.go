// Package database: floatingips_repo.go wraps the sqlc-generated
// floating_ips queries (WS-30, ADR-0037). Every query is tenant-scoped
// via WithTenant at the repository seam — callers cannot pass a tenant
// id directly. A cross-tenant floating_ip_id surfaces as ErrNoRows,
// never as the row itself.
//
// The one exception is ListAllAddressesInPool which is global by design
// — the operator's free/allocated counter needs the count across every
// tenant, not just the caller's tenant. The HTTP boundary gates every
// caller of that helper behind RequirePerm(compute.ip_pool.manage).
package database

import (
	"context"

	"github.com/google/uuid"
	"net/netip"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// FloatingIPsRepository is the persistence boundary for the floating_ips
// table. Tenant-scoped via WithTenant at the repository seam.
type FloatingIPsRepository struct {
	q *gen.Queries
}

// NewFloatingIPsRepository wraps the given sqlc queries.
func NewFloatingIPsRepository(q *gen.Queries) *FloatingIPsRepository {
	return &FloatingIPsRepository{q: q}
}

// CreateFloatingIPParams carries the user-controlled fields of a new
// floating_ips row. TenantID is taken from the request context, NOT
// from the caller. Address is the resolved host IP (the service layer
// computes the next-free address before this call).
type CreateFloatingIPParams struct {
	PoolID             uuid.UUID
	Address            netip.Addr
	Family             int32
	PtrTarget          *string
	InstanceID         *uuid.UUID
	NetworkName        *string
	ForwardPushStatus  string
}

// Create inserts a new floating_ips row scoped to the tenant in ctx.
// Returns the new row so the caller can return the id straight to the API.
func (r *FloatingIPsRepository) Create(
	ctx context.Context,
	arg CreateFloatingIPParams,
) (gen.FloatingIp, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.FloatingIp{}, err
	}
	return r.q.CreateFloatingIP(ctx, gen.CreateFloatingIPParams{
		TenantID:           tenantID,
		PoolID:             arg.PoolID,
		Address:            arg.Address,
		Family:             arg.Family,
		PtrTarget:          arg.PtrTarget,
		InstanceID:         arg.InstanceID,
		NetworkName:        arg.NetworkName,
		ForwardPushStatus:  arg.ForwardPushStatus,
	})
}

// Get returns the non-deleted floating IP with the given id within the
// tenant in ctx.
func (r *FloatingIPsRepository) Get(ctx context.Context, id uuid.UUID) (gen.FloatingIp, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.FloatingIp{}, err
	}
	return r.q.GetFloatingIPByID(ctx, gen.GetFloatingIPByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByAddress returns the non-deleted floating IP with the given
// address within the tenant in ctx. Cross-tenant collisions are caught
// by the global unique index on (address) WHERE deleted_at IS NULL.
func (r *FloatingIPsRepository) GetByAddress(ctx context.Context, addr netip.Addr) (gen.FloatingIp, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.FloatingIp{}, err
	}
	return r.q.GetFloatingIPByAddress(ctx, gen.GetFloatingIPByAddressParams{
		TenantID: tenantID, Address: addr,
	})
}

// List returns a page of the tenant's floating IPs, newest first.
func (r *FloatingIPsRepository) List(ctx context.Context, limit, offset int32) ([]gen.FloatingIp, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListFloatingIPs(ctx, gen.ListFloatingIPsParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted floating IPs in the tenant.
func (r *FloatingIPsRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountFloatingIPs(ctx, tenantID)
}

// ListByPool returns a page of the tenant's floating IPs allocated from
// the given pool. Ordered by address for deterministic operator UI
// rendering (the operator sees the pool's allocations in IP order).
func (r *FloatingIPsRepository) ListByPool(
	ctx context.Context,
	poolID uuid.UUID,
	limit, offset int32,
) ([]gen.FloatingIp, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListFloatingIPsByPool(ctx, gen.ListFloatingIPsByPoolParams{
		TenantID: tenantID, PoolID: poolID, Limit: limit, Offset: offset,
	})
}

// CountByPool returns the number of non-deleted floating IPs the tenant
// has allocated from the given pool.
func (r *FloatingIPsRepository) CountByPool(ctx context.Context, poolID uuid.UUID) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountFloatingIPsByPool(ctx, gen.CountFloatingIPsByPoolParams{
		TenantID: tenantID, PoolID: poolID,
	})
}

// GetByInstance returns the floating IP currently attached to the given
// instance within the tenant, if any. Used by the instance-detail
// "attached IP" card.
func (r *FloatingIPsRepository) GetByInstance(ctx context.Context, instanceID uuid.UUID) (gen.FloatingIp, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.FloatingIp{}, err
	}
	return r.q.GetFloatingIPByInstance(ctx, gen.GetFloatingIPByInstanceParams{
		TenantID: tenantID, InstanceID: &instanceID,
	})
}

// ListAllAddressesForTenant returns the addresses (as strings) of every
// non-deleted allocation in the tenant. Used by the allocation logic to
// compute the set of already-allocated addresses without dragging the
// whole row.
func (r *FloatingIPsRepository) ListAllAddressesForTenant(ctx context.Context) ([]string, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAllAddressesForTenant(ctx, tenantID)
}

// ListAllAddressesInPool returns the addresses (as strings) of every
// non-deleted allocation in the pool across every tenant. Admin-only;
// the HTTP boundary gates every caller behind
// RequirePerm(compute.ip_pool.manage). Used by the operator's
// free/allocated counter for the pool.
func (r *FloatingIPsRepository) ListAllAddressesInPool(ctx context.Context, poolID uuid.UUID) ([]string, error) {
	return r.q.ListAllAddressesInPool(ctx, poolID)
}

// SetFloatingIPInstanceParams carries the attach/detach fields. Pass a
// non-nil InstanceID to attach; pass nil to detach. NetworkName +
// ForwardPushStatus record the best-effort Incus forward push outcome.
type SetFloatingIPInstanceParams struct {
	ID                 uuid.UUID
	InstanceID         *uuid.UUID
	ForwardPushStatus  string
	NetworkName        *string
}

// SetInstance attaches (instance_id != nil) or detaches (instance_id == nil)
// the floating IP. The service layer pushes the Incus forward on attach
// (best-effort) and removes it on detach before flipping this column.
func (r *FloatingIPsRepository) SetInstance(ctx context.Context, arg SetFloatingIPInstanceParams) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetFloatingIPInstance(ctx, gen.SetFloatingIPInstanceParams{
		TenantID:          tenantID,
		ID:                arg.ID,
		InstanceID:        arg.InstanceID,
		ForwardPushStatus: arg.ForwardPushStatus,
		NetworkName:       arg.NetworkName,
	})
}

// SetPTRTarget replaces the ptr_target. The service layer re-publishes
// the PTR record into the pool's ptr_zone (or the in-addr.arpa zone the
// tenant owns) on every change.
func (r *FloatingIPsRepository) SetPTRTarget(ctx context.Context, id uuid.UUID, target *string) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetFloatingIPPTRTarget(ctx, gen.SetFloatingIPPTRTargetParams{
		TenantID: tenantID, ID: id, PtrTarget: target,
	})
}

// SetForwardPushStatus records the best-effort Incus forward push
// outcome for the floating IP. Used by the service layer after a
// successful or failed forward push so the operator UI can render it.
func (r *FloatingIPsRepository) SetForwardPushStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
	networkName *string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetFloatingIPForwardPushStatus(ctx, gen.SetFloatingIPForwardPushStatusParams{
		TenantID: tenantID, ID: id, ForwardPushStatus: status, NetworkName: networkName,
	})
}

// SoftDelete marks the allocation deleted_at = now(). The unique index
// on (address) WHERE deleted_at IS NULL releases so the address can be
// re-allocated after release. The service layer removes the Incus
// forward + publishes the PTR-record delete into the pool's ptr_zone
// before this.
func (r *FloatingIPsRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteFloatingIP(ctx, gen.SoftDeleteFloatingIPParams{
		TenantID: tenantID, ID: id,
	})
}
