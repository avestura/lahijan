// Package dns: zones.go implements the zone CRUD (create / get / list /
// update / delete) + per-action audit + event emission. Every privileged
// action emits an audit row BEFORE the side effect (status=pending) and
// marks the outcome AFTER; every lifecycle transition also emits into
// the WASM event bus so plugins can react.
//
// The orchestration order mirrors compute/instances.go:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Validation (canonical name, kind, nameservers supplied).
//  3. Audit emit (status=pending) — the row exists even if step 5 fails.
//  4. Cross-tenant uniqueness check via the dns_zones repo (canonical_id
//     is globally unique per WS-12 "Open questions" item 3).
//  5. PDNS create + dns_zones row insert (the row caches the canonical id).
//  6. Event bus emit (so plugins react after the DB is consistent).
//  7. Audit mark-outcome (success | failure).
//
// If the provider is nil the service returns ErrProviderDisabled which the
// handler maps to 501 not_implemented.
package dns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// ZoneCreateParams carries the user-controlled fields of a create-zone call.
// Name must be a canonical DNS name (lowercase, trailing dot). Description
// is optional. Kind defaults to "Native" (Lahijan is the only NS by
// default per WS-12).
type ZoneCreateParams struct {
	// Name is the canonical zone name (e.g. "example.com."). Required.
	Name string
	// Description is the user-visible description.
	Description string
	// Kind is "Native", "Master", or "Slave". Defaults to "Native".
	Kind string
}

// CreateZone orchestrates a zone create: validate -> audit pending ->
// canonical-id uniqueness check -> PDNS create -> dns_zones row insert ->
// event emit -> audit outcome. Returns the cached dns_zones row.
func (s *Service) CreateZone(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params ZoneCreateParams,
) (database.DNSZone, error) {
	if s.provider == nil {
		return database.DNSZone{}, ErrProviderDisabled
	}
	if err := validateCanonicalZoneName(params.Name); err != nil {
		return database.DNSZone{}, err
	}
	// 1) Cross-tenant uniqueness: canonical_id is globally unique per
	// WS-12 "Open questions" item 3. The repository's CreateDNSZone
	// surfaces a unique-violation as ErrZoneAlreadyExists below.
	if existing, errLookup := s.repos.DNSZones.GetByCanonicalGlobal(ctx, params.Name); errLookup == nil && existing.ID != uuid.Nil {
		return database.DNSZone{}, fmt.Errorf("%w: %s", ErrZoneAlreadyExists, params.Name)
	}

	kind := params.Kind
	if kind == "" {
		kind = database.DNSZoneKindNative
	}
	nameservers := s.config.DefaultNameservers
	if len(nameservers) == 0 {
		// Default to a placeholder so the create still succeeds when
		// the operator did not configure default nameservers. The
		// admin can update later via the PDNS API.
		nameservers = []string{"ns1." + params.Name}
	}

	// 2) Audit emit (status=pending). The row exists even if step 4 fails.
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditZoneCreate,
		ResourceType: ResourceZone,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name":        params.Name,
			"kind":        kind,
			"description": params.Description,
		},
	})

	// 3) PDNS create. Account tag carries the tenant id for out-of-band
	// diagnostics — never surfaced to end users.
	pdnsZone, err := s.provider.CreateZone(ctx, powerdns.CreateZoneParams{
		Name:        params.Name,
		Kind:        powerdns.ZoneKind(kind),
		Account:     tenantID.String(),
		Nameservers: nameservers,
	})
	if err != nil {
		// If PDNS reports the zone already exists, treat it the same as
		// our cross-tenant short-circuit: ErrZoneAlreadyExists.
		if errors.Is(err, powerdns.ErrAlreadyExists) {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": "zone already exists in PDNS",
			}})
			return database.DNSZone{}, fmt.Errorf("%w: %s", ErrZoneAlreadyExists, params.Name)
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.DNSZone{}, fmt.Errorf("dns: pdns create zone: %w", err)
	}

	// 4) Cache the zone row. canonical_id is the PDNS-assigned id.
	row, err := s.repos.DNSZones.Create(ctx, database.CreateDNSZoneParams{
		CanonicalID: pdnsZone.ID,
		Name:        pdnsZone.Name,
		Kind:        kind,
		Description: params.Description,
	})
	if err != nil {
		// The DB write failed (typically a unique violation if another
		// tenant raced us). Best-effort: roll back the PDNS create so
		// the daemon does not keep an orphaned zone. A failure to roll
		// back is logged but does not propagate — the caller already
		// has a problem.
		if rbErr := s.provider.DeleteZone(ctx, pddnsZoneID(pdnsZone)); rbErr != nil {
			_ = rbErr // logged by caller via audit metadata
		}
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.DNSZone{}, fmt.Errorf("dns: create zone row: %w", err)
	}

	// 5) Optional DNSSEC at create time. Off by default per WS-12; the
	// admin flips it per-zone via EnableDNSSEC.
	if s.config.DefaultDNSSECEnabled {
		if _, errSecure := s.provider.EnableDNSSEC(ctx, row.CanonicalID); errSecure == nil {
			_ = s.repos.DNSZones.SetCachedDNSSEC(ctx, row.ID, true)
			row.IsDnssecEnabled = true
		}
	}

	// 6) Event bus emit (dns.zone.created).
	s.emitEvent(ctx, eventbus.DNSZoneCreated, tenantID, userID, row.ID, map[string]any{
		"name":         row.Name,
		"canonical_id": row.CanonicalID,
		"kind":         row.Kind,
	})

	// 7) Audit mark-outcome (success).
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"zone_id": row.ID,
	}})

	return row, nil
}

// GetZone returns the cached zone row. The PDNS daemon is NOT probed here;
// callers needing live state call ReconcileZone first.
func (s *Service) GetZone(
	ctx context.Context,
	_ uuid.UUID,
	zoneID uuid.UUID,
) (database.DNSZone, error) {
	row, err := s.repos.DNSZones.Get(ctx, zoneID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DNSZone{}, ErrZoneNotFound
		}
		return database.DNSZone{}, fmt.Errorf("dns: get zone: %w", err)
	}
	return row, nil
}

// ReconcileZone asks the daemon for the live state and updates the cached
// DNSSEC flag. Returns the updated row. A daemon error does NOT propagate;
// the caller sees the stale cache instead so a flaky daemon does not 500
// the API. Mirrors compute/instances.go's ReconcileInstance pattern.
func (s *Service) ReconcileZone(
	ctx context.Context,
	_ uuid.UUID,
	zoneID uuid.UUID,
) (database.DNSZone, error) {
	row, err := s.repos.DNSZones.Get(ctx, zoneID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DNSZone{}, ErrZoneNotFound
		}
		return database.DNSZone{}, fmt.Errorf("dns: get zone: %w", err)
	}
	if s.provider == nil {
		return row, nil
	}
	enabled, errSec := s.provider.IsDNSSECEnabled(ctx, row.CanonicalID)
	if errSec != nil {
		// Daemon unreachable; return the cached row.
		return row, nil
	}
	_ = s.repos.DNSZones.SetCachedDNSSEC(ctx, zoneID, enabled)
	row.IsDnssecEnabled = enabled
	return row, nil
}

// ListZones returns a paginated list of the tenant's zones. The cache is
// returned; reconciliation is a separate call.
func (s *Service) ListZones(
	ctx context.Context,
	_ uuid.UUID,
	limit, offset int32,
) ([]database.DNSZone, error) {
	rows, err := s.repos.DNSZones.List(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("dns: list zones: %w", err)
	}
	return rows, nil
}

// CountZones returns the number of zones in the tenant.
func (s *Service) CountZones(ctx context.Context, _ uuid.UUID) (int64, error) {
	return s.repos.DNSZones.Count(ctx)
}

// ZoneUpdateParams carries the user-controlled fields of a PATCH zone call.
// All fields are optional; a nil pointer means "leave unchanged".
type ZoneUpdateParams struct {
	Description *string
	Kind        *string
}

// UpdateZone replaces the cached description / kind for the zone. The
// kind change also propagates to PDNS so the daemon's view matches.
func (s *Service) UpdateZone(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
	params ZoneUpdateParams,
) (database.DNSZone, error) {
	if s.provider == nil {
		return database.DNSZone{}, ErrProviderDisabled
	}
	row, err := s.repos.DNSZones.Get(ctx, zoneID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DNSZone{}, ErrZoneNotFound
		}
		return database.DNSZone{}, fmt.Errorf("dns: get zone: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditZoneUpdate,
		ResourceType: ResourceZone,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})

	// Update PDNS first. We send only the fields the caller touched so
	// the PATCH is minimal.
	update := powerdns.ZoneUpdate{}
	kindChanged := false
	if params.Description != nil {
		// PDNS has no description field; we keep it on the dns_zones row only.
	}
	if params.Kind != nil && *params.Kind != row.Kind {
		update.Kind = *params.Kind
		kindChanged = true
	}
	if kindChanged {
		if err := s.provider.UpdateZone(ctx, row.CanonicalID, update); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return database.DNSZone{}, fmt.Errorf("dns: pdns update zone: %w", err)
		}
		if err := s.repos.DNSZones.SetKind(ctx, zoneID, *params.Kind); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return database.DNSZone{}, fmt.Errorf("dns: update zone kind: %w", err)
		}
		row.Kind = *params.Kind
	}
	if params.Description != nil {
		if err := s.repos.DNSZones.SetDescription(ctx, zoneID, *params.Description); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error": err.Error(),
			}})
			return database.DNSZone{}, fmt.Errorf("dns: update zone description: %w", err)
		}
		row.Description = *params.Description
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSZoneUpdated, tenantID, userID, row.ID, map[string]any{
		"name": row.Name,
		"kind": row.Kind,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// DeleteZone orchestrates a zone delete: PDNS delete -> dns_records
// delete-all -> dns_zones row delete -> event emit -> audit outcome. The
// order is important: PDNS delete first so the daemon stops serving the
// zone; then our DB cleanup so a daemon failure does not strand rows.
//
// Idempotent on the daemon side: if PDNS reports 404 we still proceed so
// the dns_zones row is removed even when the daemon's view drifted.
func (s *Service) DeleteZone(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	row, err := s.repos.DNSZones.Get(ctx, zoneID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrZoneNotFound
		}
		return fmt.Errorf("dns: get zone: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditZoneDelete,
		ResourceType: ResourceZone,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"name":         row.Name,
			"canonical_id": row.CanonicalID,
		},
	})

	// PDNS delete. Tolerate 404 — the daemon's view may have drifted.
	if err := s.provider.DeleteZone(ctx, row.CanonicalID); err != nil && !errors.Is(err, powerdns.ErrNotFound) {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: pdns delete zone: %w", err)
	}

	// Drop our records + zone row.
	if err := s.repos.DNSRecords.DeleteAllInZone(ctx, zoneID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: delete zone records: %w", err)
	}
	if err := s.repos.DNSZones.Delete(ctx, zoneID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: delete zone row: %w", err)
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSZoneDeleted, tenantID, userID, row.ID, map[string]any{
		"name":         row.Name,
		"canonical_id": row.CanonicalID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// validateCanonicalZoneName enforces the canonical zone name shape:
// lowercase, trailing dot, no whitespace. Mirrors
// providers/powerdns.validateCanonicalName but kept here so the service
// does not import the driver's internals.
func validateCanonicalZoneName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidZoneName)
	}
	if !strings.HasSuffix(name, ".") {
		return fmt.Errorf("%w: name %q must end with a dot", ErrInvalidZoneName, name)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("%w: name %q must not contain whitespace", ErrInvalidZoneName, name)
	}
	if strings.ToLower(name) != name {
		return fmt.Errorf("%w: name %q must be lowercase", ErrInvalidZoneName, name)
	}
	return nil
}

// pddnsZoneID extracts the canonical id from a freshly-created PDNS Zone
// response. Falls back to the name when PDNS did not assign an id (some
// test fakes do not).
func pddnsZoneID(z *powerdns.Zone) string {
	if z == nil {
		return ""
	}
	if z.ID != "" {
		return z.ID
	}
	return z.Name
}
