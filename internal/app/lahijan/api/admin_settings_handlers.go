// Package api: admin_settings_handlers.go implements the runtime platform
// settings endpoints, gated by platform.settings.manage:
//
//	GET /api/v1/admin/settings -> GetPlatformSettings
//	PUT /api/v1/admin/settings -> UpdatePlatformSettings
package api

import (
	"github.com/gofiber/fiber/v2"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/settings"
)

func (s *Server) settingsDisabled(c *fiber.Ctx) bool {
	if s.settingsSvc != nil {
		return false
	}
	_ = SendNotImplemented(c, i18n.T(c.UserContext(), "directory.err_disabled", nil))
	return true
}

// GetPlatformSettings handles GET /api/v1/admin/settings.
func (s *Server) GetPlatformSettings(c *fiber.Ctx) error {
	if s.settingsDisabled(c) {
		return nil
	}
	st, err := s.settingsSvc.Get(c.UserContext())
	if err != nil {
		logUnexpectedError(c, "admin.settings.get", err)
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(toPlatformSettingsDTO(st))
}

// UpdatePlatformSettings handles PUT /api/v1/admin/settings.
func (s *Server) UpdatePlatformSettings(c *fiber.Ctx) error {
	if s.settingsDisabled(c) {
		return nil
	}
	ctx := c.UserContext()
	actor, ok := currentUserID(c)
	if !ok {
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_unauthorized", nil))
	}
	var req apigen.PlatformSettingsUpdate
	if err := c.BodyParser(&req); err != nil || (req.RegistrationEnabled == nil && !boolPtrOr(req.ResetRegistration, false)) {
		return SendBadRequest(c, i18n.T(ctx, "admin.err_settings_bad_request", nil), nil)
	}
	var (
		st  settings.State
		err error
	)
	switch {
	case boolPtrOr(req.ResetRegistration, false):
		st, err = s.settingsSvc.ClearRegistration(ctx, actor)
	default:
		st, err = s.settingsSvc.SetRegistration(ctx, actor, *req.RegistrationEnabled)
	}
	if err != nil {
		logUnexpectedError(c, "admin.settings.update", err)
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	return c.JSON(toPlatformSettingsDTO(st))
}

func toPlatformSettingsDTO(st settings.State) apigen.PlatformSettings {
	return apigen.PlatformSettings{
		RegistrationEnabled:    st.RegistrationEnabled,
		RegistrationDefault:    st.RegistrationDefault,
		RegistrationOverridden: st.RegistrationOverridden,
	}
}
