// Package api: compute_handlers.go implements the OpenAPI-derived compute
// endpoints (WS-14). Each handler is thin: parse request, resolve IDs from
// the request scope, call the compute service, render the response. Every
// privileged route is gated by RequirePerm via the audit gate middleware
// (extended in router.go to cover /api/v1/compute/*).
//
// Error mapping follows the WS-14 DoD:
//
//   - 400 bad_request          -> malformed body / params
//   - 401 unauthorized         -> no session (auth middleware)
//   - 403 forbidden            -> missing permission (rbac middleware)
//   - 402 payment_required     -> insufficient balance (ADR-0013)
//   - 404 not_found            -> instance/image/profile/network/volume missing
//   - 409 conflict             -> name taken / wrong state for action
//   - 422 unprocessable_entity -> quota exceeded
//   - 501 not_implemented      -> Incus provider disabled
package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultComputePageSize is the page size for compute list endpoints when
// the caller does not pass one. Kept conservative so a wide catalog does
// not stress the DB on every list call.
const DefaultComputePageSize = 50

// MaxComputePageSize caps a single page so a misbehaving client cannot
// request millions of rows in one call.
const MaxComputePageSize = 200

// computePageParams clamps + defaults limit/offset for the compute list
// endpoints. Mirrors the audit list helper but with its own constants so
// the two areas can grow independently.
func computePageParams(limit, offset *int) (int32, int32) {
	lim := DefaultComputePageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxComputePageSize {
		lim = MaxComputePageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return int32(lim), int32(off)
}

// computeDisabledMsg is the localised "feature disabled" message the
// handlers return when the Incus provider is not wired. Re-uses the
// plugins.err_disabled key shape so the UI can render consistently.
func computeDisabledMsg(c *fiber.Ctx) string {
	return i18n.T(c.UserContext(), "compute.err_disabled", nil)
}

// computeTenantAndUser resolves the tenant + user id pair from the request
// scope. Returns (zero, zero, false) when either is missing; the handler
// returns the appropriate envelope in that case.
func (s *Server) computeTenantAndUser(c *fiber.Ctx) (uuid.UUID, uuid.UUID, bool) {
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

// mapComputeError translates a compute service error to the right envelope.
// Returns (status, code, message) the handler passes to SendError.
func mapComputeError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, compute.ErrProviderDisabled):
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	case errors.Is(err, compute.ErrInstanceNotFound),
		errors.Is(err, compute.ErrImageNotFound),
		errors.Is(err, compute.ErrProfileNotFound),
		errors.Is(err, compute.ErrNetworkNotFound),
		errors.Is(err, compute.ErrVolumeNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "compute.err_not_found", nil))
	case errors.Is(err, compute.ErrInstanceNameTaken) || errors.Is(err, compute.ErrNameTaken):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "compute.err_name_taken", nil), nil)
	case errors.Is(err, compute.ErrInstanceNotRunning):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "compute.err_not_running", nil), nil)
	case errors.Is(err, compute.ErrInstanceNotVM):
		// WS-24: only VMs get a graphical console; containers stay on exec.
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "compute.err_not_vm", nil), nil)
	case errors.Is(err, compute.ErrVNCUnavailable):
		// WS-24: daemon refused or failed the console-open call.
		return SendServiceUnavailable(c, i18n.T(c.UserContext(), "compute.err_vnc_unavailable", nil))
	case errors.Is(err, compute.ErrInvalidName), errors.Is(err, compute.ErrInvalidImage):
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	case compute.IsQuotaExceeded(err):
		// The quota error carries the structured detail the UI needs to
		// render "using X of Y"; surface it as the envelope's details map.
		var qe *compute.QuotaExceededError
		if errors.As(err, &qe) {
			return SendError(
				c, fiber.StatusUnprocessableEntity, "quota_exceeded",
				i18n.T(c.UserContext(), "compute.err_quota_exceeded", nil),
				map[string]any{
					"dimension": qe.Dimension,
					"limit":     qe.Limit,
					"current":   qe.Current,
					"requested": qe.Requested,
				},
			)
		}
		return SendError(c, fiber.StatusUnprocessableEntity, "quota_exceeded",
			i18n.T(c.UserContext(), "compute.err_quota_exceeded", nil), nil)
	case errors.Is(err, compute.ErrInsufficientBalance):
		return SendError(c, fiber.StatusPaymentRequired, "insufficient_balance",
			i18n.T(c.UserContext(), "compute.err_insufficient_balance", nil), nil)
	}
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// ---------------------------------------------------------------------------
// Instances
// ---------------------------------------------------------------------------

// ListComputeInstances handles GET /api/v1/compute/instances.
func (s *Server) ListComputeInstances(c *fiber.Ctx, params apigen.ListComputeInstancesParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListInstances(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapComputeError(c, err)
	}
	items := make([]apigen.ComputeInstance, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeInstanceDTO(row))
	}
	total, err := s.computeSvc.CountInstances(c.UserContext(), tid)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeInstancePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeInstance handles POST /api/v1/compute/instances.
func (s *Server) CreateComputeInstance(c *fiber.Ctx) error {
	// Validate body shape BEFORE the svc check so a missing-name or
	// missing-imageAlias returns 400 (not 501) even when the Incus
	// provider is disabled. The 501 path is reserved for "the body is
	// valid but the daemon isn't wired".
	var req apigen.ComputeInstanceCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Name == "" || req.ImageAlias == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.CreateInstance(c.UserContext(), tid, uid, compute.InstanceCreateParams{
		Name:        req.Name,
		Type:        string(ptrComputeType(req.Type)),
		ImageAlias:  req.ImageAlias,
		Description: ptrString(req.Description),
		Config:      ptrStringMap(req.Config),
		Devices:     ptrDevicesMap(req.Devices),
		Profiles:    ptrStringSlice(req.Profiles),
	})
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeInstanceDTO(row))
}

// GetComputeInstance handles GET /api/v1/compute/instances/{instanceId}.
// Reconciles with the daemon (best-effort).
func (s *Server) GetComputeInstance(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.ReconcileInstance(c.UserContext(), tid, instanceID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeInstanceDTO(row))
}

// UpdateComputeInstance handles PATCH /api/v1/compute/instances/{instanceId}.
func (s *Server) UpdateComputeInstance(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeInstanceUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.UpdateInstance(
		c.UserContext(), tid, uid, instanceID,
		ptrString(req.Description),
		ptrStringMap(req.Config),
		ptrDevicesMap(req.Devices),
		ptrStringSlice(req.Profiles),
	)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeInstanceDTO(row))
}

// DeleteComputeInstance handles DELETE /api/v1/compute/instances/{instanceId}.
func (s *Server) DeleteComputeInstance(
	c *fiber.Ctx,
	instanceID openapi_types.UUID,
	params apigen.DeleteComputeInstanceParams,
) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	force := false
	if params.Force != nil {
		force = *params.Force
	}
	if err := s.computeSvc.DeleteInstance(c.UserContext(), tid, uid, instanceID, force); err != nil {
		return mapComputeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SetComputeInstanceState handles POST /api/v1/compute/instances/{instanceId}/{action}.
func (s *Server) SetComputeInstanceState(
	c *fiber.Ctx,
	instanceID openapi_types.UUID,
	action string,
	params apigen.SetComputeInstanceStateParams,
) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	force := false
	if params.Force != nil {
		force = *params.Force
	}
	timeout := 30
	if params.Timeout != nil && *params.Timeout >= 0 {
		timeout = *params.Timeout
	}
	row, err := s.computeSvc.SetInstanceState(c.UserContext(), tid, uid,
		instanceID, compute.InstanceLifecycleAction(action), force, timeout)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeInstanceDTO(row))
}

// ExecComputeInstance handles POST /api/v1/compute/instances/{instanceId}/exec.
func (s *Server) ExecComputeInstance(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeExecRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if len(req.Command) == 0 {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	res, err := s.computeSvc.Exec(c.UserContext(), tid, uid, compute.ExecParams{
		InstanceID:  instanceID,
		Command:     req.Command,
		Environment: ptrStringMap(req.Environment),
		User:        ptrInt(req.User),
		Group:       ptrInt(req.Group),
		Cwd:         ptrString(req.Cwd),
		Stdin:       []byte(ptrStringOr(req.Stdin, "")),
	})
	if err != nil {
		return mapComputeError(c, err)
	}
	out := apigen.ComputeExecResult{ExitCode: res.ExitCode}
	if len(res.Stdout) > 0 {
		s := encodeExecOutput(res.Stdout)
		out.Stdout = &s
	}
	if len(res.Stderr) > 0 {
		s := encodeExecOutput(res.Stderr)
		out.Stderr = &s
	}
	return c.JSON(out)
}

// encodeExecOutput returns the UTF-8 text when the output is valid UTF-8,
// otherwise base64-encodes it so the JSON envelope stays text-safe. The
// ComputeExecResult schema documents this contract.
func encodeExecOutput(b []byte) string {
	// Cheap valid-UTF-8 check: if the bytes round-trip through a string
	// without invalid runes, return them as-is. The base64 fallback
	// covers binary output (compressed logs, etc.).
	if isValidUTF8(b) {
		return string(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// isValidUTF8 walks the bytes and returns false on the first invalid UTF-8
// sequence. Avoids pulling unicode/utf8 for the one call site.
func isValidUTF8(b []byte) bool {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			size := 1
			i += size
		case c&0xE0 == 0xC0:
			size := 2
			if !validUTF8Cont(b, i, size) {
				return false
			}
			i += size
		case c&0xF0 == 0xE0:
			size := 3
			if !validUTF8Cont(b, i, size) {
				return false
			}
			i += size
		case c&0xF8 == 0xF0:
			size := 4
			if !validUTF8Cont(b, i, size) {
				return false
			}
			i += size
		default:
			return false
		}
	}
	return true
}

// validUTF8Cont checks the continuation bytes for a multi-byte sequence.
func validUTF8Cont(b []byte, start, size int) bool {
	if start+size > len(b) {
		return false
	}
	for j := 1; j < size; j++ {
		if b[start+j]&0xC0 != 0x80 {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Images
// ---------------------------------------------------------------------------

// ListComputeImages handles GET /api/v1/compute/images.
func (s *Server) ListComputeImages(c *fiber.Ctx, params apigen.ListComputeImagesParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListImages(c.UserContext(), limit, offset)
	if err != nil {
		return mapComputeError(c, err)
	}
	items := make([]apigen.ComputeImage, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeImageDTO(row))
	}
	total, err := s.computeSvc.CountImages(c.UserContext())
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeImagePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// UploadComputeImage handles POST /api/v1/compute/images.
func (s *Server) UploadComputeImage(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeImageUploadRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.UploadImage(c.UserContext(), tid, uid, compute.UploadImageParams{
		Alias:        req.Alias,
		Fingerprint:  req.Fingerprint,
		Type:         string(ptrImageType(req.Type)),
		Architecture: ptrString(req.Architecture),
		SizeBytes:    ptrInt64(req.SizeBytes),
		Properties:   ptrStringMap(req.Properties),
		Description:  ptrString(req.Description),
	})
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeImageDTO(row))
}

// GetComputeImage handles GET /api/v1/compute/images/{imageId}.
func (s *Server) GetComputeImage(c *fiber.Ctx, imageID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetImage(c.UserContext(), imageID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeImageDTO(row))
}

// DeleteComputeImage handles DELETE /api/v1/compute/images/{imageId}.
func (s *Server) DeleteComputeImage(c *fiber.Ctx, imageID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteImage(c.UserContext(), tid, uid, imageID); err != nil {
		return mapComputeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Profiles
// ---------------------------------------------------------------------------

// ListComputeProfiles handles GET /api/v1/compute/profiles.
func (s *Server) ListComputeProfiles(c *fiber.Ctx, params apigen.ListComputeProfilesParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListProfiles(c.UserContext(), limit, offset)
	if err != nil {
		return mapComputeError(c, err)
	}
	items := make([]apigen.ComputeProfile, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeProfileDTO(row))
	}
	total, err := s.computeSvc.CountProfiles(c.UserContext())
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeProfilePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeProfile handles POST /api/v1/compute/profiles.
func (s *Server) CreateComputeProfile(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeProfileCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.CreateProfile(c.UserContext(), tid, uid, compute.CreateProfileParams{
		Name:        req.Name,
		Description: ptrString(req.Description),
		Config:      ptrStringMap(req.Config),
		Devices:     ptrDevicesMap(req.Devices),
	})
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeProfileDTO(row))
}

// GetComputeProfile handles GET /api/v1/compute/profiles/{profileId}.
func (s *Server) GetComputeProfile(c *fiber.Ctx, profileID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetProfile(c.UserContext(), profileID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeProfileDTO(row))
}

// DeleteComputeProfile handles DELETE /api/v1/compute/profiles/{profileId}.
func (s *Server) DeleteComputeProfile(c *fiber.Ctx, profileID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteProfile(c.UserContext(), tid, uid, profileID); err != nil {
		return mapComputeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Networks
// ---------------------------------------------------------------------------

// ListComputeNetworks handles GET /api/v1/compute/networks.
func (s *Server) ListComputeNetworks(c *fiber.Ctx, params apigen.ListComputeNetworksParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListNetworks(c.UserContext(), limit, offset)
	if err != nil {
		return mapComputeError(c, err)
	}
	items := make([]apigen.ComputeNetwork, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeNetworkDTO(row))
	}
	total, err := s.computeSvc.CountNetworks(c.UserContext())
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeNetworkPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeNetwork handles POST /api/v1/compute/networks.
func (s *Server) CreateComputeNetwork(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeNetworkCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.CreateNetwork(c.UserContext(), tid, uid, compute.CreateNetworkParams{
		Name:        req.Name,
		Description: ptrString(req.Description),
		Type:        ptrStringOr(req.Type, "bridge"),
		Config:      ptrStringMap(req.Config),
	})
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeNetworkDTO(row))
}

// GetComputeNetwork handles GET /api/v1/compute/networks/{networkId}.
func (s *Server) GetComputeNetwork(c *fiber.Ctx, networkID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetNetwork(c.UserContext(), networkID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeNetworkDTO(row))
}

// DeleteComputeNetwork handles DELETE /api/v1/compute/networks/{networkId}.
func (s *Server) DeleteComputeNetwork(c *fiber.Ctx, networkID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteNetwork(c.UserContext(), tid, uid, networkID); err != nil {
		return mapComputeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Storage volumes
// ---------------------------------------------------------------------------

// ListComputeStorageVolumes handles GET /api/v1/compute/storage.
func (s *Server) ListComputeStorageVolumes(c *fiber.Ctx, params apigen.ListComputeStorageVolumesParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListVolumes(c.UserContext(), limit, offset)
	if err != nil {
		return mapComputeError(c, err)
	}
	items := make([]apigen.ComputeStorageVolume, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeStorageVolumeDTO(row))
	}
	total, err := s.computeSvc.CountVolumes(c.UserContext())
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(apigen.ComputeStorageVolumePage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeStorageVolume handles POST /api/v1/compute/storage.
func (s *Server) CreateComputeStorageVolume(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeStorageVolumeCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.CreateVolume(c.UserContext(), tid, uid, compute.CreateVolumeParams{
		Name:        req.Name,
		Description: ptrString(req.Description),
		PoolName:    ptrStringOr(req.PoolName, "default"),
		Config:      ptrStringMap(req.Config),
	})
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeStorageVolumeDTO(row))
}

// GetComputeStorageVolume handles GET /api/v1/compute/storage/{volumeId}.
func (s *Server) GetComputeStorageVolume(c *fiber.Ctx, volumeID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	_, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetVolume(c.UserContext(), volumeID)
	if err != nil {
		return mapComputeError(c, err)
	}
	return c.JSON(toComputeStorageVolumeDTO(row))
}

// DeleteComputeStorageVolume handles DELETE /api/v1/compute/storage/{volumeId}.
func (s *Server) DeleteComputeStorageVolume(c *fiber.Ctx, volumeID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteVolume(c.UserContext(), tid, uid, volumeID); err != nil {
		return mapComputeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

// toComputeInstanceDTO converts a database row to the OpenAPI ComputeInstance
// schema. Config + Devices are parsed from the cached config_json blob so
// the UI sees the structured shape (not a JSON string).
func toComputeInstanceDTO(row database.ComputeInstance) apigen.ComputeInstance {
	out := apigen.ComputeInstance{
		Id:               row.ID,
		TenantId:         row.TenantID,
		Name:             row.Name,
		Status:           row.Status,
		ImageAlias:       row.ImageAlias,
		ImageFingerprint: ptrIfNonEmpty(row.ImageFingerprint),
		Description:      ptrIfNonEmpty(row.Description),
		CreatedAt:        row.CreatedAt,
	}
	if row.Type != "" {
		t := apigen.ComputeInstanceType(row.Type)
		out.Type = &t
	}
	if row.StatusCode != 0 {
		sc := int(row.StatusCode)
		out.StatusCode = &sc
	}
	if len(row.Profiles) > 0 {
		p := row.Profiles
		out.Profiles = &p
	}
	out.UpdatedAt = &row.UpdatedAt
	if cfg, err := compute.ParseInstanceConfig(row.ConfigJson); err == nil {
		if len(cfg.Config) > 0 {
			out.Config = &cfg.Config
		}
		if len(cfg.Devices) > 0 {
			out.Devices = &cfg.Devices
		}
	}
	// WS-26: surface the cached cluster_member column. NULL on a
	// single-node daemon; the API contract carries it as a nullable
	// string so the UI can render "where does this instance live".
	if row.ClusterMember != nil {
		cm := *row.ClusterMember
		out.ClusterMember = &cm
	}
	return out
}

func toComputeImageDTO(row database.ComputeImage) apigen.ComputeImage {
	out := apigen.ComputeImage{
		Id:           row.ID,
		Alias:        row.Alias,
		Source:       apigen.ComputeImageSource(row.Source),
		Fingerprint:  ptrIfNonEmpty(row.Fingerprint),
		Architecture: ptrIfNonEmpty(row.Architecture),
		SizeBytes:    &row.SizeBytes,
		Description:  ptrIfNonEmpty(row.Description),
		CreatedAt:    &row.CreatedAt,
		UpdatedAt:    &row.UpdatedAt,
	}
	if row.Type != "" {
		t := apigen.ComputeImageType(row.Type)
		out.Type = &t
	}
	return out
}

func toComputeProfileDTO(row database.ComputeProfile) apigen.ComputeProfile {
	out := apigen.ComputeProfile{
		Id:          row.ID,
		Name:        row.Name,
		Description: ptrIfNonEmpty(row.Description),
		CreatedAt:   &row.CreatedAt,
		UpdatedAt:   &row.UpdatedAt,
	}
	if cfg, err := compute.ParseInstanceConfig(row.ConfigJson); err == nil {
		if len(cfg.Config) > 0 {
			out.Config = &cfg.Config
		}
		if len(cfg.Devices) > 0 {
			out.Devices = &cfg.Devices
		}
	}
	return out
}

func toComputeNetworkDTO(row database.ComputeNetwork) apigen.ComputeNetwork {
	out := apigen.ComputeNetwork{
		Id:           row.ID,
		Name:         row.Name,
		Description:  ptrIfNonEmpty(row.Description),
		Type:         row.Type,
		AclNames:     ptrStringSliceIfNonEmpty(row.AclNames),
		ForwardNames: ptrStringSliceIfNonEmpty(row.ForwardNames),
		CreatedAt:    &row.CreatedAt,
		UpdatedAt:    &row.UpdatedAt,
	}
	if len(row.ConfigJson) > 0 && string(row.ConfigJson) != "{}" {
		cfg := map[string]string{}
		// best-effort decode; config_json is a free-form map.
		_ = decodeStringMap(row.ConfigJson, &cfg)
		if len(cfg) > 0 {
			out.Config = &cfg
		}
	}
	return out
}

func toComputeStorageVolumeDTO(row database.ComputeStorageVolume) apigen.ComputeStorageVolume {
	out := apigen.ComputeStorageVolume{
		Id:          row.ID,
		Name:        row.Name,
		Description: ptrIfNonEmpty(row.Description),
		Type:        ptrIfNonEmpty(row.Type),
		PoolName:    row.PoolName,
		CreatedAt:   &row.CreatedAt,
		UpdatedAt:   &row.UpdatedAt,
	}
	if len(row.ConfigJson) > 0 && string(row.ConfigJson) != "{}" {
		cfg := map[string]string{}
		_ = decodeStringMap(row.ConfigJson, &cfg)
		if len(cfg) > 0 {
			out.Config = &cfg
		}
	}
	return out
}

// decodeStringMap is a tiny helper that json.Unmarshal's raw into a string
// map. Errors are swallowed by the caller (the DTO degrades to a nil map).
func decodeStringMap(raw []byte, dst *map[string]string) error {
	if len(raw) == 0 {
		return nil
	}
	return jsonDecodeStringMap(raw, dst)
}

// jsonDecodeStringMap is split out so the lint pass does not flag an unused
// import for encoding/json when other helpers in this file grow.
func jsonDecodeStringMap(raw []byte, dst *map[string]string) error {
	return jsonUnmarshal(raw, dst)
}

// ptr helpers (kept local to avoid coupling to other handler files).

func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func ptrStringOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func ptrStringMap(p *map[string]string) map[string]string {
	if p == nil {
		return nil
	}
	return *p
}

func ptrDevicesMap(p *map[string]map[string]string) map[string]map[string]string {
	if p == nil {
		return nil
	}
	return *p
}

func ptrStringSlice(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

func ptrInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func ptrInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ptrStringSliceIfNonEmpty returns &s when the slice is non-empty, nil
// otherwise. Matches the *[]string shape the generated DTO uses for
// optional slice fields.
func ptrStringSliceIfNonEmpty(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

func ptrComputeType(p *apigen.ComputeInstanceCreateRequestType) apigen.ComputeInstanceCreateRequestType {
	if p == nil {
		return apigen.ComputeInstanceCreateRequestTypeContainer
	}
	return *p
}

func ptrImageType(p *apigen.ComputeImageUploadRequestType) apigen.ComputeImageUploadRequestType {
	if p == nil {
		return apigen.ComputeImageUploadRequestTypeContainer
	}
	return *p
}

// jsonUnmarshal is a tiny wrapper to keep encoding/json referenced from
// compute_handlers.go (the file uses it via decodeStringMap). Centralising
// here keeps the lint pass happy as the file grows.
func jsonUnmarshal(raw []byte, dst any) error {
	return json.Unmarshal(raw, dst)
}

// ensure unused imports stay imported even as the file grows; the helpers
// above reference every import declared at the top of this file.
var _ = strings.Contains
