// Package api: dns_domains_handlers.go implements the OpenAPI-derived
// DNS domain endpoints (WS-28). Each handler is thin: parse request,
// resolve IDs from the request scope, call the registrar service,
// render the response. Every privileged route is gated by RequirePerm
// via the audit gate middleware (extended in router.go to cover
// /api/v1/dns/domains/*).
//
// Error mapping follows the WS-28 DoD:
//
//   - 400 bad_request          -> malformed body / params / invalid name
//   - 401 unauthorized         -> no session (auth middleware)
//   - 402 payment_required     -> billing charge rejected (insufficient balance)
//   - 403 forbidden            -> missing permission (rbac middleware)
//   - 404 not_found            -> domain missing
//   - 409 conflict             -> domain taken / locked for transfer
//   - 501 not_implemented      -> registrar provider disabled
package api

import (
	"errors"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/registrar"
	registrarsvc "github.com/avestura/lahijan/internal/app/lahijan/registrar"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// domainDisabledMsg is the localised "feature disabled" message the
// handlers return when the registrar provider is not wired.
func domainDisabledMsg(c *fiber.Ctx) string {
	return i18n.T(c.UserContext(), "dns.err_domain_disabled", nil)
}

// mapRegistrarError translates a registrar service error to the right
// envelope. Mirrors mapDNSError for the WS-15 surface.
func mapRegistrarError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, registrarsvc.ErrProviderDisabled):
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	case errors.Is(err, registrarsvc.ErrDomainNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "dns.err_domain_not_found", nil))
	case errors.Is(err, registrarsvc.ErrDomainUnavailable):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "dns.err_domain_unavailable", nil), nil)
	case errors.Is(err, registrarsvc.ErrBillingRequired):
		return SendError(c, fiber.StatusPaymentRequired, CodePaymentRequired,
			i18n.T(c.UserContext(), "billing.err_insufficient_balance", nil), nil)
	case errors.Is(err, registrarsvc.ErrInvalidDomain),
		errors.Is(err, registrarsvc.ErrInvalidPeriod),
		errors.Is(err, registrarsvc.ErrInvalidContact),
		errors.Is(err, registrarsvc.ErrAuthCodeRequired),
		errors.Is(err, registrarsvc.ErrNoPricing):
		return SendBadRequest(c, err.Error(), nil)
	}
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// toDNSDomainDTO converts a database row to the OpenAPI DNSDomain
// schema. Nullable fields are surfaced as pointers per the spec.
func toDNSDomainDTO(d database.DNSDomain) apigen.DNSDomain {
	out := apigen.DNSDomain{
		Id:               d.ID,
		TenantId:         d.TenantID,
		Name:             d.Name,
		Status:           apigen.DNSDomainStatus(d.Status),
		RegistrarOrderId: &d.RegistrarOrderID,
		ContactProfileId: &d.ContactProfileID,
		PriceCents:       d.PriceCents,
		Currency:         d.Currency,
		PeriodYears:      int(d.PeriodYears),
		IsDnssecEnabled:  d.IsDnssecEnabled,
		IsAutoRenew:      d.IsAutoRenew,
		RegisteredAt:     d.RegisteredAt,
		ExpiresAt:        d.ExpiresAt,
		CreatedAt:        d.CreatedAt,
		UpdatedAt:        d.UpdatedAt,
	}
	if d.ZoneID != nil {
		zid := *d.ZoneID
		out.ZoneId = &zid
	}
	if d.LedgerEntryID != nil {
		lid := *d.LedgerEntryID
		out.LedgerEntryId = &lid
	}
	return out
}

// toDNSDomainPricingDTO converts a service Pricing to the OpenAPI
// DNSDomainPricing schema.
func toDNSDomainPricingDTO(p registrarsvc.Pricing) apigen.DNSDomainPricing {
	return apigen.DNSDomainPricing{
		PeriodYears: int(p.PeriodYears),
		PriceCents:  p.PriceCents,
		Currency:    p.Currency,
	}
}

// SearchDNSDomain handles POST /api/v1/dns/domains/search.
func (s *Server) SearchDNSDomain(c *fiber.Ctx) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	var body apigen.SearchDNSDomainJSONRequestBody
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	res, err := s.registrarSvc.SearchDomain(c.UserContext(), tid, uid, body.Domain)
	if err != nil {
		return mapRegistrarError(c, err)
	}
	out := apigen.DNSDomainSearchResult{
		Domain:    res.Domain,
		Available: res.Available,
		Status:    apigen.DNSDomainSearchResultStatus(res.Status),
		Pricing:   make([]apigen.DNSDomainPricing, 0, len(res.Pricing)),
	}
	if res.Reason != "" {
		reason := res.Reason
		out.Reason = &reason
	}
	for _, p := range res.Pricing {
		out.Pricing = append(out.Pricing, toDNSDomainPricingDTO(p))
	}
	return c.JSON(out)
}

// ListDNSDomains handles GET /api/v1/dns/domains.
func (s *Server) ListDNSDomains(c *fiber.Ctx, params apigen.ListDNSDomainsParams) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	tid, _, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := dnsPageParams(params.Limit, params.Offset)
	rows, err := s.registrarSvc.ListDomains(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapRegistrarError(c, err)
	}
	total, err := s.registrarSvc.CountDomains(c.UserContext(), tid)
	if err != nil {
		return mapRegistrarError(c, err)
	}
	items := make([]apigen.DNSDomain, 0, len(rows))
	for _, r := range rows {
		items = append(items, toDNSDomainDTO(r))
	}
	return c.JSON(apigen.DNSDomainPage{
		Items:  items,
		Total:  int(total),
		Limit:  int(limit),
		Offset: int(offset),
	})
}

// RegisterDNSDomain handles POST /api/v1/dns/domains.
func (s *Server) RegisterDNSDomain(c *fiber.Ctx) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	var body apigen.RegisterDNSDomainJSONRequestBody
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	contact := bodyContactToService(body.Contact)
	autoRenew := false
	if body.AutoRenew != nil {
		autoRenew = *body.AutoRenew
	}
	whoisPrivacy := false
	if body.WhoisPrivacy != nil {
		whoisPrivacy = *body.WhoisPrivacy
	}
	autoProv := true
	if body.AutoProvision != nil {
		autoProv = *body.AutoProvision
	}
	autoDNSSEC := false
	if body.AutoDNSSEC != nil {
		autoDNSSEC = *body.AutoDNSSEC
	}
	row, err := s.registrarSvc.RegisterDomain(c.UserContext(), tid, uid, registrarsvc.RegisterDomainRequest{
		Domain:        body.Domain,
		PeriodYears:   int32(body.PeriodYears),
		Contact:       contact,
		AutoRenew:     autoRenew,
		WHOISPrivacy:  whoisPrivacy,
		AutoProvision: autoProv,
		AutoDNSSEC:    autoDNSSEC,
	})
	if err != nil {
		return mapRegistrarError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toDNSDomainDTO(row))
}

// GetDNSDomain handles GET /api/v1/dns/domains/{domainId}.
func (s *Server) GetDNSDomain(c *fiber.Ctx, domainID openapi_types.UUID) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	tid, _, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.registrarSvc.GetDomain(c.UserContext(), tid, uuid.Nil, domainID)
	if err != nil {
		return mapRegistrarError(c, err)
	}
	return c.JSON(toDNSDomainDTO(row))
}

// DeleteDNSDomain handles DELETE /api/v1/dns/domains/{domainId}.
func (s *Server) DeleteDNSDomain(c *fiber.Ctx, domainID openapi_types.UUID) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.registrarSvc.DeleteDomain(c.UserContext(), tid, uid, domainID); err != nil {
		return mapRegistrarError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// RenewDNSDomain handles POST /api/v1/dns/domains/{domainId}/renew.
func (s *Server) RenewDNSDomain(c *fiber.Ctx, domainID openapi_types.UUID) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	var body apigen.RenewDNSDomainJSONRequestBody
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.registrarSvc.RenewDomain(c.UserContext(), tid, uid, registrarsvc.RenewDomainRequest{
		DomainID:    domainID,
		PeriodYears: int32(body.PeriodYears),
	})
	if err != nil {
		return mapRegistrarError(c, err)
	}
	return c.JSON(toDNSDomainDTO(row))
}

// TransferDNSDomain handles POST /api/v1/dns/domains/transfer.
func (s *Server) TransferDNSDomain(c *fiber.Ctx) error {
	if s.registrarSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, domainDisabledMsg(c), nil)
	}
	var body apigen.TransferDNSDomainJSONRequestBody
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	tid, uid, ok := s.dnsTenantAndUser(c)
	if !ok {
		return nil
	}
	contact := bodyContactToService(body.Contact)
	row, err := s.registrarSvc.TransferDomain(c.UserContext(), tid, uid, registrarsvc.TransferDomainRequest{
		Domain:      body.Domain,
		AuthCode:    body.AuthCode,
		PeriodYears: int32(body.PeriodYears),
		Contact:     contact,
	})
	if err != nil {
		return mapRegistrarError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toDNSDomainDTO(row))
}

// bodyContactToService converts the OpenAPI DNSDomainContact pointer to
// the registrar provider's ContactProfile pointer (so a nil contact
// flows through as "use the service's default profile").
func bodyContactToService(c *apigen.DNSDomainContact) *registrar.ContactProfile {
	if c == nil {
		return nil
	}
	out := &registrar.ContactProfile{
		OwnerFirstname:    c.OwnerFirstname,
		OwnerLastname:     c.OwnerLastname,
		OwnerOrganization: derefStr(c.OwnerOrganization),
		OwnerEmail:        string(c.OwnerEmail),
		OwnerPhone:        c.OwnerPhone,
		Address1:          c.Address1,
		Address2:          derefStr(c.Address2),
		City:              c.City,
		State:             c.State,
		Zip:               c.Zip,
		CountryCode:       c.CountryCode,
	}
	return out
}

// derefStr returns *s when non-nil, else "".
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
