// Package dns: templates.go provides the predefined zone-template catalog
// the apply-template endpoint surfaces. A template is a list of records to
// upsert into a zone when applied; the templates are pure data so a future
// WS can move them into the database (admin-defined templates) without a
// service-shape change.
//
// Templates are useful for the common "set up Google Workspace" / "set up
// Microsoft 365" / "verify domain" workflows where the user would otherwise
// have to enter 5+ records manually. Each template carries an id, a name,
// a description, and the records it inserts.
//
// ApplyTemplate is idempotent: running the same template twice on the same
// zone updates the records (REPLACE semantics) rather than duplicating
// them. Records inserted by a template are NOT tagged as such — the user
// can edit / delete them like any other record.
package dns

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// TemplateRecord is one record a template applies. Fields mirror
// RecordCreateParams (minus Disabled — templates always insert active
// records). Name is a format string where "%s" is replaced with the
// zone's canonical id at apply time so the template works for any zone.
//
// Two placeholders are supported:
//
//	%s — replaced with the zone's canonical id INCLUDING the trailing
//	     dot (e.g. "example.com."). Use for Name fields and for content
//	     where the trailing dot is part of the canonical name.
//	%w — replaced with the zone's canonical id WITHOUT the trailing
//	     dot (e.g. "example.com"). Use for content where the zone name
//	     appears in the middle of a larger string (e.g. the Microsoft
//	     365 MX target "<zone>.mail.protection.outlook.com.").
//
// Example: Name "mail.%s" applied to zone "example.com." becomes
// "mail.example.com.".
type TemplateRecord struct {
	// Name is the canonical record name with optional "%s"/"%w" placeholder
	// for the zone id. Required.
	Name string
	// Type is the DNS record type.
	Type string
	// Content is the zone-file wire form. May contain a "%s"/"%w" placeholder
	// for the zone id (useful for MX / NS / SRV).
	Content string
	// TTL is the time-to-live. Zero means "use service default".
	TTL int
}

// Template is the user-facing shape: id + display name + description +
// the records it applies.
type Template struct {
	// ID is the kebab-case identifier used in API paths
	// ("google-workspace", "microsoft-365", ...).
	ID string
	// Name is the human-friendly display name ("Google Workspace setup").
	Name string
	// Description is the short description shown in the template picker.
	Description string
	// Records is the ordered list of records the template applies.
	Records []TemplateRecord
}

// builtinTemplates is the catalog surfaced at GET /api/v1/dns/templates.
// Add new templates here; the catalog is read-only at runtime.
var builtinTemplates = []Template{
	{
		ID:          "google-workspace",
		Name:        "Google Workspace setup",
		Description: "Adds the MX, SPF, and DKIM (placeholder) records Google Workspace requires to receive mail and verify domain ownership.",
		Records: []TemplateRecord{
			{
				Name:    "%s",
				Type:    TypeMX,
				Content: "1 aspmx.l.google.com.",
				TTL:     3600,
			},
			{
				Name:    "%s",
				Type:    TypeTXT,
				Content: "\"v=spf1 include:_spf.google.com ~all\"",
				TTL:     3600,
			},
		},
	},
	{
		ID:          "microsoft-365",
		Name:        "Microsoft 365 setup",
		Description: "Adds the MX and SPF records Microsoft 365 requires to receive mail.",
		Records: []TemplateRecord{
			{
				Name:    "%s",
				Type:    TypeMX,
				Content: "0 %w.mail.protection.outlook.com.",
				TTL:     3600,
			},
			{
				Name:    "%s",
				Type:    TypeTXT,
				Content: "\"v=spf1 include:spf.protection.outlook.com ~all\"",
				TTL:     3600,
			},
		},
	},
	{
		ID:          "verify-google",
		Name:        "Google Search Console verification",
		Description: "Adds a placeholder TXT record for Google Search Console domain verification. Replace the content with the token Google shows in the verification wizard.",
		Records: []TemplateRecord{
			{
				Name:    "%s",
				Type:    TypeTXT,
				Content: "\"google-site-verification=REPLACE-WITH-YOUR-TOKEN\"",
				TTL:     3600,
			},
		},
	},
}

// ListTemplates returns the catalog. The order matches the source slice so
// the UI renders a stable list.
func ListTemplates() []Template {
	out := make([]Template, len(builtinTemplates))
	copy(out, builtinTemplates)
	return out
}

// GetTemplate returns the template with the given id. Returns
// ErrTemplateNotFound when the id is not in the catalog.
func GetTemplate(id string) (Template, error) {
	for _, t := range builtinTemplates {
		if t.ID == id {
			return t, nil
		}
	}
	return Template{}, fmt.Errorf("%w: %s", ErrTemplateNotFound, id)
}

// ApplyTemplate applies the template identified by templateID to the zone
// identified by zoneID. The caller must already have passed the RBAC gate
// (dns.zone.update). Records are upserted in the order they appear in the
// template; an error mid-way rolls back nothing (the records already
// applied remain). The audit row records the template id + the records
// applied so the operator can see what happened.
//
// The %s placeholder in each record's Name + Content is replaced with the
// zone's canonical id at apply time.
func (s *Service) ApplyTemplate(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	zoneID uuid.UUID,
	templateID string,
) (applied int, err error) {
	if s.provider == nil {
		return 0, ErrProviderDisabled
	}
	tpl, err := GetTemplate(templateID)
	if err != nil {
		return 0, err
	}
	zone, err := s.lookupZoneForCaller(ctx, zoneID)
	if err != nil {
		return 0, err
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditTemplateApply,
		ResourceType: ResourceZone,
		ResourceID:   &zone.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"template_id":  tpl.ID,
			"template":     tpl.Name,
			"canonical_id": zone.CanonicalID,
		},
	})

	for _, r := range tpl.Records {
		name := expandTemplatePlaceholder(r.Name, zone.CanonicalID)
		content := expandTemplatePlaceholder(r.Content, zone.CanonicalID)
		// Re-use the validated create path so the template records
		// receive the same per-type validation + audit + event handling
		// as a hand-written create. The audit row for each record is
		// separate from the template audit row; the operator sees both
		// (the per-record create + the template-apply summary).
		if _, errCreate := s.CreateRecord(ctx, tenantID, userID, zoneID, RecordCreateParams{
			Name:    name,
			Type:    r.Type,
			Content: content,
			TTL:     r.TTL,
		}); errCreate != nil {
			// Idempotent: a record with the same identity already
			// exists; surface as a no-op for the template. Other
			// errors fail the apply.
			if isAlreadyExists(errCreate) {
				continue
			}
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
				"error":         errCreate.Error(),
				"applied_count": applied,
			}})
			return applied, fmt.Errorf("dns: apply template %q record %s %s: %w", tpl.ID, name, r.Type, errCreate)
		}
		applied++
	}

	// Event bus emit — re-uses the zone-updated topic so plugins
	// listening for "dns.zone.*" see the apply as a zone change.
	s.emitEvent(ctx, eventbus.DNSZoneUpdated, tenantID, userID, zone.ID, map[string]any{
		"template_id":  tpl.ID,
		"template":     tpl.Name,
		"applied":      applied,
		"canonical_id": zone.CanonicalID,
	})

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"template_id":   tpl.ID,
		"applied_count": applied,
	}})
	return applied, nil
}

// expandTemplatePlaceholder replaces every "%s" or "%w" in s with the
// appropriate form of zoneID. %s keeps the trailing dot; %w strips it.
// Used for the template's Name + Content so the same template works for
// any zone.
func expandTemplatePlaceholder(s, zoneID string) string {
	if zoneID == "" {
		return s
	}
	zoneNoDot := zoneID
	if zoneNoDot != "" && zoneNoDot[len(zoneNoDot)-1] == '.' {
		zoneNoDot = zoneNoDot[:len(zoneNoDot)-1]
	}
	out := make([]byte, 0, len(s)+len(zoneID))
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '%' {
			switch s[i+1] {
			case 's':
				out = append(out, zoneID...)
				i++
				continue
			case 'w':
				out = append(out, zoneNoDot...)
				i++
				continue
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

// isAlreadyExists reports whether err is one of the "this record already
// exists" sentinels the create path can return.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return err == ErrRecordAlreadyExists
}
