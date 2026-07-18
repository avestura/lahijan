// Package api: admin_marketplace_handlers.go implements the OpenAPI-derived
// admin endpoints for browsing and installing from the plugin marketplace
// (WS-10c).
//
// Four endpoints, all platform-admin-only (the path-aware AuditGate in
// router.go runs RequirePerm with plugins.install before the request
// reaches the handler):
//
//	GET  /api/v1/admin/marketplace                          -> ListAdminMarketplace
//	GET  /api/v1/admin/marketplace/{name}                   -> GetAdminMarketplaceEntry
//	POST /api/v1/admin/plugins/install/{name}               -> InstallAdminPluginFromMarketplace
//	POST /api/v1/admin/plugins/upgrade/{name}               -> UpgradeAdminPluginFromMarketplace
//
// Handlers are intentionally thin (parse -> marketplace.Service ->
// render). The marketplace.Service owns the audit emission so the same
// code path serves the HTTP API and any future CLI / webhook installer.
package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/installer"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/marketplace"
)

// ListAdminMarketplace handles GET /api/v1/admin/marketplace.
func (s *Server) ListAdminMarketplace(c *fiber.Ctx) error {
	if s.marketplaceSvc == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.marketplace.list")
	defer span.End()

	entries, err := s.marketplaceSvc.List(ctx)
	if err != nil {
		return translateMarketplaceError(c, err)
	}
	out := make([]apigen.AdminMarketplaceEntry, 0, len(entries))
	for i := range entries {
		out = append(out, toMarketplaceEntryDTO(&entries[i]))
	}
	return c.JSON(apigen.AdminMarketplacePage{Items: out})
}

// GetAdminMarketplaceEntry handles GET /api/v1/admin/marketplace/{name}.
func (s *Server) GetAdminMarketplaceEntry(c *fiber.Ctx, name string) error {
	if s.marketplaceSvc == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.marketplace.get")
	defer span.End()

	entry, err := s.marketplaceSvc.Get(ctx, name)
	if err != nil {
		return translateMarketplaceError(c, err)
	}
	return c.JSON(toMarketplaceEntryDTO(&entry))
}

// InstallAdminPluginFromMarketplace handles
// POST /api/v1/admin/plugins/install/{name}.
func (s *Server) InstallAdminPluginFromMarketplace(c *fiber.Ctx, name string) error {
	if s.marketplaceSvc == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.marketplace.install")
	defer span.End()

	uid, _ := currentUserID(c)
	tidPtr, _ := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)
	row, err := s.marketplaceSvc.Install(ctx, name, marketplace.InstallParams{
		TenantID:    tidPtr,
		ActorUserID: uid,
		RequestID:   &rid,
	})
	if err != nil {
		return translateMarketplaceError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toAdminPluginDTO(&row, nil, false))
}

// UpgradeAdminPluginFromMarketplace handles
// POST /api/v1/admin/plugins/upgrade/{name}.
func (s *Server) UpgradeAdminPluginFromMarketplace(c *fiber.Ctx, name string) error {
	if s.marketplaceSvc == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "plugins.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.marketplace.upgrade")
	defer span.End()

	uid, _ := currentUserID(c)
	tidPtr, _ := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)
	res, err := s.marketplaceSvc.Upgrade(ctx, name, marketplace.InstallParams{
		TenantID:    tidPtr,
		ActorUserID: uid,
		RequestID:   &rid,
	})
	if err != nil {
		return translateMarketplaceError(c, err)
	}
	plugin := toAdminPluginDTO(&res.New, nil, false)
	// The detail view would include grants; we render the upgrade
	// bundle without grants to keep the response shape uniform across
	// the install + upgrade paths. The admin can GET
	// /api/v1/admin/plugins/{id} for the full grant list.
	return c.JSON(apigen.AdminPluginUpgradeResult{
		Plugin:          plugin,
		OldId:           res.OldID,
		PreservedGrants: res.PreservedGrants,
		DroppedGrants:   res.DroppedGrants,
		NewPermissions:  res.NewPermissions,
	})
}

// toMarketplaceEntryDTO converts a marketplace.Entry into the OpenAPI
// AdminMarketplaceEntry schema. Pointer-valued fields are populated only
// when the source is non-empty so the schema's omitempty drops them.
func toMarketplaceEntryDTO(e *marketplace.Entry) apigen.AdminMarketplaceEntry {
	out := apigen.AdminMarketplaceEntry{
		Name:    e.Name,
		Version: e.Version,
		Sha256:  e.SHA256,
		Source: struct {
			GitRef *string                                `json:"gitRef,omitempty"`
			GitURL *string                                `json:"gitURL,omitempty"`
			Path   *string                                `json:"path,omitempty"`
			Repo   apigen.AdminMarketplaceEntrySourceRepo `json:"repo"`
		}{
			Repo: apigen.AdminMarketplaceEntrySourceRepo(e.Source.Repo),
		},
	}
	if e.Description != "" {
		d := e.Description
		out.Description = &d
	}
	if e.Author != "" {
		a := e.Author
		out.Author = &a
	}
	if e.License != "" {
		l := e.License
		out.License = &l
	}
	if e.Homepage != "" {
		h := e.Homepage
		out.Homepage = &h
	}
	if len(e.Permissions) > 0 {
		perms := append([]string(nil), e.Permissions...)
		out.Permissions = &perms
	}
	if e.Source.Path != "" {
		p := e.Source.Path
		out.Source.Path = &p
	}
	if e.Source.GitURL != "" {
		u := e.Source.GitURL
		out.Source.GitURL = &u
	}
	if e.Source.GitRef != "" {
		r := e.Source.GitRef
		out.Source.GitRef = &r
	}
	return out
}

// translateMarketplaceError maps marketplace + installer sentinels to the
// right HTTP envelope. Falls through to translateInstallerError for
// shared sentinels (ErrDuplicateUpload, ErrNotFound, etc.).
func translateMarketplaceError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, marketplace.ErrNotFound):
		return SendNotFound(c, i18n.T(ctx, "marketplace.err_not_found", nil))
	case errors.Is(err, marketplace.ErrMarketplaceDisabled):
		return SendNotImplemented(c, i18n.T(ctx, "plugins.err_disabled", nil))
	case errors.Is(err, marketplace.ErrHashMismatch):
		return SendError(c, fiber.StatusUnprocessableEntity, CodeBadRequest,
			i18n.T(ctx, "marketplace.err_hash_mismatch", nil), nil)
	case errors.Is(err, installer.ErrNotInstalled):
		return SendNotFound(c, i18n.T(ctx, "marketplace.err_not_installed", nil))
	case errors.Is(err, installer.ErrSameVersion):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "marketplace.err_same_version", nil), nil)
	case errors.Is(err, installer.ErrDowngrade):
		return SendError(c, fiber.StatusBadRequest, CodeBadRequest,
			i18n.T(ctx, "marketplace.err_downgrade", nil), nil)
	}
	return translateInstallerError(c, err)
}
