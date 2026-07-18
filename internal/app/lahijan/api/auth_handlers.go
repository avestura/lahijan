// Package api: auth_handlers.go implements the OpenAPI-derived auth endpoints.
// Each handler is thin: decode -> call service -> set/clear cookies -> render.
// Errors are mapped to the standard envelope with localizable messages via i18n.
package api

import (
	"errors"
	"net/netip"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/pat"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/session"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// extractUA returns a best-effort user-agent + ip pointer from the request,
// used for session bookkeeping (sessions.user_agent, sessions.ip_address).
func extractUA(c *fiber.Ctx) (*string, *netip.Addr) {
	ua := c.Get("User-Agent")
	var uaPtr *string
	if ua != "" {
		uaPtr = &ua
	}
	var ipPtr *netip.Addr
	if ip, err := netip.ParseAddr(c.IP()); err == nil {
		ipPtr = &ip
	}
	return uaPtr, ipPtr
}

// Register handles POST /api/v1/auth/register.
func (s *Server) Register(c *fiber.Ctx) error {
	var req apigen.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	ua, ip := extractUA(c)
	sess, err := s.sessionSvc.Register(c.UserContext(), session.RegisterInput{
		Email:       string(req.Email),
		Password:    req.Password,
		DisplayName: req.DisplayName,
		Locale:      strPtrOr(req.Locale, "en"),
		UserAgent:   ua,
		IPAddress:   ip,
	})
	if err != nil {
		return s.mapAuthError(c, err)
	}
	setSessionCookie(c, s.cookies, sess.CookieValue, sess.Refresh.Raw)
	user, err := s.users.GetByID(c.UserContext(), sess.UserID)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.Status(fiber.StatusCreated).JSON(apigen.AuthResponse{User: toUserDTO(user)})
}

// Login handles POST /api/v1/auth/login.
//
// When the MFA service is wired AND the user requires MFA (has an enrolled
// factor OR any tenant membership requires it), returns 202 Accepted with
// a pending_session_token; the client must complete the challenge at
// /api/v1/auth/mfa/challenge to obtain the real session. Otherwise behaves
// as the WS-06 flow: returns 200 with session cookies.
func (s *Server) Login(c *fiber.Ctx) error {
	var req apigen.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	ua, ip := extractUA(c)

	// MFA-aware path: verify credentials, decide between immediate session
	// and pending MFA challenge.
	if s.mfaSvc != nil {
		user, err := s.sessionSvc.VerifyCredentials(c.UserContext(), string(req.Email), req.Password)
		if err != nil {
			return s.mapAuthError(c, err)
		}
		required, err := s.mfaSvc.IsMFARequired(c.UserContext(), user.ID)
		if err != nil {
			return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
		}
		if required {
			pending, perr := s.mfaSvc.BeginLogin(c.UserContext(), user.ID, ua, ip)
			if perr != nil {
				return s.mapMFAError(c, perr)
			}
			if pending.Token == "" {
				// Should not happen: IsMFARequired returned true so
				// BeginLogin must either issue a token or error.
				return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
			}
			factors := []apigen.MFAChallengeRequiredEnrolledFactors{}
			if s.mfaSvc.HasTOTP(c.UserContext(), user.ID) {
				factors = append(factors, apigen.MFAChallengeRequiredEnrolledFactors("totp"))
			}
			if s.mfaSvc.HasWebAuthn(c.UserContext(), user.ID) {
				factors = append(factors, apigen.MFAChallengeRequiredEnrolledFactors("webauthn"))
			}
			return c.Status(fiber.StatusAccepted).JSON(apigen.MFAChallengeRequired{
				MfaRequired:         true,
				PendingSessionToken: pending.Token,
				EnrolledFactors:     &factors,
			})
		}
		// Not required: open the real session now.
		sess, err := s.sessionSvc.OpenForExistingUser(c.UserContext(), user.ID, ua, ip)
		if err != nil {
			return s.mapAuthError(c, err)
		}
		setSessionCookie(c, s.cookies, sess.CookieValue, sess.Refresh.Raw)
		fetched, err := s.users.GetByID(c.UserContext(), sess.UserID)
		if err != nil {
			return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
		}
		return c.JSON(apigen.AuthResponse{User: toUserDTO(fetched)})
	}

	// Legacy path: MFA service not wired (dev). Behaves exactly as WS-06.
	sess, err := s.sessionSvc.Login(c.UserContext(), session.LoginInput{
		Email:     string(req.Email),
		Password:  req.Password,
		UserAgent: ua,
		IPAddress: ip,
	})
	if err != nil {
		return s.mapAuthError(c, err)
	}
	setSessionCookie(c, s.cookies, sess.CookieValue, sess.Refresh.Raw)
	user, err := s.users.GetByID(c.UserContext(), sess.UserID)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(apigen.AuthResponse{User: toUserDTO(user)})
}

// Logout handles POST /api/v1/auth/logout.
func (s *Server) Logout(c *fiber.Ctx) error {
	var req apigen.LogoutRequest
	_ = c.BodyParser(&req) // optional body
	raw := strPtrOr(req.RefreshToken, "")
	if raw == "" {
		raw = c.Cookies(s.cookies.RefreshName)
	}
	if raw == "" {
		// No refresh token to revoke; still clear the cookies and report ok.
		clearSessionCookie(c, s.cookies)
		return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_logged_out", nil)})
	}
	_ = s.sessionSvc.Logout(c.UserContext(), raw)
	clearSessionCookie(c, s.cookies)
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_logged_out", nil)})
}

// Refresh handles POST /api/v1/auth/refresh.
func (s *Server) Refresh(c *fiber.Ctx) error {
	var req apigen.RefreshRequest
	_ = c.BodyParser(&req)
	raw := strPtrOr(req.RefreshToken, "")
	if raw == "" {
		raw = c.Cookies(s.cookies.RefreshName)
	}
	if raw == "" {
		return SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_invalid_token", nil))
	}
	ua, ip := extractUA(c)
	sess, err := s.sessionSvc.Refresh(c.UserContext(), raw, ua, ip)
	if err != nil {
		clearSessionCookie(c, s.cookies)
		return s.mapAuthError(c, err)
	}
	// On refresh the session cookie stays the same (CookieValue == ""); only
	// the refresh-token cookie rotates.
	setSessionCookie(c, s.cookies, sess.CookieValue, sess.Refresh.Raw)
	user, err := s.users.GetByID(c.UserContext(), sess.UserID)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(apigen.AuthResponse{User: toUserDTO(user)})
}

// VerifyEmail handles POST /api/v1/auth/verify-email.
func (s *Server) VerifyEmail(c *fiber.Ctx) error {
	var req apigen.TokenRequest
	if err := c.BodyParser(&req); err != nil || req.Token == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if err := s.emailSvc.Verify(c.UserContext(), req.Token); err != nil {
		return s.mapAuthError(c, err)
	}
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_email_verified", nil)})
}

// ResendVerification handles POST /api/v1/auth/resend-verification.
func (s *Server) ResendVerification(c *fiber.Ctx) error {
	var req apigen.EmailRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	// Look up the user so we can resend; respond identically whether or not
	// they exist (no account enumeration).
	if u, err := s.users.GetByEmail(c.UserContext(), string(req.Email)); err == nil {
		_ = s.emailSvc.SendVerification(c.UserContext(), u.ID)
	}
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_email_sent", nil)})
}

// RequestPasswordReset handles POST /api/v1/auth/password-reset/request.
func (s *Server) RequestPasswordReset(c *fiber.Ctx) error {
	var req apigen.EmailRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	_ = s.emailSvc.RequestPasswordReset(c.UserContext(), string(req.Email))
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_email_sent", nil)})
}

// ConfirmPasswordReset handles POST /api/v1/auth/password-reset/confirm.
func (s *Server) ConfirmPasswordReset(c *fiber.Ctx) error {
	var req apigen.PasswordResetConfirmRequest
	if err := c.BodyParser(&req); err != nil || req.Token == "" || req.NewPassword == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if err := s.emailSvc.ConfirmPasswordReset(c.UserContext(), req.Token, req.NewPassword); err != nil {
		return s.mapAuthError(c, err)
	}
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_password_reset", nil)})
}

// GetCurrentUser handles GET /api/v1/auth/me.
func (s *Server) GetCurrentUser(c *fiber.Ctx) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	user, err := s.users.GetByID(c.UserContext(), uid)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_not_found", nil))
	}
	return c.JSON(toUserDTO(user))
}

// UpdateCurrentUser handles PATCH /api/v1/auth/me. It applies display name /
// locale changes immediately; a password change requires currentPassword; a
// newEmail triggers an email-change confirmation link.
func (s *Server) UpdateCurrentUser(c *fiber.Ctx) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	var req apigen.UpdateMeRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}

	if req.DisplayName != nil {
		if err := s.users.UpdateDisplayName(c.UserContext(), uid, req.DisplayName); err != nil {
			return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
		}
	}
	if req.Locale != nil && *req.Locale != "" {
		if err := s.users.UpdateLocale(c.UserContext(), uid, *req.Locale); err != nil {
			return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
		}
	}
	if req.NewPassword != nil && *req.NewPassword != "" {
		if req.CurrentPassword == nil || *req.CurrentPassword == "" {
			return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), map[string]any{"field": "currentPassword"})
		}
		if err := s.changePassword(c, uid, *req.CurrentPassword, *req.NewPassword); err != nil {
			return s.mapAuthError(c, err)
		}
	}
	if req.NewEmail != nil && string(*req.NewEmail) != "" {
		if err := s.emailSvc.RequestEmailChange(c.UserContext(), uid, string(*req.NewEmail)); err != nil {
			return s.mapAuthError(c, err)
		}
	}

	user, err := s.users.GetByID(c.UserContext(), uid)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(toUserDTO(user))
}

// changePassword verifies the current password, strength-validates the new one,
// and delegates the hash + persist to the session service (which owns the
// hasher). Used by PATCH /me.
func (s *Server) changePassword(c *fiber.Ctx, uid uuid.UUID, current, newPw string) error {
	return s.sessionSvc.ChangePassword(c.UserContext(), uid, current, newPw)
}

// ListPersonalAccessTokens handles GET /api/v1/auth/personal-access-tokens.
func (s *Server) ListPersonalAccessTokens(c *fiber.Ctx) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	rows, err := s.patSvc.List(c.UserContext(), uid)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	out := make([]apigen.PersonalAccessToken, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPATDTO(r, false))
	}
	return c.JSON(out)
}

// CreatePersonalAccessToken handles POST /api/v1/auth/personal-access-tokens.
func (s *Server) CreatePersonalAccessToken(c *fiber.Ctx) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	var req apigen.CreatePersonalAccessTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	var scopes []string
	if req.Scopes != nil {
		scopes = *req.Scopes
	}
	created, err := s.patSvc.Create(c.UserContext(), pat.CreateInput{
		UserID:    uid,
		Name:      req.Name,
		Scopes:    scopes,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return s.mapAuthError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toPATDTO(created.PersonalAccessToken, true, created.Raw))
}

// DeletePersonalAccessToken handles DELETE /api/v1/auth/personal-access-tokens/{id}.
func (s *Server) DeletePersonalAccessToken(c *fiber.Ctx, tokenID openapi_types.UUID) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	if err := s.patSvc.Revoke(c.UserContext(), tokenID, uid); err != nil {
		if errors.Is(err, pat.ErrNotFound) {
			return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_pat_not_found", nil))
		}
		return s.mapAuthError(c, err)
	}
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_pat_revoked", nil)})
}

// toPATDTO converts a database PAT row to the OpenAPI schema. includeRaw + raw
// are only set on create (the raw token is never returned again).
func toPATDTO(p database.PersonalAccessToken, includeRaw bool, raw ...string) apigen.PersonalAccessToken {
	out := apigen.PersonalAccessToken{
		Id:         p.ID,
		Name:       p.Name,
		Scopes:     p.Scopes,
		ExpiresAt:  p.ExpiresAt,
		LastUsedAt: p.LastUsedAt,
		CreatedAt:  p.CreatedAt,
	}
	if includeRaw && len(raw) > 0 {
		out.Token = &raw[0]
	}
	return out
}

// strPtrOr returns *s when non-nil, otherwise def.
func strPtrOr(s *string, def string) string {
	if s == nil || *s == "" {
		return def
	}
	return *s
}

// mapAuthError translates a service-layer sentinel into a localised envelope.
func (s *Server) mapAuthError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, session.ErrInvalidCredentials):
		return SendError(c, fiber.StatusUnauthorized, CodeUnauthorized,
			i18n.T(ctx, "auth.err_invalid_credentials", nil), nil)
	case errors.Is(err, session.ErrEmailTaken):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "auth.err_email_taken", nil), nil)
	case errors.Is(err, session.ErrUserInactive):
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(ctx, "auth.err_user_inactive", nil), nil)
	case errors.Is(err, session.ErrEmailUnverified):
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(ctx, "auth.err_email_unverified", nil), nil)
	case errors.Is(err, session.ErrRefreshReuse):
		return SendError(c, fiber.StatusUnauthorized, CodeUnauthorized,
			i18n.T(ctx, "auth.err_refresh_reuse", nil), nil)
	case errors.Is(err, session.ErrInvalidToken), errors.Is(err, email.ErrInvalidToken):
		return SendError(c, fiber.StatusUnauthorized, CodeUnauthorized,
			i18n.T(ctx, "auth.err_invalid_token", nil), nil)
	case errors.Is(err, session.ErrEmailInvalid):
		return SendBadRequest(c, i18n.T(ctx, "auth.err_email_invalid", nil), nil)
	case errors.Is(err, password.ErrPasswordTooWeak):
		return SendError(c, fiber.StatusBadRequest, CodeBadRequest,
			i18n.T(ctx, "auth.err_password_too_weak", map[string]any{"Min": 12}), nil)
	case errors.Is(err, pat.ErrNameRequired):
		return SendBadRequest(c, i18n.T(ctx, "auth.err_pat_name_required", nil), nil)
	case errors.Is(err, pat.ErrInvalidScope):
		return SendBadRequest(c, i18n.T(ctx, "auth.err_scopes_invalid", nil), nil)
	case errors.Is(err, pat.ErrNotFound):
		return SendNotFound(c, i18n.T(ctx, "auth.err_pat_not_found", nil))
	}
	return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
}
