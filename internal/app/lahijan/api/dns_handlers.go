// Package api: dns_handlers.go implements the OpenAPI-derived DNS endpoints
// (WS-15). Each handler is thin: parse request, resolve IDs from the
// request scope, call the DNS service, render the response. Every
// privileged route is gated by RequirePerm via the audit gate middleware
// (extended in router.go to cover /api/v1/dns/*).
//
// Error mapping follows the WS-15 DoD:
//
//   - 400 bad_request          -> malformed body / params / invalid content
//   - 401 unauthorized         -> no session (auth middleware)
//   - 403 forbidden            -> missing permission (rbac middleware)
//   - 404 not_found            -> zone / record missing
//   - 409 conflict             -> canonical id taken / record identity taken
//   - 422 unprocessable_entity -> per-tenant record cap exceeded
//   - 501 not_implemented      -> PowerDNS provider disabled
package api

import (
	"errors"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/dns"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultDNSPageSize is the page size for DNS list endpoints when the
// caller does not pass one. Mirrors DefaultComputePageSize so the two
// areas render consistently.
const DefaultDNSPageSize = 50

// MaxDNSPageSize caps a single page so a misbehaving client cannot
// request millions of rows in one call.
const MaxDNSPageSize = 200

// dnsPageParams clamps + defaults limit/offset for the DNS list endpoints.
func dnsPageParams(limit, offset *int) (int32, int32) {
	lim := DefaultDNSPageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxDNSPageSize {
		lim = MaxDNSPageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return int32(lim), int32(off)
}

// dnsDisabledMsg is the localised "feature disabled" message the handlers
// return when the PowerDNS provider is not wired.
func dnsDisabledMsg(c *fiber.Ctx) string {
	return i18n.T(c.UserContext(), "dns.err_disabled", nil)
}

// dnsTenantAndUser resolves the tenant + user id pair from the request
// scope. Mirrors computeTenantAndUser.
func (s *Server) dnsTenantAndUser(c *fiber.Ctx) (uuid.UUID, uuid.UUID, bool) {
	uid, ok := currentUserID(c)
	if !ok {
		_ = SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
		return uuid.Nil, uuid.Nil, false
	}
	tid, err := database.TenantFromContext(c.UserContext())
	if err != nil {
		_ = SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
		return uuid.Nil, uuid.Nil, false
	}
	return tid, uid, true
}

// mapDNSError translates a DNS service error to the right envelope.
func mapDNSError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, dns.ErrProviderDisabled):
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	case errors.Is(err, dns.ErrZoneNotFound), errors.Is(err, dns.ErrRecordNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "dns.err_not_found", nil))
	case errors.Is(err, dns.ErrZoneAlreadyExists), errors.Is(err, dns.ErrRecordAlreadyExists):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "dns.err_already_exists", nil), nil)
	case errors.Is(err, dns.ErrTemplateNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "dns.err_template_not_found", nil))
	case errors.Is(err, dns.ErrInvalidZoneName),
		errors.Is(err, dns.ErrInvalidRecordName),
		errors.Is(err, dns.ErrInvalidRecordType),
		errors.Is(err, dns.ErrInvalidRecordContent),
		errors.Is(err, dns.ErrInvalidTTL),
		errors.Is(err, dns.ErrCNAMEAtApex):
		// Per WS-15 DoD "invalid record content rejected with a clear
		// message (per type)": surface the validator's message verbatim
		// so the UI can render it.
		return SendBadRequest(c, err.Error(), nil)
	}
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// ---------------------------------------------------------------------------
// Zones
// ---------------------------------------------------------------------------

// ListDNSZones handles GET /api/v1/dns/zones.
func (s *Server) ListDNSZones(c *fiber.Ctx, params apigen.ListDNSZonesParams) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, _, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := dnsPageParams(params.Limit, params.Offset)
	rows, err := s.dnsSvc.ListZones(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapDNSError(c, err)
	}
	items := make([]apigen.DNSZone, 0, len(rows))
	for _, row := range rows {
		items = append(items, toDNSZoneDTO(row))
	}
	total, err := s.dnsSvc.CountZones(c.UserContext(), tid)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(apigen.DNSZonePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateDNSZone handles POST /api/v1/dns/zones.
func (s *Server) CreateDNSZone(c *fiber.Ctx) error {
	var req apigen.DNSZoneCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Name == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "dns.err_bad_request", nil), nil)
	}
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	kind := ""
	if req.Kind != nil {
		kind = string(*req.Kind)
	}
	row, err := s.dnsSvc.CreateZone(c.UserContext(), tid, uid, dns.ZoneCreateParams{
		Name:        req.Name,
		Kind:        kind,
		Description: ptrString(req.Description),
	})
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toDNSZoneDTO(row))
}

// GetDNSZone handles GET /api/v1/dns/zones/{zoneId}.
func (s *Server) GetDNSZone(c *fiber.Ctx, zoneID openapi_types.UUID) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, _, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.dnsSvc.ReconcileZone(c.UserContext(), tid, zoneID)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(toDNSZoneDTO(row))
}

// UpdateDNSZone handles PATCH /api/v1/dns/zones/{zoneId}.
func (s *Server) UpdateDNSZone(c *fiber.Ctx, zoneID openapi_types.UUID) error {
	var req apigen.DNSZoneUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	var params dns.ZoneUpdateParams
	if req.Description != nil {
		params.Description = req.Description
	}
	if req.Kind != nil {
		k := string(*req.Kind)
		params.Kind = &k
	}
	row, err := s.dnsSvc.UpdateZone(c.UserContext(), tid, uid, zoneID, params)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(toDNSZoneDTO(row))
}

// DeleteDNSZone handles DELETE /api/v1/dns/zones/{zoneId}.
func (s *Server) DeleteDNSZone(c *fiber.Ctx, zoneID openapi_types.UUID) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.dnsSvc.DeleteZone(c.UserContext(), tid, uid, zoneID); err != nil {
		return mapDNSError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

// ListDNSRecords handles GET /api/v1/dns/zones/{zoneId}/records.
func (s *Server) ListDNSRecords(c *fiber.Ctx, zoneID openapi_types.UUID, params apigen.ListDNSRecordsParams) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, _, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := dnsPageParams(params.Limit, params.Offset)
	rows, err := s.dnsSvc.ListRecords(c.UserContext(), tid, zoneID, limit, offset)
	if err != nil {
		return mapDNSError(c, err)
	}
	items := make([]apigen.DNSRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, toDNSRecordDTO(row))
	}
	total, err := s.dnsSvc.CountRecords(c.UserContext(), tid, zoneID)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(apigen.DNSRecordPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateDNSRecord handles POST /api/v1/dns/zones/{zoneId}/records.
func (s *Server) CreateDNSRecord(c *fiber.Ctx, zoneID openapi_types.UUID) error {
	var req apigen.DNSRecordCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Name == "" || req.Type == "" || req.Content == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "dns.err_bad_request", nil), nil)
	}
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	ttl := 0
	if req.Ttl != nil {
		ttl = *req.Ttl
	}
	disabled := false
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	row, err := s.dnsSvc.CreateRecord(c.UserContext(), tid, uid, zoneID, dns.RecordCreateParams{
		Name:     req.Name,
		Type:     string(req.Type),
		Content:  req.Content,
		TTL:      ttl,
		Disabled: disabled,
	})
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toDNSRecordDTO(row))
}

// GetDNSRecord handles GET /api/v1/dns/zones/{zoneId}/records/{recordId}.
func (s *Server) GetDNSRecord(c *fiber.Ctx, zoneID, recordID openapi_types.UUID) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, _, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.dnsSvc.GetRecord(c.UserContext(), tid, zoneID, recordID)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(toDNSRecordDTO(row))
}

// UpdateDNSRecord handles PATCH /api/v1/dns/zones/{zoneId}/records/{recordId}.
func (s *Server) UpdateDNSRecord(c *fiber.Ctx, zoneID, recordID openapi_types.UUID) error {
	var req apigen.DNSRecordUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	params := dns.RecordUpdateParams{
		Content:  req.Content,
		TTL:      req.Ttl,
		Disabled: req.Disabled,
	}
	row, err := s.dnsSvc.UpdateRecord(c.UserContext(), tid, uid, zoneID, recordID, params)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(toDNSRecordDTO(row))
}

// DeleteDNSRecord handles DELETE /api/v1/dns/zones/{zoneId}/records/{recordId}.
func (s *Server) DeleteDNSRecord(c *fiber.Ctx, zoneID, recordID openapi_types.UUID) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.dnsSvc.DeleteRecord(c.UserContext(), tid, uid, zoneID, recordID); err != nil {
		return mapDNSError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// DNSSEC
// ---------------------------------------------------------------------------

// SetDNSZoneDNSSEC handles POST /api/v1/dns/zones/{zoneId}/dnssec/{action}.
func (s *Server) SetDNSZoneDNSSEC(c *fiber.Ctx, zoneID openapi_types.UUID, action string) error {
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	var err error
	switch action {
	case "enable":
		err = s.dnsSvc.EnableDNSSEC(c.UserContext(), tid, uid, zoneID)
	case "disable":
		err = s.dnsSvc.DisableDNSSEC(c.UserContext(), tid, uid, zoneID)
	default:
		return SendBadRequest(c, i18n.T(c.UserContext(), "dns.err_bad_request", nil), nil)
	}
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

// ListDNSTemplates handles GET /api/v1/dns/templates.
func (s *Server) ListDNSTemplates(c *fiber.Ctx) error {
	items := dns.ListTemplates()
	out := make([]apigen.DNSTemplate, 0, len(items))
	for _, t := range items {
		out = append(out, toDNSTemplateDTO(t))
	}
	// The response shape is documented inline in the OpenAPI spec as
	// `{items: [...]}`; we use fiber.Map because the spec uses an inline
	// schema rather than a named component. The shape is stable; a
	// future iteration can extract a named component if a client needs
	// it.
	return c.JSON(fiber.Map{"items": out})
}

// ApplyDNSTemplate handles POST /api/v1/dns/zones/{zoneId}/apply-template.
func (s *Server) ApplyDNSTemplate(c *fiber.Ctx, zoneID openapi_types.UUID) error {
	var req apigen.ApplyDNSTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.TemplateId == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "dns.err_bad_request", nil), nil)
	}
	if s.dnsSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, dnsDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	applied, err := s.dnsSvc.ApplyTemplate(c.UserContext(), tid, uid, zoneID, req.TemplateId)
	if err != nil {
		return mapDNSError(c, err)
	}
	return c.JSON(apigen.ApplyDNSTemplateResult{Applied: applied})
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

// toDNSZoneDTO converts a database.DNSZone row to the OpenAPI DNSZone schema.
func toDNSZoneDTO(row database.DNSZone) apigen.DNSZone {
	out := apigen.DNSZone{
		Id:              row.ID,
		TenantId:        row.TenantID,
		CanonicalId:     row.CanonicalID,
		Name:            row.Name,
		Kind:            apigen.DNSZoneKind(row.Kind),
		IsDnssecEnabled: row.IsDnssecEnabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       &row.UpdatedAt,
	}
	if row.Description != "" {
		out.Description = &row.Description
	}
	axfr := row.IsAxfrEnabled
	out.IsAxfrEnabled = &axfr
	return out
}

// toDNSRecordDTO converts a database.DNSRecord row to the OpenAPI DNSRecord
// schema.
func toDNSRecordDTO(row database.DNSRecord) apigen.DNSRecord {
	out := apigen.DNSRecord{
		Id:        row.ID,
		TenantId:  row.TenantID,
		ZoneId:    row.ZoneID,
		Name:      row.Name,
		Type:      row.Type,
		Content:   row.Content,
		Ttl:       int(row.Ttl),
		CreatedAt: row.CreatedAt,
		UpdatedAt: &row.UpdatedAt,
	}
	if row.Prio != 0 {
		p := int(row.Prio)
		out.Prio = &p
	}
	// Disabled is always rendered (the spec marks it omitempty so the
	// client treats absence as false). Surfacing the value explicitly
	// lets the UI render the toggle deterministically.
	disabled := row.Disabled
	out.Disabled = &disabled
	return out
}

// toDNSTemplateDTO converts a dns.Template to the OpenAPI DNSTemplate schema.
func toDNSTemplateDTO(t dns.Template) apigen.DNSTemplate {
	records := make([]apigen.DNSTemplateRecord, 0, len(t.Records))
	for _, r := range t.Records {
		rec := apigen.DNSTemplateRecord{
			Name:    r.Name,
			Type:    apigen.DNSTemplateRecordType(r.Type),
			Content: r.Content,
		}
		if r.TTL > 0 {
			ttl := r.TTL
			rec.Ttl = &ttl
		}
		records = append(records, rec)
	}
	return apigen.DNSTemplate{
		Id:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Records:     records,
	}
}
