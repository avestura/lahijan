// Package compute: floating_ips.go implements the tenant-scoped
// floating-IP surface (WS-30, ADR-0037). The surface is the user-facing
// half of WS-30; tenants allocate / list / attach / detach / release /
// set-PTR floating IPs that come from the operator's pool.
//
// Layered as the rest of the compute module:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Validation (pool exists + active; instance exists within tenant;
//    ptr_target is a canonical DNS name when set).
//  3. Audit emit (status=pending) — the row exists even if step 6 fails.
//  4. Allocation / state change in Postgres (source of truth).
//  5. Best-effort Incus forward push (ADR-0037 sub-decision B). A failed
//    push does NOT roll back the allocation; the operator may be using
//    external plumbing. The push outcome is recorded in
//    floating_ips.forward_push_status.
//  6. PTR auto-publish into the pool's reverse zone (when the pool has
//    ptr_zone_id set + the publisher seam is wired).
//  7. WASM event bus emit (compute.ip.assigned / .released).
//  8. Per-IP-hour usage meter start/stop (when the meter seam is wired).
//  9. Audit mark-outcome (success | failure).
package compute

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// FloatingIPRow is the API-facing shape for the tenant-scoped allocation.
type FloatingIPRow = database.FloatingIP

// AllocateFloatingIPParams carries the tenant-controlled fields of an
// allocate call. The service computes the actual address (next-free IP
// from the pool's ranges).
type AllocateFloatingIPParams struct {
	PoolID    uuid.UUID
	PtrTarget string // optional; empty means no PTR record published
}

// AllocateFloatingIP picks the next free IP in the named pool and
// records a new floating_ips row scoped to the caller's tenant. The
// allocation is recorded even when the Incus forward push fails (the
// operator may be using external plumbing — ADR-0037 sub-decision B).
func (s *Service) AllocateFloatingIP(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params AllocateFloatingIPParams,
) (FloatingIPRow, error) {
	pool, err := s.lookupIPPool(ctx, params.PoolID)
	if err != nil {
		return FloatingIPRow{}, err
	}
	if !pool.IsActive {
		return FloatingIPRow{}, ErrIPPoolInactive
	}

	// Pre-validate the PTR target shape (canonical DNS name) so a typo
	// does not strand the row with an unfixable ptr_target.
	var ptrTarget *string
	if strings.TrimSpace(params.PtrTarget) != "" {
		target := ensureTrailingDot(strings.TrimSpace(params.PtrTarget))
		if err := validateCanonicalDNSName(target); err != nil {
			return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.allocate: %w", err)
		}
		ptrTarget = &target
	}

	// Audit emit (status=pending). The row exists even if step 6 fails.
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeFloatingIPAllocate,
		ResourceType: audit.ResourceFloatingIP,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"pool_id":    params.PoolID.String(),
			"ptr_target": ptrTarget,
		},
	})

	// Pick the next free address. The advisory lock the placement
	// driver already holds (per-tenant) is enough for cross-tenant
	// pool races; the unique index on floating_ips.address is the
	// last-line defence.
	addr, family, err := pickNextFreeAddress(ctx, s.repos, params.PoolID)
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.allocate: %w", err)
	}

	row, err := s.repos.FloatingIPs.Create(ctx, database.CreateFloatingIPParams{
		PoolID:            params.PoolID,
		Address:           addr,
		Family:            family,
		PtrTarget:         ptrTarget,
		ForwardPushStatus: "pending",
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			// Race: another concurrent allocation grabbed this address
			// between our pick + insert. Surface as 409 so the caller
			// can retry; the next attempt picks a different address.
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": "concurrent allocation race; retry",
			}})
			return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.allocate: %w", ErrFloatingIPAlreadyAllocated)
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.allocate: %w", err)
	}

	// PTR publish (best-effort). The publisher is the operator-owned
	// reverse zone (when set on the pool).
	if ptrTarget != nil && pool.PtrZoneID != nil && s.ptrPublisher != nil {
		if err := s.ptrPublisher.PublishPTR(ctx, *pool.PtrZoneID, addr, *ptrTarget); err != nil {
			// Best-effort: a failed publish is logged via audit
			// metadata but does not fail the allocation.
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
				"warning":           "ptr_publish_failed",
				"ptr_publish_error": err.Error(),
			}})
		}
	}

	// Start per-IP-hour usage metering (best-effort).
	if s.meter != nil {
		_ = s.meter.StartIPUsage(ctx, tenantID, addr, row.ID)
	}

	// WASM event emit.
	s.emitIPEvent(ctx, eventbus.ComputeIPAssigned, tenantID, userID, row.ID, map[string]any{
		"trigger":  "allocate",
		"address":  addr.String(),
		"family":   family,
		"pool_id":  params.PoolID.String(),
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"address": addr.String(),
		"family":  family,
	}})
	return row, nil
}

// GetFloatingIP returns the floating IP with the given id within the
// caller's tenant.
func (s *Service) GetFloatingIP(ctx context.Context, id uuid.UUID) (FloatingIPRow, error) {
	row, err := s.repos.FloatingIPs.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return FloatingIPRow{}, ErrFloatingIPNotFound
		}
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.get: %w", err)
	}
	return row, nil
}

// ListFloatingIPs returns a paginated list of the caller's tenant's
// floating IPs, newest first.
func (s *Service) ListFloatingIPs(ctx context.Context, limit, offset int32) ([]FloatingIPRow, error) {
	rows, err := s.repos.FloatingIPs.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("compute.floating_ip.list: %w", err)
	}
	return rows, nil
}

// CountFloatingIPs returns the number of non-deleted floating IPs in
// the caller's tenant.
func (s *Service) CountFloatingIPs(ctx context.Context) (int64, error) {
	return s.repos.FloatingIPs.Count(ctx)
}

// GetFloatingIPByInstance returns the floating IP currently attached to
// the given instance within the caller's tenant, if any. Used by the
// instance-detail "attached IP" card.
func (s *Service) GetFloatingIPByInstance(ctx context.Context, instanceID uuid.UUID) (FloatingIPRow, error) {
	row, err := s.repos.FloatingIPs.GetByInstance(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return FloatingIPRow{}, ErrFloatingIPNotFound
		}
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.get_by_instance: %w", err)
	}
	return row, nil
}

// AttachFloatingIP attaches the floating IP to the named instance. The
// instance must be in the caller's tenant and must not already have a
// floating IP attached. The Incus forward push is best-effort: a failed
// push records forward_push_status="failed" but the attach itself
// succeeds (ADR-0037 sub-decision B).
func (s *Service) AttachFloatingIP(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	floatingIPID, instanceID uuid.UUID,
) (FloatingIPRow, error) {
	row, err := s.repos.FloatingIPs.Get(ctx, floatingIPID)
	if err != nil {
		if database.IsNoRows(err) {
			return FloatingIPRow{}, ErrFloatingIPNotFound
		}
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.attach: %w", err)
	}
	if row.InstanceID != nil {
		return FloatingIPRow{}, ErrFloatingIPAlreadyAttached
	}
	// Confirm the instance exists within the caller's tenant (the
	// repo's Get is tenant-scoped so a cross-tenant instance id
	// surfaces as ErrInstanceNotFound).
	inst, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return FloatingIPRow{}, ErrInstanceNotFound
		}
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.attach: lookup instance: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeFloatingIPAttach,
		ResourceType: audit.ResourceFloatingIP,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"address":     row.Address.String(),
			"instance_id": instanceID.String(),
		},
	})

	// Best-effort Incus forward push (ADR-0037 sub-decision B).
	// When no forward network is configured OR the provider is nil,
	// the attach still succeeds with forward_push_status="unsupported".
	pushStatus, networkName := "unsupported", (*string)(nil)
	if s.provider != nil && s.forwardNetwork != "" {
		project := s.provider.ProjectName(tenantID)
		// Empty ports list = forward every port. A future WS can plumb
		// per-attach port restrictions.
		if err := s.provider.CreateNetworkForward(ctx, project, s.forwardNetwork, row.Address.String(), nil); err != nil {
			pushStatus = "failed"
		} else {
			pushStatus = "pushed"
			network := s.forwardNetwork
			networkName = &network
		}
	}

	if err := s.repos.FloatingIPs.SetInstance(ctx, database.SetFloatingIPInstanceParams{
		ID:                row.ID,
		InstanceID:        &instanceID,
		ForwardPushStatus: pushStatus,
		NetworkName:       networkName,
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.attach: %w", err)
	}
	row.InstanceID = &instanceID
	row.ForwardPushStatus = pushStatus
	row.NetworkName = networkName

	// WASM event emit with trigger=attach so plugins can distinguish
	// from the allocate-time emit.
	s.emitIPEvent(ctx, eventbus.ComputeIPAssigned, tenantID, userID, row.ID, map[string]any{
		"trigger":           "attach",
		"address":           row.Address.String(),
		"instance_id":       instanceID.String(),
		"instance_name":     inst.Name,
		"forward_push":      pushStatus,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"forward_push_status": pushStatus,
	}})
	return row, nil
}

// DetachFloatingIP detaches the floating IP from its current instance.
// The IP stays allocated to the tenant (the "floating" state). The
// Incus forward (if any) is removed before the row is updated.
func (s *Service) DetachFloatingIP(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	floatingIPID uuid.UUID,
) (FloatingIPRow, error) {
	row, err := s.repos.FloatingIPs.Get(ctx, floatingIPID)
	if err != nil {
		if database.IsNoRows(err) {
			return FloatingIPRow{}, ErrFloatingIPNotFound
		}
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.detach: %w", err)
	}
	if row.InstanceID == nil {
		return FloatingIPRow{}, ErrFloatingIPNotAttached
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeFloatingIPDetach,
		ResourceType: audit.ResourceFloatingIP,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"address":     row.Address.String(),
			"instance_id": row.InstanceID.String(),
		},
	})

	// Best-effort Incus forward removal. A failed delete is logged but
	// does not block the detach (the operator's automation may have
	// already removed it, or the daemon may be unreachable).
	if s.provider != nil && row.NetworkName != nil && *row.NetworkName != "" {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.DeleteNetworkForward(ctx, project, *row.NetworkName, row.Address.String()); err != nil {
			// Log via audit details; do not fail the detach.
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
				"warning":          "forward_delete_failed",
				"forward_error":    err.Error(),
			}})
		}
	}

	if err := s.repos.FloatingIPs.SetInstance(ctx, database.SetFloatingIPInstanceParams{
		ID:                row.ID,
		InstanceID:        nil,
		ForwardPushStatus: "pending",
		NetworkName:       nil,
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.detach: %w", err)
	}
	row.InstanceID = nil
	row.NetworkName = nil
	row.ForwardPushStatus = "pending"

	s.emitIPEvent(ctx, eventbus.ComputeIPReleased, tenantID, userID, row.ID, map[string]any{
		"trigger": "detach",
		"address": row.Address.String(),
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// ReleaseFloatingIP releases the floating IP back to the pool. The IP
// becomes available for re-allocation. Any current attach is detached
// first; the PTR record (if any) is removed; per-IP-hour metering is
// stopped.
func (s *Service) ReleaseFloatingIP(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	floatingIPID uuid.UUID,
) error {
	row, err := s.repos.FloatingIPs.Get(ctx, floatingIPID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrFloatingIPNotFound
		}
		return fmt.Errorf("compute.floating_ip.release: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeFloatingIPRelease,
		ResourceType: audit.ResourceFloatingIP,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"address":     row.Address.String(),
			"instance_id": row.InstanceID,
		},
	})

	// Detach first if currently attached.
	if row.InstanceID != nil && s.provider != nil && row.NetworkName != nil && *row.NetworkName != "" {
		project := s.provider.ProjectName(tenantID)
		_ = s.provider.DeleteNetworkForward(ctx, project, *row.NetworkName, row.Address.String())
	}

	// PTR unpublish (best-effort).
	pool, errPool := s.lookupIPPool(ctx, row.PoolID)
	if errPool == nil && pool.PtrZoneID != nil && s.ptrPublisher != nil {
		_ = s.ptrPublisher.UnpublishPTR(ctx, *pool.PtrZoneID, row.Address)
	}

	// Stop per-IP-hour metering (best-effort).
	if s.meter != nil {
		_ = s.meter.StopIPUsage(ctx, tenantID, row.Address)
	}

	if err := s.repos.FloatingIPs.SoftDelete(ctx, floatingIPID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("compute.floating_ip.release: %w", err)
	}

	s.emitIPEvent(ctx, eventbus.ComputeIPReleased, tenantID, userID, row.ID, map[string]any{
		"trigger": "release",
		"address": row.Address.String(),
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// SetFloatingIPPTRTarget replaces the PTR target on the floating IP.
// The service publishes the new PTR into the pool's reverse zone (and
// removes the old one) when the publisher seam is wired. Empty target
// disables PTR publishing for this allocation.
func (s *Service) SetFloatingIPPTRTarget(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	floatingIPID uuid.UUID,
	ptrTarget string,
) (FloatingIPRow, error) {
	row, err := s.repos.FloatingIPs.Get(ctx, floatingIPID)
	if err != nil {
		if database.IsNoRows(err) {
			return FloatingIPRow{}, ErrFloatingIPNotFound
		}
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.set_ptr: %w", err)
	}

	var newTarget *string
	if strings.TrimSpace(ptrTarget) != "" {
		target := ensureTrailingDot(strings.TrimSpace(ptrTarget))
		if err := validateCanonicalDNSName(target); err != nil {
			return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.set_ptr: %w", err)
		}
		newTarget = &target
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       audit.ActionComputeFloatingIPSetPTR,
		ResourceType: audit.ResourceFloatingIP,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"address":     row.Address.String(),
			"ptr_target":  newTarget,
			"previous":    row.PtrTarget,
		},
	})

	// PTR publish (best-effort): unpublish the old target then publish
	// the new one. The pool's reverse zone is the target.
	if pool, errPool := s.lookupIPPool(ctx, row.PoolID); errPool == nil &&
		pool.PtrZoneID != nil && s.ptrPublisher != nil {
		if row.PtrTarget != nil {
			_ = s.ptrPublisher.UnpublishPTR(ctx, *pool.PtrZoneID, row.Address)
		}
		if newTarget != nil {
			_ = s.ptrPublisher.PublishPTR(ctx, *pool.PtrZoneID, row.Address, *newTarget)
		}
	}

	if err := s.repos.FloatingIPs.SetPTRTarget(ctx, floatingIPID, newTarget); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return FloatingIPRow{}, fmt.Errorf("compute.floating_ip.set_ptr: %w", err)
	}
	row.PtrTarget = newTarget

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// emitIPEvent is the best-effort WASM event-bus helper for the
// floating-IP surface. A nil bus or a failed emit does NOT propagate;
// the privileged action already happened.
func (s *Service) emitIPEvent(
	ctx context.Context,
	topic string,
	tenantID, userID, floatingIPID uuid.UUID,
	meta map[string]any,
) {
	s.emitEvent(ctx, topic, tenantID, userID, floatingIPID, meta)
}

// ensureTrailingDot returns name with a single trailing dot appended
// unless it already has one. Canonical DNS names end with a dot.
func ensureTrailingDot(name string) string {
	if name == "" {
		return name
	}
	if name[len(name)-1] == '.' {
		return name
	}
	return name + "."
}

// validateCanonicalDNSName enforces the minimum shape of a canonical
// DNS name (FQDN, lowercase, trailing dot, no double dots, length in
// the RFC-valid range). This is the same check the DNS module's record
// validator runs on PTR content; we duplicate it here so the compute
// module does not import the dns package transitively.
func validateCanonicalDNSName(name string) error {
	if name == "" {
		return errors.New("compute: PTR target is empty")
	}
	if len(name) > 253 {
		return fmt.Errorf("compute: PTR target %q is too long (max 253 chars)", name)
	}
	if name != ensureTrailingDot(strings.ToLower(name)) {
		return fmt.Errorf("compute: PTR target %q must be lowercase + end with a dot", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("compute: PTR target %q has empty labels", name)
	}
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if label == "" {
			return fmt.Errorf("compute: PTR target %q has empty labels", name)
		}
		if len(label) > 63 {
			return fmt.Errorf("compute: PTR target %q has a label longer than 63 chars", name)
		}
	}
	return nil
}

// incusProjectName returns the Incus project name for the tenant via
// the provider. Kept here to centralise the call.
func (s *Service) incusProjectName(tenantID uuid.UUID) string {
	if s.provider == nil {
		return ""
	}
	return s.provider.ProjectName(tenantID)
}

// ErrFloatingIPAlreadyAllocated is the sentinel for the race-condition
// path where two concurrent allocations both picked the same address.
// Distinct from ErrFloatingIPAlreadyAttached (which is about an instance
// already having a floating IP). The handler maps both to 409 conflict.
var ErrFloatingIPAlreadyAllocated = errors.New("compute: floating ip address already allocated (retry)")
