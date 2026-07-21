// Package api: compute_ip_handlers.go implements the WS-30 IP pool +
// floating IP surface on the OpenAPI-derived Fiber server.
//
// Pool admin endpoints live under /api/v1/admin/compute/ip-pools/* and
// are platform-admin only (compute.ip_pool.manage). The tenant-facing
// floating-IP endpoints live under /api/v1/compute/floating-ips/* +
// /api/v1/compute/instances/{id}/floating-ip and are gated by
// compute.floating_ip.{read,manage}.
//
// Handlers are thin: parse request -> call compute service -> render
// response. The service is the single point that owns audit emission
// + provider coordination + WASM event bus fan-out. Per pillar 1 the
// user-facing copy says "IP pool" + "floating IP", never "Incus
// forward" or "BGP".
package api

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v2"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
)

// -------------------------------------------------------------------------
// IP pool admin endpoints (/api/v1/admin/compute/ip-pools/*).
// -------------------------------------------------------------------------

// ListComputeIPPools handles GET /api/v1/admin/compute/ip-pools.
func (s *Server) ListComputeIPPools(c *fiber.Ctx, params apigen.ListComputeIPPoolsParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListIPPools(c.UserContext(), limit, offset)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	items := make([]apigen.ComputeIPPool, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeIPPoolDTO(row))
	}
	total, err := s.computeSvc.CountIPPools(c.UserContext())
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(apigen.ComputeIPPoolPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeIPPool handles POST /api/v1/admin/compute/ip-pools.
func (s *Server) CreateComputeIPPool(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var body apigen.ComputeIPPoolCreateRequest
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	var ptrZone *openapi_types.UUID
	if body.PtrZoneId != nil {
		id := *body.PtrZoneId
		ptrZone = &id
	}
	row, err := s.computeSvc.CreateIPPool(c.UserContext(), uid, compute.CreateIPPoolParams{
		Name:        body.Name,
		Description: derefStr(body.Description),
		PtrZoneID:   ptrZone,
		IsActive:    isActive,
	})
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeIPPoolDTO(row))
}

// GetComputeIPPool handles GET /api/v1/admin/compute/ip-pools/{poolId}.
func (s *Server) GetComputeIPPool(c *fiber.Ctx, poolID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	row, err := s.computeSvc.GetIPPool(c.UserContext(), poolID)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(toComputeIPPoolDTO(row))
}

// UpdateComputeIPPool handles PATCH /api/v1/admin/compute/ip-pools/{poolId}.
func (s *Server) UpdateComputeIPPool(c *fiber.Ctx, poolID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var body apigen.ComputeIPPoolUpdateRequest
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}
	var ptrZone *openapi_types.UUID
	if body.PtrZoneId != nil {
		id := *body.PtrZoneId
		ptrZone = &id
	}
	err := s.computeSvc.UpdateIPPool(c.UserContext(), uid, compute.UpdateIPPoolParams{
		ID:          poolID,
		Description: derefStr(body.Description),
		PtrZoneID:   ptrZone,
		IsActive:    isActive,
	})
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// DeleteComputeIPPool handles DELETE /api/v1/admin/compute/ip-pools/{poolId}.
func (s *Server) DeleteComputeIPPool(c *fiber.Ctx, poolID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteIPPool(c.UserContext(), uid, poolID); err != nil {
		return mapComputeIPError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ListComputeIPPoolRanges handles GET
// /api/v1/admin/compute/ip-pools/{poolId}/ranges.
func (s *Server) ListComputeIPPoolRanges(c *fiber.Ctx, poolID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	// The endpoint does not carry pagination query params today; the
	// typical pool has a handful of ranges. A future WS can add
	// PageLimit/PageOffset if a pool grows past ~100 ranges.
	limit, offset := computePageParams(nil, nil)
	rows, err := s.computeSvc.ListIPPoolRanges(c.UserContext(), poolID, limit, offset)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	items := make([]apigen.ComputeIPPoolRange, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeIPPoolRangeDTO(row))
	}
	total, err := s.computeSvc.CountIPPoolRanges(c.UserContext(), poolID)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(apigen.ComputeIPPoolRangePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// AddComputeIPPoolRange handles POST
// /api/v1/admin/compute/ip-pools/{poolId}/ranges.
func (s *Server) AddComputeIPPoolRange(c *fiber.Ctx, poolID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var body apigen.ComputeIPPoolRangeAddRequest
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	excluded := []string{}
	if body.ExcludedAddresses != nil {
		excluded = *body.ExcludedAddresses
	}
	row, err := s.computeSvc.AddIPPoolRange(c.UserContext(), uid, compute.AddIPPoolRangeParams{
		PoolID:            poolID,
		Cidr:              body.Cidr,
		Family:            int32(body.Family),
		ExcludedAddresses: excluded,
	})
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeIPPoolRangeDTO(row))
}

// DeleteComputeIPPoolRange handles DELETE
// /api/v1/admin/compute/ip-pools/{poolId}/ranges/{rangeId}.
func (s *Server) DeleteComputeIPPoolRange(c *fiber.Ctx, poolID, rangeID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteIPPoolRange(c.UserContext(), uid, poolID, rangeID); err != nil {
		return mapComputeIPError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// -------------------------------------------------------------------------
// Tenant-facing floating IP endpoints (/api/v1/compute/floating-ips/*).
// -------------------------------------------------------------------------

// ListComputeFloatingIPs handles GET /api/v1/compute/floating-ips.
func (s *Server) ListComputeFloatingIPs(c *fiber.Ctx, params apigen.ListComputeFloatingIPsParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListFloatingIPs(c.UserContext(), limit, offset)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	items := make([]apigen.ComputeFloatingIP, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeFloatingIPDTO(row))
	}
	total, err := s.computeSvc.CountFloatingIPs(c.UserContext())
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(apigen.ComputeFloatingIPPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// AllocateComputeFloatingIP handles POST /api/v1/compute/floating-ips.
func (s *Server) AllocateComputeFloatingIP(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var body apigen.ComputeFloatingIPAllocateRequest
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.AllocateFloatingIP(c.UserContext(), tid, uid, compute.AllocateFloatingIPParams{
		PoolID:    body.PoolId,
		PtrTarget: derefStr(body.PtrTarget),
	})
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeFloatingIPDTO(row))
}

// GetComputeFloatingIP handles GET
// /api/v1/compute/floating-ips/{floatingIpId}.
func (s *Server) GetComputeFloatingIP(c *fiber.Ctx, floatingIPID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	row, err := s.computeSvc.GetFloatingIP(c.UserContext(), floatingIPID)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(toComputeFloatingIPDTO(row))
}

// SetComputeFloatingIPPTR handles PATCH
// /api/v1/compute/floating-ips/{floatingIpId}.
func (s *Server) SetComputeFloatingIPPTR(c *fiber.Ctx, floatingIPID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var body apigen.ComputeFloatingIPSetPTRRequest
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.SetFloatingIPPTRTarget(c.UserContext(), tid, uid, floatingIPID, derefStr(body.PtrTarget))
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(toComputeFloatingIPDTO(row))
}

// ReleaseComputeFloatingIP handles DELETE
// /api/v1/compute/floating-ips/{floatingIpId}.
func (s *Server) ReleaseComputeFloatingIP(c *fiber.Ctx, floatingIPID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.ReleaseFloatingIP(c.UserContext(), tid, uid, floatingIPID); err != nil {
		return mapComputeIPError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// AttachComputeFloatingIP handles POST
// /api/v1/compute/floating-ips/{floatingIpId}/attach.
func (s *Server) AttachComputeFloatingIP(c *fiber.Ctx, floatingIPID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var body apigen.ComputeFloatingIPAttachRequest
	if err := c.BodyParser(&body); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.AttachFloatingIP(c.UserContext(), tid, uid, floatingIPID, body.InstanceId)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(toComputeFloatingIPDTO(row))
}

// DetachComputeFloatingIP handles POST
// /api/v1/compute/floating-ips/{floatingIpId}/detach.
func (s *Server) DetachComputeFloatingIP(c *fiber.Ctx, floatingIPID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.DetachFloatingIP(c.UserContext(), tid, uid, floatingIPID)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(toComputeFloatingIPDTO(row))
}

// GetComputeFloatingIPByInstance handles GET
// /api/v1/compute/instances/{instanceId}/floating-ip.
func (s *Server) GetComputeFloatingIPByInstance(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	row, err := s.computeSvc.GetFloatingIPByInstance(c.UserContext(), instanceID)
	if err != nil {
		return mapComputeIPError(c, err)
	}
	return c.JSON(toComputeFloatingIPDTO(row))
}

// -------------------------------------------------------------------------
// Error mapping + DTO helpers.
// -------------------------------------------------------------------------

// mapComputeIPError translates a compute IP-pool/floating-IP service
// error to the right envelope.
func mapComputeIPError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, compute.ErrIPPoolNotFound), errors.Is(err, compute.ErrFloatingIPNotFound),
		errors.Is(err, compute.ErrIPPoolRangeNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "compute.err_not_found", nil))
	case errors.Is(err, compute.ErrIPPoolNameTaken),
		errors.Is(err, compute.ErrIPPoolRangeExists),
		errors.Is(err, compute.ErrIPPoolInactive),
		errors.Is(err, compute.ErrIPPoolExhausted),
		errors.Is(err, compute.ErrIPPoolHasAllocations),
		errors.Is(err, compute.ErrFloatingIPAlreadyAttached),
		errors.Is(err, compute.ErrFloatingIPNotAttached),
		errors.Is(err, compute.ErrFloatingIPAlreadyAllocated):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "compute.err_ip_conflict", nil), nil)
	case errors.Is(err, compute.ErrInvalidCIDR), errors.Is(err, compute.ErrInvalidIPFamily),
		errors.Is(err, compute.ErrInvalidName):
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	// Reuse the general compute error mapper for shared sentinels
	// (ErrProviderDisabled, etc.).
	return mapComputeError(c, err)
}

// toComputeIPPoolDTO converts a database.IPPool row to the OpenAPI DTO.
func toComputeIPPoolDTO(row compute.IPPoolRow) apigen.ComputeIPPool {
	out := apigen.ComputeIPPool{
		Id:        row.ID,
		Name:      row.Name,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt,
		UpdatedAt: &row.UpdatedAt,
	}
	if row.Description != nil {
		out.Description = row.Description
	}
	if row.PtrZoneID != nil {
		zid := *row.PtrZoneID
		out.PtrZoneId = &zid
	}
	return out
}

// toComputeIPPoolRangeDTO converts a database.IPPoolRange row to the
// OpenAPI DTO. The excluded_addresses JSONB blob is decoded into the
// []string the schema expects.
func toComputeIPPoolRangeDTO(row compute.IPPoolRangeRow) apigen.ComputeIPPoolRange {
	familyVal := apigen.ComputeIPPoolRangeFamily(4)
	if row.Family != 4 {
		familyVal = apigen.ComputeIPPoolRangeFamily(6)
	}
	out := apigen.ComputeIPPoolRange{
		Id:                row.ID,
		PoolId:            row.PoolID,
		Cidr:              row.Cidr,
		Family:            familyVal,
		ExcludedAddresses: decodeExcludedAddresses(row.ExcludedAddresses),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         &row.UpdatedAt,
	}
	return out
}

// toComputeFloatingIPDTO converts a database.FloatingIP row to the
// OpenAPI DTO.
func toComputeFloatingIPDTO(row compute.FloatingIPRow) apigen.ComputeFloatingIP {
	familyVal := apigen.ComputeFloatingIPFamily(4)
	if row.Family != 4 {
		familyVal = apigen.ComputeFloatingIPFamily(6)
	}
	out := apigen.ComputeFloatingIP{
		Id:                row.ID,
		TenantId:          row.TenantID,
		PoolId:            row.PoolID,
		Address:           row.Address.String(),
		Family:            familyVal,
		ForwardPushStatus: apigen.ComputeFloatingIPForwardPushStatus(row.ForwardPushStatus),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         &row.UpdatedAt,
	}
	if row.PtrTarget != nil {
		out.PtrTarget = row.PtrTarget
	}
	if row.InstanceID != nil {
		iid := *row.InstanceID
		out.InstanceId = &iid
	}
	if row.NetworkName != nil {
		out.NetworkName = row.NetworkName
	}
	return out
}

// decodeExcludedAddresses unmarshals the JSONB excluded_addresses column
// into a []string. Returns an empty (non-nil) slice for an empty blob
// so the OpenAPI response always carries the field shape.
func decodeExcludedAddresses(raw []byte) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	// The row's column is json.RawMessage which decodes cleanly with
	// the standard library. The pool's add-path marshalled it via
	// database.marshalExcludedAddresses.
	if err := json.Unmarshal(raw, &out); err != nil {
		return []string{}
	}
	return out
}

// Note: derefStr is shared with dns_domains_handlers.go (declared there).
