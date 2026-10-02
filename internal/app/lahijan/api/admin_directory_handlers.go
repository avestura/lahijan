// Package api: admin_directory_handlers.go implements the external-directory
// endpoints (LDAP / SAML connections), all gated by platform.directory.manage:
//
//	GET    /api/v1/admin/directory/connections                       -> ListDirectoryConnections
//	POST   /api/v1/admin/directory/connections                       -> CreateDirectoryConnection
//	GET    /api/v1/admin/directory/connections/{connectionId}        -> GetDirectoryConnection
//	PATCH  /api/v1/admin/directory/connections/{connectionId}        -> UpdateDirectoryConnection
//	DELETE /api/v1/admin/directory/connections/{connectionId}        -> DeleteDirectoryConnection
//	POST   /api/v1/admin/directory/connections/{connectionId}/sync   -> SyncDirectoryConnection
//	GET    /api/v1/admin/directory/connections/{connectionId}/groups -> ListDirectoryGroups
//	POST   /api/v1/admin/directory/test                              -> TestDirectoryConnection
package api

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/directory"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
)

func (s *Server) directoryDisabled(c *fiber.Ctx) bool {
	if s.directorySvc != nil {
		return false
	}
	_ = SendNotImplemented(c, i18n.T(c.UserContext(), "directory.err_disabled", nil))
	return true
}

// ListDirectoryConnections handles GET /api/v1/admin/directory/connections.
func (s *Server) ListDirectoryConnections(c *fiber.Ctx) error {
	if s.directoryDisabled(c) {
		return nil
	}
	rows, err := s.directorySvc.List(c.UserContext())
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	items := make([]apigen.DirectoryConnection, 0, len(rows))
	for _, r := range rows {
		items = append(items, toDirectoryConnectionDTO(r))
	}
	return c.JSON(apigen.DirectoryConnectionList{Items: items})
}

// GetDirectoryConnection handles GET /api/v1/admin/directory/connections/{id}.
func (s *Server) GetDirectoryConnection(c *fiber.Ctx, id openapi_types.UUID) error {
	if s.directoryDisabled(c) {
		return nil
	}
	row, err := s.directorySvc.Get(c.UserContext(), id)
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	return c.JSON(toDirectoryConnectionDTO(row))
}

// CreateDirectoryConnection handles POST /api/v1/admin/directory/connections.
func (s *Server) CreateDirectoryConnection(c *fiber.Ctx) error {
	if s.directoryDisabled(c) {
		return nil
	}
	var req apigen.DirectoryConnectionRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "admin.err_bad_request", nil), nil)
	}
	in, err := directoryInput(req)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "admin.err_bad_request", nil), nil)
	}
	actor, _ := currentUserID(c)
	row, err := s.directorySvc.Create(c.UserContext(), actor, in)
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toDirectoryConnectionDTO(row))
}

// UpdateDirectoryConnection handles PATCH /api/v1/admin/directory/connections/{id}.
func (s *Server) UpdateDirectoryConnection(c *fiber.Ctx, id openapi_types.UUID) error {
	if s.directoryDisabled(c) {
		return nil
	}
	var req apigen.DirectoryConnectionRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "admin.err_bad_request", nil), nil)
	}
	in, err := directoryInput(req)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "admin.err_bad_request", nil), nil)
	}
	actor, _ := currentUserID(c)
	row, err := s.directorySvc.Update(c.UserContext(), actor, id, in)
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	return c.JSON(toDirectoryConnectionDTO(row))
}

// DeleteDirectoryConnection handles DELETE /api/v1/admin/directory/connections/{id}.
func (s *Server) DeleteDirectoryConnection(c *fiber.Ctx, id openapi_types.UUID) error {
	if s.directoryDisabled(c) {
		return nil
	}
	actor, _ := currentUserID(c)
	if err := s.directorySvc.Delete(c.UserContext(), actor, id); err != nil {
		return s.mapDirectoryError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// TestDirectoryConnection handles POST /api/v1/admin/directory/test.
func (s *Server) TestDirectoryConnection(c *fiber.Ctx) error {
	if s.directoryDisabled(c) {
		return nil
	}
	var req apigen.DirectoryTestRequest
	if err := c.BodyParser(&req); err != nil || (req.ConnectionId == nil && req.Connection == nil) {
		return SendBadRequest(c, i18n.T(c.UserContext(), "admin.err_bad_request", nil), nil)
	}
	var id *uuid.UUID
	if req.ConnectionId != nil {
		v := *req.ConnectionId
		id = &v
	}
	var draft *directory.Input
	if req.Connection != nil {
		in, err := directoryInput(*req.Connection)
		if err != nil {
			return SendBadRequest(c, i18n.T(c.UserContext(), "admin.err_bad_request", nil), nil)
		}
		draft = &in
	}
	actor, _ := currentUserID(c)
	res, err := s.directorySvc.Test(c.UserContext(), actor, id, draft)
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	return c.JSON(toDirectoryTestDTO(res))
}

// SyncDirectoryConnection handles POST /api/v1/admin/directory/connections/{id}/sync.
func (s *Server) SyncDirectoryConnection(c *fiber.Ctx, id openapi_types.UUID) error {
	if s.directoryDisabled(c) {
		return nil
	}
	actor, _ := currentUserID(c)
	res, err := s.directorySvc.Sync(c.UserContext(), actor, id)
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	return c.JSON(apigen.DirectorySyncResult{
		Users: res.Users, Created: res.Created, Linked: res.Linked, Skipped: res.Skipped, Groups: res.Groups,
	})
}

// ListDirectoryGroups handles GET /api/v1/admin/directory/connections/{id}/groups.
func (s *Server) ListDirectoryGroups(c *fiber.Ctx, id openapi_types.UUID, params apigen.ListDirectoryGroupsParams) error {
	if s.directoryDisabled(c) {
		return nil
	}
	limit, offset := pageParams(params.Limit, params.Offset)
	rows, total, err := s.directorySvc.ListGroups(c.UserContext(), id, limit, offset)
	if err != nil {
		return s.mapDirectoryError(c, err)
	}
	items := make([]apigen.DirectoryGroup, 0, len(rows))
	for _, r := range rows {
		desc := r.Description
		items = append(items, apigen.DirectoryGroup{
			Id: r.ID, ExternalId: r.ExternalID, Name: r.Name, Description: &desc, MemberCount: int(r.MemberCount),
		})
	}
	return c.JSON(apigen.DirectoryGroupPage{Items: items, Total: total, Limit: int(limit), Offset: int(offset)})
}

// --- helpers -------------------------------------------------------------

func directoryInput(req apigen.DirectoryConnectionRequest) (directory.Input, error) {
	cfg, err := json.Marshal(req.Config)
	if err != nil {
		return directory.Input{}, err
	}
	return directory.Input{
		Kind:         string(req.Kind),
		Name:         req.Name,
		Enabled:      boolPtrOr(req.Enabled, true),
		Config:       cfg,
		BindPassword: req.BindPassword,
	}, nil
}

func toDirectoryConnectionDTO(r directory.Connection) apigen.DirectoryConnection {
	cfg := map[string]any{}
	_ = json.Unmarshal(r.Config, &cfg)
	dto := apigen.DirectoryConnection{
		Id: r.ID, Kind: apigen.DirectoryConnectionKind(r.Kind), Name: r.Name, Enabled: r.Enabled,
		Config: cfg, HasSecret: r.HasSecret, Active: r.Active, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.ActivationError != "" {
		msg := r.ActivationError
		dto.ActivationError = &msg
	}
	if r.LastSyncAt != nil {
		st := apigen.DirectoryConnectionLastSyncStatus(r.LastSyncStatus)
		users, groups := r.LastSyncUsers, r.LastSyncGroups
		dto.LastSyncAt = r.LastSyncAt
		dto.LastSyncStatus = &st
		dto.LastSyncUsers = &users
		dto.LastSyncGroups = &groups
		if r.LastSyncMessage != "" {
			msg := r.LastSyncMessage
			dto.LastSyncMessage = &msg
		}
	}
	return dto
}

func toDirectoryTestDTO(r directory.TestResult) apigen.DirectoryTestResult {
	dto := apigen.DirectoryTestResult{Ok: r.OK, Code: apigen.DirectoryTestResultCode(r.Code)}
	if r.Detail != "" {
		d := r.Detail
		dto.Detail = &d
	}
	if r.OK && r.EntityID == "" {
		u, g := r.Users, r.Groups
		dto.Users, dto.Groups = &u, &g
	}
	if r.EntityID != "" {
		e, sso := r.EntityID, r.SSOURL
		dto.EntityId, dto.SsoUrl = &e, &sso
	}
	return dto
}

func (s *Server) mapDirectoryError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, directory.ErrNotFound):
		return SendNotFound(c, i18n.T(ctx, "directory.err_not_found", nil))
	case errors.Is(err, directory.ErrInvalid):
		return SendBadRequest(c, i18n.T(ctx, "directory.err_invalid", nil), map[string]any{"reason": err.Error()})
	case errors.Is(err, directory.ErrNameTaken):
		return SendError(c, fiber.StatusConflict, CodeConflict, i18n.T(ctx, "directory.err_name_taken", nil), nil)
	case errors.Is(err, directory.ErrSyncUnsupported):
		return SendError(c, fiber.StatusConflict, CodeConflict, i18n.T(ctx, "directory.err_sync_unsupported", nil), nil)
	case errors.Is(err, directory.ErrCryptoRequired):
		return SendError(c, fiber.StatusInternalServerError, CodeInternal, i18n.T(ctx, "directory.err_crypto_required", nil), nil)
	case errors.Is(err, directory.ErrSyncFailed), errors.Is(err, directory.ErrLDAPConnect):
		return SendError(c, fiber.StatusBadGateway, "bad_gateway", i18n.T(ctx, "directory.err_sync_failed", nil),
			map[string]any{"reason": err.Error()})
	}
	logUnexpectedError(c, "directory", err)
	return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
}
