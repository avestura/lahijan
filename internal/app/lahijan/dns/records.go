// Package dns: records.go implements the record CRUD (create / get / list
// / update / delete) + per-action audit + event emission. The orchestration
// order mirrors zones.go:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary).
//  2. Validation (canonical name, supported type, content-shape per type,
//     TTL in [300, 86400], CNAME-not-at-apex).
//  3. Audit emit (status=pending).
//  4. PDNS RRset REPLACE/DELETE.
//  5. dns_records row insert/update/delete.
//  6. Event bus emit.
//  7. Audit mark-outcome.
//
// CNAME-at-apex is rejected per WS-15 "Open questions" item 2 (strict RFC).
// CNAME-to-apex (i.e. apex CNAME — same thing) is also rejected for the
// same reason.
package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// RecordCreateParams carries the user-controlled fields of a create-record
// call. Name must be a canonical DNS name (lowercase, trailing dot) and
// must belong to the zone (apex or sub-domain).
type RecordCreateParams struct {
	// Name is the canonical record name (e.g. "www.example.com.").
	Name string
	// Type is the DNS record type (A, AAAA, CNAME, MX, TXT, NS, SOA,
	// SRV, CAA, PTR).
	Type string
	// Content is the zone-file wire form (e.g. "192.0.2.1" for A).
	Content string
	// TTL is the time-to-live in seconds. Defaults to the service's
	// configured default when zero.
	TTL int
	// Disabled marks the record as inactive. PDNS still serves the rest
	// of the set.
	Disabled bool
}

// CreateRecord orchestrates a record create: validate -> audit pending ->
// PDNS REPLACE -> dns_records row insert -> event emit -> audit outcome.
// Returns the cached dns_records row.
//
// Records are unique per (zone, name, type, content). A duplicate surfaces
// as ErrRecordAlreadyExists (409).
func (s *Service) CreateRecord(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
	params RecordCreateParams,
) (database.DnsRecord, error) {
	if s.provider == nil {
		return database.DnsRecord{}, ErrProviderDisabled
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return database.DnsRecord{}, err
	}

	// 1) Validate name + type + content + TTL.
	if err := validateRecordName(params.Name, zone.CanonicalID); err != nil {
		return database.DnsRecord{}, err
	}
	if !IsSupportedRecordType(params.Type) {
		return database.DnsRecord{}, fmt.Errorf("%w: %q", ErrInvalidRecordType, params.Type)
	}
	if params.Type == TypeCNAME && params.Name == zone.CanonicalID {
		return database.DnsRecord{}, ErrCNAMEAtApex
	}
	if err := ValidateRecordContent(params.Type, params.Content, zone.CanonicalID); err != nil {
		return database.DnsRecord{}, err
	}
	ttl := params.TTL
	if ttl == 0 {
		ttl = s.config.DefaultTTL
	}
	if err := ValidateTTL(ttl); err != nil {
		return database.DnsRecord{}, err
	}

	// 2) Quota check (per-tenant record cap). Zero means unlimited.
	if s.config.MaxRecordsPerTenant > 0 {
		current, errCount := s.repos.DNSRecords.CountForTenant(ctx)
		if errCount != nil {
			return database.DnsRecord{}, fmt.Errorf("dns: count records for quota: %w", errCount)
		}
		if int(current) >= s.config.MaxRecordsPerTenant {
			return database.DnsRecord{}, fmt.Errorf("dns: per-tenant record cap reached (%d)", s.config.MaxRecordsPerTenant)
		}
	}

	// 3) Short-circuit duplicate (zone, name, type, content).
	if existing, errLookup := s.repos.DNSRecords.GetByIdentity(ctx, zoneID, params.Name, params.Type, params.Content); errLookup == nil && existing.ID != uuid.Nil {
		return database.DnsRecord{}, fmt.Errorf("%w: %s %s %s", ErrRecordAlreadyExists, params.Name, params.Type, params.Content)
	}

	prio := recordPriority(params.Type, params.Content)

	// 4) Audit emit (status=pending).
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditRecordCreate,
		ResourceType: ResourceRecord,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"zone_id":  zoneID.String(),
			"name":     params.Name,
			"type":     params.Type,
			"ttl":      ttl,
			"priority": prio,
		},
	})

	// 5) PDNS REPLACE.
	if err := s.provider.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID: zone.CanonicalID,
		Name:   params.Name,
		Type:   powerdns.RecordType(params.Type),
		TTL:    ttl,
		Records: []powerdns.Record{{
			Content:  params.Content,
			Disabled: params.Disabled,
		}},
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.DnsRecord{}, fmt.Errorf("dns: pdns replace rrset: %w", err)
	}

	// 6) dns_records row insert. Content is canonicalised for IPs so two
	// equivalent representations collapse to one row.
	content := canonicalisedContent(params.Type, params.Content)
	row, err := s.repos.DNSRecords.Create(ctx, database.CreateDNSRecordParams{
		ZoneID:   zoneID,
		Name:     params.Name,
		Type:     params.Type,
		Content:  content,
		TTL:      int32(ttl),
		Prio:     prio,
		Disabled: params.Disabled,
	})
	if err != nil {
		// Row insert failed (typically unique violation if a parallel
		// caller raced us). The PDNS REPLACE already succeeded; the
		// reconciliation worker (TODO: future WS) will pick up the
		// divergence. For now we report the failure.
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.DnsRecord{}, fmt.Errorf("dns: create record row: %w", err)
	}

	// 7) Event bus emit (dns.record.created).
	s.emitEvent(ctx, eventbus.DNSRecordCreated, tenantID, userID, row.ID, map[string]any{
		"zone_id": zoneID.String(),
		"name":    row.Name,
		"type":    row.Type,
		"ttl":     row.Ttl,
	})

	// 8) Audit mark-outcome (success).
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"record_id": row.ID,
	}})

	return row, nil
}

// GetRecord returns the cached record row.
func (s *Service) GetRecord(
	ctx context.Context,
	_ uuid.UUID,
	zoneID, recordID uuid.UUID,
) (database.DnsRecord, error) {
	row, err := s.repos.DNSRecords.Get(ctx, recordID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DnsRecord{}, ErrRecordNotFound
		}
		return database.DnsRecord{}, fmt.Errorf("dns: get record: %w", err)
	}
	if row.ZoneID != zoneID {
		// Cross-zone lookup within the same tenant — treat as not found.
		return database.DnsRecord{}, ErrRecordNotFound
	}
	return row, nil
}

// ListRecords returns a paginated list of the records in the zone.
func (s *Service) ListRecords(
	ctx context.Context,
	_ uuid.UUID,
	zoneID uuid.UUID,
	limit, offset int32,
) ([]database.DnsRecord, error) {
	rows, err := s.repos.DNSRecords.List(ctx, zoneID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("dns: list records: %w", err)
	}
	return rows, nil
}

// CountRecords returns the number of records in the zone.
func (s *Service) CountRecords(
	ctx context.Context,
	_ uuid.UUID,
	zoneID uuid.UUID,
) (int64, error) {
	return s.repos.DNSRecords.CountInZone(ctx, zoneID)
}

// RecordUpdateParams carries the mutable fields of a record. Name and
// Type are immutable — callers wanting a "rename" issue a delete + create.
type RecordUpdateParams struct {
	// Content is the new zone-file wire form. When nil the content is
	// left unchanged.
	Content *string
	// TTL is the new TTL. When nil the TTL is left unchanged.
	TTL *int
	// Disabled flips the record's disabled flag.
	Disabled *bool
}

// Update replaces content / TTL / disabled for the record. The PDNS side
// is updated via a REPLACE so the live RRset matches.
func (s *Service) UpdateRecord(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID, recordID uuid.UUID,
	params RecordUpdateParams,
) (database.DnsRecord, error) {
	if s.provider == nil {
		return database.DnsRecord{}, ErrProviderDisabled
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return database.DnsRecord{}, err
	}
	row, err := s.repos.DNSRecords.Get(ctx, recordID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DnsRecord{}, ErrRecordNotFound
		}
		return database.DnsRecord{}, fmt.Errorf("dns: get record: %w", err)
	}
	if row.ZoneID != zoneID {
		return database.DnsRecord{}, ErrRecordNotFound
	}

	// Build the merged shape so we can validate the post-update content.
	newContent := row.Content
	if params.Content != nil {
		newContent = *params.Content
	}
	if err := ValidateRecordContent(row.Type, newContent, zone.CanonicalID); err != nil {
		return database.DnsRecord{}, err
	}
	newContent = canonicalisedContent(row.Type, newContent)

	newTTL := int(row.Ttl)
	if params.TTL != nil {
		newTTL = *params.TTL
	}
	if err := ValidateTTL(newTTL); err != nil {
		return database.DnsRecord{}, err
	}
	newDisabled := row.Disabled
	if params.Disabled != nil {
		newDisabled = *params.Disabled
	}
	prio := recordPriority(row.Type, newContent)

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditRecordUpdate,
		ResourceType: ResourceRecord,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})

	// PDNS REPLACE.
	if err := s.provider.ReplaceRRset(ctx, powerdns.RRsetUpsertParams{
		ZoneID: zone.CanonicalID,
		Name:   row.Name,
		Type:   powerdns.RecordType(row.Type),
		TTL:    newTTL,
		Records: []powerdns.Record{{
			Content:  newContent,
			Disabled: newDisabled,
		}},
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.DnsRecord{}, fmt.Errorf("dns: pdns replace rrset: %w", err)
	}

	// Row update.
	if err := s.repos.DNSRecords.Update(ctx, recordID, database.UpdateDNSRecordParams{
		Content:  newContent,
		TTL:      int32(newTTL),
		Prio:     prio,
		Disabled: newDisabled,
	}); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.DnsRecord{}, fmt.Errorf("dns: update record row: %w", err)
	}
	row.Content = newContent
	row.Ttl = int32(newTTL)
	row.Prio = prio
	row.Disabled = newDisabled

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSRecordUpdated, tenantID, userID, row.ID, map[string]any{
		"zone_id": zoneID.String(),
		"name":    row.Name,
		"type":    row.Type,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// DeleteRecord orchestrates a record delete: PDNS DELETE -> dns_records
// row delete -> event emit -> audit outcome.
func (s *Service) DeleteRecord(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID, recordID uuid.UUID,
) error {
	if s.provider == nil {
		return ErrProviderDisabled
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return err
	}
	row, err := s.repos.DNSRecords.Get(ctx, recordID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrRecordNotFound
		}
		return fmt.Errorf("dns: get record: %w", err)
	}
	if row.ZoneID != zoneID {
		return ErrRecordNotFound
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditRecordDelete,
		ResourceType: ResourceRecord,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})

	// PDNS DELETE. Tolerate 404 — the daemon's view may have drifted.
	if err := s.provider.DeleteRRset(ctx, powerdns.DeleteRRsetParams{
		ZoneID: zone.CanonicalID,
		Name:   row.Name,
		Type:   powerdns.RecordType(row.Type),
	}); err != nil && !errors.Is(err, powerdns.ErrNotFound) {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: pdns delete rrset: %w", err)
	}

	if err := s.repos.DNSRecords.Delete(ctx, recordID); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return fmt.Errorf("dns: delete record row: %w", err)
	}

	// Event bus emit.
	s.emitEvent(ctx, eventbus.DNSRecordDeleted, tenantID, userID, row.ID, map[string]any{
		"zone_id": zoneID.String(),
		"name":    row.Name,
		"type":    row.Type,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}

// lookupZoneForCaller resolves the zone row within the tenant in ctx.
// Returns ErrZoneNotFound when the zone does not exist OR is owned by
// another tenant (tenant scoping is enforced at the repository seam).
func (s *Service) lookupZoneForCaller(ctx context.Context, zoneID uuid.UUID) (database.DNSZone, error) {
	zone, err := s.repos.DNSZones.Get(ctx, zoneID)
	if err != nil {
		if database.IsNoRows(err) {
			return database.DNSZone{}, ErrZoneNotFound
		}
		return database.DNSZone{}, fmt.Errorf("dns: get zone: %w", err)
	}
	return zone, nil
}

// validateRecordName enforces the record name is a canonical DNS name and
// belongs to the zone (apex match or ".<zone-id>" suffix).
func validateRecordName(name, zoneID string) error {
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidRecordName)
	}
	if !strings.HasSuffix(name, ".") {
		return fmt.Errorf("%w: name %q must end with a dot", ErrInvalidRecordName, name)
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("%w: name %q must not contain whitespace", ErrInvalidRecordName, name)
	}
	if strings.ToLower(name) != name {
		return fmt.Errorf("%w: name %q must be lowercase", ErrInvalidRecordName, name)
	}
	// Allow exact apex match (SOA / NS at "example.com.") + sub-domain.
	if name == zoneID {
		return nil
	}
	if !strings.HasSuffix(name, "."+zoneID) {
		return fmt.Errorf("%w: name %q does not belong to zone %q", ErrInvalidRecordName, name, zoneID)
	}
	return nil
}

// recordPriority extracts the priority for MX / SRV records from the
// content; 0 for types that have no priority.
func recordPriority(recordType, content string) int32 {
	switch recordType {
	case TypeMX:
		return ParseMXPriority(content)
	case TypeSRV:
		return ParseSRVPriority(content)
	}
	return 0
}

// canonicalisedContent returns the canonical form of the content for
// storage. For IPs both A and AAAA records are normalised so two
// equivalent representations collapse to one row.
func canonicalisedContent(recordType, content string) string {
	switch recordType {
	case TypeA, TypeAAAA:
		return CanonicalizeIP(content)
	}
	return content
}

// emitEvent is the best-effort event-bus helper. A nil bus or a failed
// emit does NOT propagate; the privileged action already happened.
func (s *Service) emitEvent(
	ctx context.Context,
	topic string,
	tenantID, userID, resourceID uuid.UUID,
	meta map[string]any,
) {
	if s.bus == nil {
		return
	}
	var raw []byte
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = b
		}
	}
	tid := tenantID
	uid := userID
	rid := resourceID
	_ = s.bus.Emit(ctx, eventbus.Event{
		Topic:      topic,
		TenantID:   &tid,
		ActorType:  audit.ActorUser,
		ActorID:    &uid,
		ResourceID: &rid,
		Metadata:   raw,
	})
}
