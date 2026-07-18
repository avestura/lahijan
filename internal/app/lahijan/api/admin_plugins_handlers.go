// Package api: admin_plugins_handlers.go implements the OpenAPI-derived
// admin endpoints for installing and managing WASM plugins (WS-10a).
//
// Seven endpoints, all platform-admin-only (the path-aware AuditGate in
// router.go runs RequirePerm with the appropriate plugins.* slug before
// the request reaches the handler):
//
//	GET    /api/v1/admin/plugins                                          -> ListAdminPlugins
//	POST   /api/v1/admin/plugins/upload (multipart)                       -> UploadAdminPlugin
//	GET    /api/v1/admin/plugins/{pluginId}                               -> GetAdminPlugin
//	DELETE /api/v1/admin/plugins/{pluginId}                               -> DeleteAdminPlugin
//	POST   /api/v1/admin/plugins/{pluginId}/permissions/{p}/{act}        -> SetAdminPluginPermission
//	POST   /api/v1/admin/plugins/{pluginId}/enable                        -> EnableAdminPlugin
//	POST   /api/v1/admin/plugins/{pluginId}/disable                       -> DisableAdminPlugin
//
// Handlers are intentionally thin (parse -> installer -> render). The
// installer.Service owns the audit emission so the same code path serves
// the HTTP API and any future CLI / webhook installer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/conf"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/manifest"
	"github.com/gofiber/fiber/v2"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultAdminPluginsPageSize is the page size used when the caller does
// not pass one. Mirrors the audit log default so the UI's paginator can
// share a constant.
const DefaultAdminPluginsPageSize = 50

// MaxAdminPluginsPageSize caps a misbehaving client's page size.
const MaxAdminPluginsPageSize = 200

// MaxManifestBytes caps the accepted manifest upload size. The manifest is
// YAML text and should be a few hundred bytes for any reasonable plugin;
// 64 KiB is generous while still preventing a single upload from
// dominating memory.
const MaxManifestBytes = 65536

// ListAdminPlugins handles GET /api/v1/admin/plugins.
func (s *Server) ListAdminPlugins(c *fiber.Ctx, params apigen.ListAdminPluginsParams) error {
	if s.pluginsRepo == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.plugins.list")
	defer span.End()

	limit, offset := adminPluginsPageParams(params.Limit, params.Offset)
	rows, err := s.pluginsRepo.ListGlobal(ctx, int32(limit), int32(offset))
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	total, err := s.pluginsRepo.CountGlobal(ctx)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	items := make([]apigen.AdminPlugin, 0, len(rows))
	for i := range rows {
		// List view does NOT include grants or manifest (cheaper + smaller
		// payload). The detail endpoint enriches a single row.
		items = append(items, toAdminPluginDTO(&rows[i], nil, false))
	}
	return c.JSON(apigen.AdminPluginPage{
		Items: items, Total: total, Limit: limit, Offset: offset,
	})
}

// UploadAdminPlugin handles POST /api/v1/admin/plugins/upload (multipart).
//
// Accepts two parts: `wasm` (binary) and `manifest` (YAML text). The
// wasm.part is capped at conf.wasm.maxModuleSize; the manifest at
// MaxManifestBytes. Returns the new plugin row in "pending" status.
func (s *Server) UploadAdminPlugin(c *fiber.Ctx) error {
	if s.pluginSvc == nil || s.pluginsRepo == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.plugins.upload")
	defer span.End()

	// wasm part: binary, capped at conf.wasm.maxModuleSize.
	wasmFile, err := c.FormFile("wasm")
	if err != nil {
		return SendBadRequest(c, i18n.T(ctx, "plugins.err_missing_wasm", nil), nil)
	}
	if max := conf.GetWasmMaxModuleSize(); max > 0 && int(wasmFile.Size) > max {
		return SendError(c, fiber.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			i18n.T(ctx, "plugins.err_wasm_too_large", map[string]any{"Max": max}), nil)
	}
	wasmSrc, err := wasmFile.Open()
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	defer func() { _ = wasmSrc.Close() }()
	maxWasm := int64(conf.GetWasmMaxModuleSize())
	if maxWasm <= 0 {
		maxWasm = 10 * 1024 * 1024 // 10 MiB fallback if conf is unset
	}
	wasmBytes, err := io.ReadAll(io.LimitReader(wasmSrc, maxWasm+1))
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}

	// manifest part: YAML text, capped at MaxManifestBytes.
	manifestFile, err := c.FormFile("manifest")
	if err != nil {
		return SendBadRequest(c, i18n.T(ctx, "plugins.err_missing_manifest", nil), nil)
	}
	if manifestFile.Size > MaxManifestBytes {
		return SendBadRequest(c,
			i18n.T(ctx, "plugins.err_manifest_too_large", nil), nil)
	}
	manifestSrc, err := manifestFile.Open()
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	defer func() { _ = manifestSrc.Close() }()
	manifestBytes, err := io.ReadAll(io.LimitReader(manifestSrc, MaxManifestBytes+1))
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	m, err := manifest.Parse(manifestBytes)
	if err != nil {
		return SendBadRequest(c, err.Error(), nil)
	}

	uid, _ := currentUserID(c)
	tidPtr, _ := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)
	row, err := s.pluginSvc.Upload(ctx, installer.UploadParams{
		TenantID:    tidPtr,
		ActorUserID: uid,
		WasmBytes:   wasmBytes,
		Manifest:    m,
		RequestID:   &rid,
	})
	if err != nil {
		return translateInstallerError(c, err)
	}
	grants, err := s.pluginsRepo.ListPermissions(ctx, row.ID)
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	return c.Status(fiber.StatusCreated).JSON(toAdminPluginDTO(&row, grants, true))
}

// GetAdminPlugin handles GET /api/v1/admin/plugins/{pluginId}.
func (s *Server) GetAdminPlugin(c *fiber.Ctx, pluginID openapi_types.UUID) error {
	if s.pluginsRepo == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.plugins.get")
	defer span.End()
	row, err := s.pluginsRepo.Get(ctx, pluginID)
	if err != nil {
		return translateRepoError(c, err)
	}
	grants, err := s.pluginsRepo.ListPermissions(ctx, row.ID)
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	return c.JSON(toAdminPluginDTO(&row, grants, true))
}

// DeleteAdminPlugin handles DELETE /api/v1/admin/plugins/{pluginId}.
func (s *Server) DeleteAdminPlugin(c *fiber.Ctx, pluginID openapi_types.UUID) error {
	if s.pluginSvc == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.plugins.delete")
	defer span.End()
	uid, _ := currentUserID(c)
	tenantPtr, _ := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)
	if err := s.pluginSvc.Delete(ctx, pluginID, uid, tenantPtr, &rid); err != nil {
		return translateInstallerError(c, err)
	}
	return c.JSON(apigen.MessageResponse{
		Message: i18n.T(ctx, "plugins.msg_deleted", nil),
	})
}

// SetAdminPluginPermission handles POST
// /api/v1/admin/plugins/{pluginId}/permissions/{permission}/{action}.
//
// {action} is "grant" or "revoke". Returns the updated grant list.
func (s *Server) SetAdminPluginPermission(
	c *fiber.Ctx,
	pluginID openapi_types.UUID,
	permission string,
	action apigen.SetAdminPluginPermissionParamsAction,
) error {
	if s.pluginSvc == nil || s.pluginsRepo == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.plugins.set_permission")
	defer span.End()

	uid, _ := currentUserID(c)
	tenantPtr, _ := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)

	switch action {
	case apigen.SetAdminPluginPermissionParamsActionGrant:
		if err := s.pluginSvc.Grant(ctx, pluginID, uid, permission, tenantPtr, &rid); err != nil {
			return translateInstallerError(c, err)
		}
	case apigen.SetAdminPluginPermissionParamsActionRevoke:
		if err := s.pluginSvc.Revoke(ctx, pluginID, uid, permission, tenantPtr, &rid); err != nil {
			return translateInstallerError(c, err)
		}
	default:
		return SendBadRequest(c,
			i18n.T(ctx, "plugins.err_bad_action", map[string]any{"Action": string(action)}), nil)
	}

	grants, err := s.pluginsRepo.ListPermissions(ctx, pluginID)
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		out = append(out, g.Permission)
	}
	return c.JSON(out)
}

// EnableAdminPlugin handles POST /api/v1/admin/plugins/{pluginId}/enable.
func (s *Server) EnableAdminPlugin(c *fiber.Ctx, pluginID openapi_types.UUID) error {
	return s.setPluginStatus(c, pluginID, true)
}

// DisableAdminPlugin handles POST /api/v1/admin/plugins/{pluginId}/disable.
func (s *Server) DisableAdminPlugin(c *fiber.Ctx, pluginID openapi_types.UUID) error {
	return s.setPluginStatus(c, pluginID, false)
}

func (s *Server) setPluginStatus(c *fiber.Ctx, pluginID openapi_types.UUID, enable bool) error {
	if s.pluginSvc == nil || s.pluginsRepo == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.plugins.set_status")
	defer span.End()
	uid, _ := currentUserID(c)
	tenantPtr, _ := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)

	var err error
	if enable {
		err = s.pluginSvc.Enable(ctx, pluginID, uid, tenantPtr, &rid)
	} else {
		err = s.pluginSvc.Disable(ctx, pluginID, uid, tenantPtr, &rid)
	}
	if err != nil {
		return translateInstallerError(c, err)
	}
	row, err := s.pluginsRepo.Get(ctx, pluginID)
	if err != nil {
		return translateRepoError(c, err)
	}
	grants, err := s.pluginsRepo.ListPermissions(ctx, row.ID)
	if err != nil {
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	return c.JSON(toAdminPluginDTO(&row, grants, true))
}

// adminPluginsPageParams clamps and defaults the limit/offset pair.
func adminPluginsPageParams(limit, offset *int) (int, int) {
	lim := DefaultAdminPluginsPageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxAdminPluginsPageSize {
		lim = MaxAdminPluginsPageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return lim, off
}

// toAdminPluginDTO converts a database.Plugin row + grants slice into the
// OpenAPI AdminPlugin schema. includeDetail controls whether the manifest
// is decoded into the response (the list view skips it for size).
func toAdminPluginDTO(row *database.Plugin, grants []database.PluginPermission, includeDetail bool) apigen.AdminPlugin {
	out := apigen.AdminPlugin{
		Id:          row.ID,
		TenantId:    (*openapi_types.UUID)(row.TenantID),
		Name:        row.Name,
		Version:     row.Version,
		Description: &row.Description,
		WasmHash:    row.WasmHash,
		WasmSize:    row.WasmSize,
		Status:      apigen.AdminPluginStatus(row.Status),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if includeDetail && len(row.ManifestJson) > 0 {
		if m := decodeManifestJSON(row.ManifestJson); m != nil {
			out.Manifest = m
		}
	}
	if len(grants) > 0 {
		strs := make([]string, 0, len(grants))
		for _, g := range grants {
			strs = append(strs, g.Permission)
		}
		out.Permissions = &strs
	}
	return out
}

// decodeManifestJSON unmarshals a JSONB manifest blob into a generic map
// for the API response. Returns nil on empty/invalid input so the OpenAPI
// schema's omitempty drops the field.
func decodeManifestJSON(raw []byte) *map[string]any {
	if len(raw) == 0 {
		return nil
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return &map[string]any{"_raw": string(raw)}
	}
	if len(m) == 0 {
		return nil
	}
	return &m
}

// translateInstallerError maps installer sentinels to the right envelope.
func translateInstallerError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, installer.ErrNotFound):
		return SendNotFound(c, i18n.T(ctx, "plugins.err_not_found", nil))
	case errors.Is(err, installer.ErrDuplicateUpload):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "plugins.err_duplicate", nil), nil)
	case errors.Is(err, installer.ErrManifestInvalid):
		return SendBadRequest(c, err.Error(), nil)
	case errors.Is(err, installer.ErrModuleRejected):
		return SendError(c, fiber.StatusBadRequest, CodeBadRequest,
			i18n.T(ctx, "plugins.err_module_rejected", nil),
			map[string]any{"cause": cleanErrMessage(err)})
	}
	return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
}

// translateRepoError maps the database "no rows" sentinel to 404.
func translateRepoError(c *fiber.Ctx, err error) error {
	if database.IsNoRows(err) {
		return SendNotFound(c, i18n.T(c.UserContext(), "plugins.err_not_found", nil))
	}
	return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
}

// cleanErrMessage returns the underlying error message without the
// wrapping sentinel prefix. Used so the API envelope surfaces the wazero
// compile error to the admin (an operator-visible message) without
// leaking the installer sentinel slug.
func cleanErrMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Strip the leading "installer: <sentinel>:" prefix the installer adds.
	if i := strings.Index(msg, ": "); i >= 0 {
		rest := msg[i+2:]
		if j := strings.Index(rest, ": "); j >= 0 {
			return strings.TrimSpace(rest[j+2:])
		}
		return strings.TrimSpace(rest)
	}
	return msg
}

// _ keeps the context import alive even when the only use is inside a
// tracing span closure (Go's import linter is conservative about that).
var _ = context.Background
