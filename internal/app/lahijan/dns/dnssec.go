// Package dns: dnssec.go implements the per-zone DNSSEC toggle + key
// rotation. The orchestration order mirrors zones.go / records.go:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Audit emit (status=pending).
//  3. PDNS enable/disable/rotate.
//  4. dns_zones cached flag update.
//  5. Event bus emit.
//  6. Audit mark-outcome.
//
// Per WS-12 "Open questions" item 1 DNSSEC is off by default for new zones;
// the admin opts in per-zone via EnableDNSSEC. DisableDNSSEC removes every
// cryptokey (per the PDNS driver's policy).
package dns

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// EnableDNSSEC turns DNSSEC on for the zone by asking PDNS to create the
// combined signing key. Idempotent: if the zone already has an active
// signing key, PDNS returns it and the cached flag is flipped without
// re-issuing. Returns the (possibly newly-created) cryptokey on success.
func (s *Service) EnableDNSSEC(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return err
	}
	if zone.IsDnssecEnabled {
		// Already enabled; no-op so a double-click is safe.
		return nil
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditDNSSECEnable,
		ResourceType: ResourceZone,
		ResourceID:   &zone.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"canonical_id": zone.CanonicalID,
		},
	})

	if _, err := s.provider.EnableDNSSEC(ctx, zone.CanonicalID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: pdns enable dnssec: %w", err)
	}

	if err := s.repos.DNSZones.SetCachedDNSSEC(ctx, zoneID, true); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: cache dnssec flag: %w", err)
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSZoneDNSECSecured, tenantID, userID, zone.ID, map[string]any{
		"name":         zone.Name,
		"canonical_id": zone.CanonicalID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// DisableDNSSEC turns DNSSEC off for the zone by asking PDNS to remove
// every cryptokey. Idempotent: if the zone has no cryptokeys, the call
// succeeds without side effect.
func (s *Service) DisableDNSSEC(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return err
	}
	if !zone.IsDnssecEnabled {
		// Already disabled; no-op so a double-click is safe.
		return nil
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditDNSSECDisable,
		ResourceType: ResourceZone,
		ResourceID:   &zone.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"canonical_id": zone.CanonicalID,
		},
	})

	if err := s.provider.DisableDNSSEC(ctx, zone.CanonicalID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: pdns disable dnssec: %w", err)
	}

	if err := s.repos.DNSZones.SetCachedDNSSEC(ctx, zoneID, false); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: cache dnssec flag: %w", err)
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSZoneDNSSECDisabled, tenantID, userID, zone.ID, map[string]any{
		"name":         zone.Name,
		"canonical_id": zone.CanonicalID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// RotateDNSSECKey rotates the zone's signing key by disabling the current
// key, creating a new one, and (eventually) removing the old one. The
// "remove old" step is left to the operator via the admin UI for now;
// PDNS' recommended rollover flow waits for the DNSKEY TTL to expire
// before the old key can be safely removed.
//
// Per the WS-15 doc this is in scope but the Phase-7 candidate (full
// automated rollover) is deferred. The audit + event emission here is
// the canonical record for the rotation.
func (s *Service) RotateDNSSECKey(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return err
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditDNSSECEnable + ".rotate", // dns.zone.dnssec.enable.rotate
		ResourceType: ResourceZone,
		ResourceID:   &zone.ID,
		Status:       audit.StatusPending,
	})

	// Fetch the current key so we know what to (eventually) retire.
	keys, err := s.provider.ListCryptoKeys(ctx, zone.CanonicalID)
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: pdns list cryptokeys: %w", err)
	}

	// Create the new key. PDNS will start signing with it once the
	// previous key is deactivated; the operator decides when to retire
	// the previous key based on the DNSKEY TTL.
	if _, err := s.provider.EnableDNSSEC(ctx, zone.CanonicalID); err != nil {
		// EnableDNSSEC is idempotent; if it errors here the zone is in
		// a wedged state and the operator needs to investigate.
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: pdns rotate cryptokey: %w", err)
	}

	// Deactivate the previous keys so PDNS stops signing with them.
	// We do NOT delete them yet — that's the operator's call once the
	// DNSKEY TTL has expired.
	for i := range keys {
		k := keys[i]
		if !k.Active {
			continue
		}
		// Best-effort: deactivate; do not fail the rotation if a single
		// key cannot be deactivated (PDNS may have already retired it).
		_ = s.deactivateCryptoKey(ctx, zone.CanonicalID, k.ID)
	}

	// Cache stays enabled.
	if !zone.IsDnssecEnabled {
		_ = s.repos.DNSZones.SetCachedDNSSEC(ctx, zoneID, true)
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSZoneDNSSECRotated, tenantID, userID, zone.ID, map[string]any{
		"name":          zone.Name,
		"canonical_id":  zone.CanonicalID,
		"previous_keys": len(keys),
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// deactivateCryptoKey wraps a single PDNS ToggleCryptoKey call so the
// rotate path can iterate without dragging each error into the audit row.
func (s *Service) deactivateCryptoKey(ctx context.Context, canonicalID string, keyID int64) error {
	// We reach into the provider directly. The narrow DNSSEC interface
	// does not export ToggleCryptoKey because the only callers are the
	// DNSSEC rotation helper and the lifecycle paths; both go through
	// the provider's higher-level Enable/Disable. Toggle is exposed via
	// a type assertion so the production provider satisfies the seam
	// without dragging the toggle method into every test fake.
	toggler, ok := s.provider.(interface {
		ToggleCryptoKey(ctx context.Context, zoneID string, keyID int64, active bool) error
	})
	if !ok {
		// The provider does not expose ToggleCryptoKey (test fake); skip.
		return nil
	}
	return toggler.ToggleCryptoKey(ctx, canonicalID, keyID, false)
}

// IsDNSSECEnabled returns the cached DNSSEC flag. The flag is reconciled
// on every GetZone + ReconcileZone so a stale cache is rare; for the
// authoritative answer callers should call ReconcileZone first.
func (s *Service) IsDNSSECEnabled(
	ctx context.Context,
	_ uuid.UUID,
	zoneID uuid.UUID,
) (bool, error) {
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return false, err
	}
	return zone.IsDnssecEnabled, nil
}
