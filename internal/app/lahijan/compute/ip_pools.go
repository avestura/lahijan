// Package compute: ip_pools.go implements the operator-owned IP-pool
// admin surface (WS-30, ADR-0037). The surface is platform-admin only;
// the HTTP boundary gates every method behind
// RequirePerm(compute.ip_pool.manage). The repo's queries are global
// (no tenant_id) — the pool is the operator's resource.
//
// Layered as the rest of the compute module:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Validation (name shape, CIDR shape, family parity).
//  3. Audit emit (status=pending).
//  4. Repo write (Postgres is the source of truth).
//  5. Audit mark-outcome (success | failure).
//
// Per ADR-0037 the operator owns the data plane (BGP, FRR, static
// route, ...). Lahijan does NOT push anything to Incus from this file;
// the floating-IP attach path (floating_ips.go) handles the best-effort
// Incus forward push when a tenant attaches.
package compute

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// IPPoolRow is the API-facing shape for the operator-owned pool.
type IPPoolRow = database.IPPool

// IPPoolRangeRow is the API-facing shape for a per-pool CIDR range.
type IPPoolRangeRow = database.IPPoolRange

// CreateIPPoolParams carries the operator-controlled fields of a
// create-pool call. Name is required; Description + PtrZoneID +
// IsActive are optional.
type CreateIPPoolParams struct {
	Name        string
	Description string
	PtrZoneID   *uuid.UUID
	IsActive    bool
}

// CreateIPPool creates a new operator-owned IP pool. The pool starts
// empty (no ranges); the operator adds ranges via AddIPPoolRange.
func (s *Service) CreateIPPool(
	ctx context.Context,
	_ uuid.UUID, // tenant not used; pool is global. Kept for API symmetry.
	userID uuid.UUID,
	params CreateIPPoolParams,
) (IPPoolRow, error) {
	if strings.TrimSpace(params.Name) == "" {
		return IPPoolRow{}, ErrInvalidName
	}
	isActive := params.IsActive // defaults to false (zero value) if unset
	row, err := s.repos.IPPools.Create(ctx, database.CreateIPPoolParams{
		Name:        params.Name,
		Description: descriptionOrNil(params.Description),
		PtrZoneID:   params.PtrZoneID,
		IsActive:    isActive,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return IPPoolRow{}, fmt.Errorf("%w: name=%s", ErrIPPoolNameTaken, params.Name)
		}
		return IPPoolRow{}, fmt.Errorf("compute: create ip pool: %w", err)
	}

	// Audit emit. Pool admin is a platform-admin action; the actor is
	// captured by user_id, the tenant_id is nil (global action).
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeIPPoolCreate,
		ResourceType: audit.ResourceIPPool,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"name":         params.Name,
			"ptr_zone_set": params.PtrZoneID != nil,
			"is_active":    isActive,
		},
	})
	_ = auditID // no MarkOutcome here; the side effect is already done.

	return row, nil
}

// GetIPPool returns the non-deleted pool with the given id.
func (s *Service) GetIPPool(ctx context.Context, id uuid.UUID) (IPPoolRow, error) {
	row, err := s.repos.IPPools.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return IPPoolRow{}, ErrIPPoolNotFound
		}
		return IPPoolRow{}, fmt.Errorf("compute: get ip pool: %w", err)
	}
	return row, nil
}

// ListIPPools returns a page of non-deleted pools, oldest first.
func (s *Service) ListIPPools(ctx context.Context, limit, offset int32) ([]IPPoolRow, error) {
	rows, err := s.repos.IPPools.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("compute: list ip pools: %w", err)
	}
	return rows, nil
}

// CountIPPools returns the number of non-deleted pools.
func (s *Service) CountIPPools(ctx context.Context) (int64, error) {
	return s.repos.IPPools.Count(ctx)
}

// UpdateIPPoolParams carries the mutable fields of a pool. The name is
// immutable.
type UpdateIPPoolParams struct {
	ID          uuid.UUID
	Description string
	PtrZoneID   *uuid.UUID
	IsActive    bool
}

// UpdateIPPool replaces the mutable fields of a pool.
func (s *Service) UpdateIPPool(
	ctx context.Context,
	_ uuid.UUID,
	userID uuid.UUID,
	params UpdateIPPoolParams,
) error {
	if _, err := s.lookupIPPool(ctx, params.ID); err != nil {
		return err
	}
	desc := descriptionOrNil(params.Description)
	if err := s.repos.IPPools.Update(ctx, database.UpdateIPPoolParams{
		ID:          params.ID,
		Description: desc,
		PtrZoneID:   params.PtrZoneID,
		IsActive:    params.IsActive,
	}); err != nil {
		return fmt.Errorf("compute: update ip pool: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeIPPoolUpdate,
		ResourceType: audit.ResourceIPPool,
		ResourceID:   &params.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"ptr_zone_set": params.PtrZoneID != nil,
			"is_active":    params.IsActive,
		},
	})
	return nil
}

// DeleteIPPool soft-deletes the pool. The caller is responsible for
// releasing every floating-IP allocation first; the service refuses to
// delete a pool that still has live allocations (ON DELETE RESTRICT in
// the schema is the last-line defence).
func (s *Service) DeleteIPPool(
	ctx context.Context,
	_ uuid.UUID,
	userID uuid.UUID,
	poolID uuid.UUID,
) error {
	if _, err := s.lookupIPPool(ctx, poolID); err != nil {
		return err
	}
	// Refuse if any live allocations exist. The cross-tenant count is
	// the right check here because the pool is global.
	addrs, err := s.repos.FloatingIPs.ListAllAddressesInPool(ctx, poolID)
	if err != nil {
		return fmt.Errorf("compute: list allocations for pool: %w", err)
	}
	if len(addrs) > 0 {
		return fmt.Errorf("compute: delete ip pool: %w (%d allocations remain)", ErrIPPoolHasAllocations, len(addrs))
	}
	if err := s.repos.IPPools.SoftDelete(ctx, poolID); err != nil {
		return fmt.Errorf("compute: soft delete ip pool: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeIPPoolDelete,
		ResourceType: audit.ResourceIPPool,
		ResourceID:   &poolID,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// AddIPPoolRangeParams carries the operator-controlled fields of an
// add-range call. CIDR is the canonical netip.Prefix string. Family is
// the explicit 4 or 6 marker (kept separate from the CIDR so a typo
// cannot make the cached column lie). ExcludedAddresses is the
// operator's reserved-list (gateway, broadcast, ...).
type AddIPPoolRangeParams struct {
	PoolID            uuid.UUID
	Cidr              string
	Family            int32
	ExcludedAddresses []string
}

// AddIPPoolRange registers a CIDR range with the pool. The CIDR is
// validated for canonical form (netip.ParsePrefix) + family parity
// (v4 prefix with family=4, v6 with family=6).
func (s *Service) AddIPPoolRange(
	ctx context.Context,
	_ uuid.UUID,
	userID uuid.UUID,
	params AddIPPoolRangeParams,
) (IPPoolRangeRow, error) {
	if _, err := s.lookupIPPool(ctx, params.PoolID); err != nil {
		return IPPoolRangeRow{}, err
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(params.Cidr))
	if err != nil {
		return IPPoolRangeRow{}, fmt.Errorf("%w: %s", ErrInvalidCIDR, params.Cidr)
	}
	prefix = prefix.Masked()
	// Re-emit the canonical form so the stored string is stable.
	canon := prefix.String()
	if fam := familyOf(prefix); fam != params.Family {
		return IPPoolRangeRow{}, fmt.Errorf("%w: cidr=%s family=%d", ErrInvalidIPFamily, canon, params.Family)
	}
	// Validate every excluded address parses + sits inside the range.
	cleanExcluded := make([]string, 0, len(params.ExcludedAddresses))
	for _, e := range params.ExcludedAddresses {
		addr, errParse := netip.ParseAddr(strings.TrimSpace(e))
		if errParse != nil {
			return IPPoolRangeRow{}, fmt.Errorf("compute: excluded address %q is not a valid IP: %w", e, errParse)
		}
		if !prefix.Contains(addr) {
			return IPPoolRangeRow{}, fmt.Errorf("compute: excluded address %q is not inside the range %s", e, canon)
		}
		cleanExcluded = append(cleanExcluded, addr.String())
	}

	row, err := s.repos.IPPoolRanges.Create(ctx, database.CreateIPPoolRangeParams{
		PoolID:            params.PoolID,
		Cidr:              canon,
		Family:            params.Family,
		ExcludedAddresses: cleanExcluded,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return IPPoolRangeRow{}, fmt.Errorf("%w: cidr=%s", ErrIPPoolRangeExists, canon)
		}
		return IPPoolRangeRow{}, fmt.Errorf("compute: add ip pool range: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeIPPoolRangeAdd,
		ResourceType: audit.ResourceIPPoolRange,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"pool_id":           params.PoolID.String(),
			"cidr":              canon,
			"family":            params.Family,
			"excluded_count":    len(cleanExcluded),
		},
	})
	return row, nil
}

// ListIPPoolRanges returns a paginated list of ranges in the pool.
func (s *Service) ListIPPoolRanges(
	ctx context.Context,
	poolID uuid.UUID,
	limit, offset int32,
) ([]IPPoolRangeRow, error) {
	if _, err := s.lookupIPPool(ctx, poolID); err != nil {
		return nil, err
	}
	rows, err := s.repos.IPPoolRanges.List(ctx, poolID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("compute: list ip pool ranges: %w", err)
	}
	return rows, nil
}

// CountIPPoolRanges returns the number of ranges in the pool.
func (s *Service) CountIPPoolRanges(ctx context.Context, poolID uuid.UUID) (int64, error) {
	if _, err := s.lookupIPPool(ctx, poolID); err != nil {
		return 0, err
	}
	return s.repos.IPPoolRanges.Count(ctx, poolID)
}

// DeleteIPPoolRange removes a range from its pool. Idempotent at the
// HTTP layer (a missing range surfaces as 404 here). Existing
// floating_ips allocations inside the range remain valid (they
// reference pool_id, not range_id).
func (s *Service) DeleteIPPoolRange(
	ctx context.Context,
	_ uuid.UUID,
	userID uuid.UUID,
	poolID, rangeID uuid.UUID,
) error {
	if _, err := s.lookupIPPool(ctx, poolID); err != nil {
		return err
	}
	row, err := s.repos.IPPoolRanges.Get(ctx, rangeID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrIPPoolRangeNotFound
		}
		return fmt.Errorf("compute: get ip pool range: %w", err)
	}
	if row.PoolID != poolID {
		return ErrIPPoolRangeNotFound
	}
	if err := s.repos.IPPoolRanges.Delete(ctx, poolID, rangeID); err != nil {
		return fmt.Errorf("compute: delete ip pool range: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeIPPoolRangeDelete,
		ResourceType: audit.ResourceIPPoolRange,
		ResourceID:   &rangeID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"pool_id": poolID.String(),
			"cidr":    row.Cidr,
		},
	})
	return nil
}

// lookupIPPool returns the pool or ErrIPPoolNotFound. Centralised so the
// CRUD methods get a consistent 404 mapping.
func (s *Service) lookupIPPool(ctx context.Context, id uuid.UUID) (IPPoolRow, error) {
	row, err := s.repos.IPPools.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return IPPoolRow{}, ErrIPPoolNotFound
		}
		return IPPoolRow{}, fmt.Errorf("compute: lookup ip pool: %w", err)
	}
	return row, nil
}

// familyOf returns 4 for v4 prefixes, 6 for v6 prefixes.
func familyOf(prefix netip.Prefix) int32 {
	if prefix.Addr().Is4() {
		return 4
	}
	return 6
}

// descriptionOrNil returns nil for an empty description so Postgres
// stores NULL (matches the column's nullable shape + lets the UI
// distinguish "no description" from "empty description").
func descriptionOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Sentinel returned when a delete-pool call would strand allocations.
// Wrap with the count when surfacing; the handler maps to 409 conflict.
var ErrIPPoolHasAllocations = errors.New("compute: ip pool still has allocations")
