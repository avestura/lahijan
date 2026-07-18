// Package api: mfa_handlers.go implements the OpenAPI-derived MFA endpoints
// (WS-07c). Each handler is thin: decode -> call service -> render.
//
// The login flow is split into two paths: when MFA is NOT required, the
// existing /login handler returns 200 with session cookies unchanged;
// when MFA IS required, the handler returns 202 with a pending_session_token
// the client must exchange at /api/v1/auth/mfa/challenge.
//
// The handlers live here so router.go's generated RegisterHandlers can
// reach them via the ServerInterface methods on *Server.
package api

import (
	"encoding/json"
	"errors"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/webauthn"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// mfaDisabled reports whether the MFA service is not wired (dev without
// WebAuthn config). Returns a 501 "feature disabled" envelope in that case.
func (s *Server) mfaDisabled(c *fiber.Ctx) bool {
	if s.mfaSvc != nil {
		return false
	}
	_ = SendError(c, fiber.StatusNotImplemented, CodeNotImplemented,
		i18n.T(c.UserContext(), "auth.err_mfa_disabled", nil), nil)
	return true
}

// EnrollTOTP handles POST /api/v1/me/mfa/totp/enroll.
func (s *Server) EnrollTOTP(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	user, err := s.users.GetByID(c.UserContext(), uid)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_not_found", nil))
	}
	secret, err := s.mfaSvc.EnrollTOTP(c.UserContext(), mfa.EnrollTOTPInput{
		UserID: uid,
		Email:  user.Email,
	})
	if err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.TOTPEnrollResponse{
		Secret:          secret.Raw,
		ProvisioningUri: secret.ProvisioningURI,
	})
}

// VerifyTOTP handles POST /api/v1/me/mfa/totp/verify.
func (s *Server) VerifyTOTP(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	var req apigen.VerifyTOTPJSONRequestBody
	if err := c.BodyParser(&req); err != nil || req.Code == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if err := s.mfaSvc.VerifyTOTP(c.UserContext(), uid, req.Code); err != nil {
		return s.mapMFAError(c, err)
	}
	// On first-time verification we mint a fresh batch of recovery codes
	// and return them once.
	codes, err := s.mfaSvc.RegenerateRecoveryCodes(c.UserContext(), uid)
	if err != nil {
		// Non-fatal: the user can still use TOTP; dashboard can prompt later.
		return c.JSON(apigen.MessageResponse{
			Message: i18n.T(c.UserContext(), "auth.msg_mfa_totp_confirmed", nil),
		})
	}
	return c.JSON(apigen.RecoveryCodesBatch{Codes: codes})
}

// DisableTOTP handles POST /api/v1/me/mfa/totp/disable.
func (s *Server) DisableTOTP(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	var req apigen.DisableTOTPJSONRequestBody
	if err := c.BodyParser(&req); err != nil || req.CurrentPassword == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_mfa_password_required", nil),
			map[string]any{"field": "currentPassword"})
	}
	// Re-authenticate the user before disarming MFA so a stolen session
	// cookie cannot silently disable it. VerifyCredentials handles the
	// user lookup, password verify, and the inactive / unverified checks.
	user, err := s.users.GetByID(c.UserContext(), uid)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_not_found", nil))
	}
	if _, err := s.sessionSvc.VerifyCredentials(c.UserContext(), user.Email, req.CurrentPassword); err != nil {
		return SendError(c, fiber.StatusUnauthorized, CodeUnauthorized,
			i18n.T(c.UserContext(), "auth.err_mfa_password_required", nil), nil)
	}
	if err := s.mfaSvc.DisableTOTP(c.UserContext(), uid); err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.MessageResponse{
		Message: i18n.T(c.UserContext(), "auth.msg_mfa_totp_disabled", nil),
	})
}

// BeginWebAuthnRegistration handles POST /api/v1/me/mfa/webauthn/register/begin.
func (s *Server) BeginWebAuthnRegistration(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	if !s.mfaSvc.WebAuthnEnabled() {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented,
			i18n.T(c.UserContext(), "auth.err_mfa_disabled", nil), nil)
	}
	var req apigen.BeginWebAuthnRegistrationJSONRequestBody
	_ = c.BodyParser(&req)
	creation, sess, err := s.mfaSvc.BeginWebAuthnRegistration(c.UserContext(), uid)
	if err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.WebAuthnBeginRegistrationResponse{
		PublicKey: marshalResponseMap(c, creation.Response),
		Session:   encodeWebauthnSession(sess),
	})
}

// FinishWebAuthnRegistration handles POST /api/v1/me/mfa/webauthn/register/finish.
func (s *Server) FinishWebAuthnRegistration(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	var req apigen.FinishWebAuthnRegistrationJSONRequestBody
	if err := c.BodyParser(&req); err != nil || req.Session == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	sess, err := decodeWebauthnSession(req.Session)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	body, err := json.Marshal(req.Response)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	parsed, err := webauthn.ParseCreationResponseBody(body)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if err := s.mfaSvc.FinishWebAuthnRegistration(c.UserContext(), uid, sess, parsed, req.Name); err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.MessageResponse{
		Message: i18n.T(c.UserContext(), "auth.msg_mfa_webauthn_registered", nil),
	})
}

// BeginWebAuthnLogin handles POST /api/v1/me/mfa/webauthn/login/begin.
func (s *Server) BeginWebAuthnLogin(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	if !s.mfaSvc.WebAuthnEnabled() {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented,
			i18n.T(c.UserContext(), "auth.err_mfa_disabled", nil), nil)
	}
	assertion, sess, err := s.mfaSvc.BeginWebAuthnLogin(c.UserContext(), uid)
	if err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.WebAuthnLoginBeginResponse{
		PublicKey: marshalResponseMap(c, assertion.Response),
		Session:   encodeWebauthnSession(sess),
	})
}

// FinishWebAuthnLogin handles POST /api/v1/me/mfa/webauthn/login/finish.
func (s *Server) FinishWebAuthnLogin(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	var req apigen.FinishWebAuthnLoginJSONRequestBody
	if err := c.BodyParser(&req); err != nil || req.Session == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	sess, err := decodeWebauthnSession(req.Session)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	body, err := json.Marshal(req.Response)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	parsed, err := webauthn.ParseAssertionResponseBody(body)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if _, err := s.mfaSvc.FinishWebAuthnLogin(c.UserContext(), uid, sess, parsed); err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.MessageResponse{
		Message: i18n.T(c.UserContext(), "auth.msg_mfa_webauthn_registered", nil),
	})
}

// DeleteWebAuthnCredential handles DELETE /api/v1/me/mfa/webauthn/credentials/{credentialId}.
func (s *Server) DeleteWebAuthnCredential(c *fiber.Ctx, credentialId openapi_types.UUID) error {
	_ = credentialId
	// Stub: not load-bearing for WS-07c DoD; admin-only revocation path.
	return SendNotImplemented(c, "WebAuthn credential deletion not implemented")
}

// ListMyRecoveryCodes handles GET /api/v1/me/mfa/recovery.
func (s *Server) ListMyRecoveryCodes(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	rows, err := s.mfaSvc.ListRecoveryCodes(c.UserContext(), uid)
	if err != nil {
		return s.mapMFAError(c, err)
	}
	out := apigen.RecoveryCodesListResponse{
		Total:     len(rows),
		Remaining: 0,
	}
	for _, r := range rows {
		if r.UsedAt == nil {
			out.Remaining++
		}
	}
	return c.JSON(out)
}

// RegenerateMyRecoveryCodes handles POST /api/v1/me/mfa/recovery.
func (s *Server) RegenerateMyRecoveryCodes(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	codes, err := s.mfaSvc.RegenerateRecoveryCodes(c.UserContext(), uid)
	if err != nil {
		return s.mapMFAError(c, err)
	}
	return c.JSON(apigen.RecoveryCodesBatch{Codes: codes})
}

// ChallengeMFA handles POST /api/v1/auth/mfa/challenge.
func (s *Server) ChallengeMFA(c *fiber.Ctx) error {
	if s.mfaDisabled(c) {
		return nil
	}
	var req apigen.ChallengeMFAJSONRequestBody
	if err := c.BodyParser(&req); err != nil || req.PendingSessionToken == "" || req.Kind == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	in := mfa.ChallengeInput{
		Token: req.PendingSessionToken,
		Kind:  string(req.Kind),
		Code:  strPtrOr(req.Code, ""),
	}
	if req.Kind == apigen.MFAChallengeRequestKindWebauthn && req.WebauthnSession != nil {
		sess, err := decodeWebauthnSession(*req.WebauthnSession)
		if err != nil {
			return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
		}
		in.WebAuthnSession = sess
		if req.WebauthnResponse != nil {
			body, err := json.Marshal(*req.WebauthnResponse)
			if err != nil {
				return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
			}
			parsed, err := webauthn.ParseAssertionResponseBody(body)
			if err != nil {
				return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
			}
			in.WebAuthnAssertion = parsed
		}
	}
	res, err := s.mfaSvc.Challenge(c.UserContext(), in)
	if err != nil {
		return s.mapMFAError(c, err)
	}
	setSessionCookie(c, s.cookies, res.Session.CookieValue, res.Session.Refresh.Raw)
	user, err := s.users.GetByID(c.UserContext(), res.Session.UserID)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(apigen.AuthResponse{User: toUserDTO(user)})
}

// mapMFAError translates an MFA service-layer sentinel into a localised
// envelope.
func (s *Server) mapMFAError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, mfa.ErrAlreadyEnrolled):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "auth.err_mfa_already_enrolled", nil), nil)
	case errors.Is(err, mfa.ErrNotEnrolled):
		return SendNotFound(c, i18n.T(ctx, "auth.err_mfa_not_enrolled", nil))
	case errors.Is(err, mfa.ErrNotFound):
		return SendNotFound(c, i18n.T(ctx, "auth.err_mfa_factor_not_found", nil))
	case errors.Is(err, mfa.ErrPendingNotFound):
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_mfa_pending_not_found", nil))
	case errors.Is(err, mfa.ErrPendingExpired):
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_mfa_pending_expired", nil))
	case errors.Is(err, mfa.ErrPendingConsumed):
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_mfa_pending_consumed", nil))
	case errors.Is(err, mfa.ErrPendingRevoked):
		return SendUnauthorized(c, i18n.T(ctx, "auth.err_mfa_pending_revoked", nil))
	case errors.Is(err, mfa.ErrTooManyAttempts):
		return SendError(c, fiber.StatusTooManyRequests, "too_many_requests",
			i18n.T(ctx, "auth.err_mfa_too_many_attempts", nil), nil)
	case errors.Is(err, mfa.ErrInvalidChallenge):
		return SendError(c, fiber.StatusUnauthorized, CodeUnauthorized,
			i18n.T(ctx, "auth.err_mfa_invalid_challenge", nil), nil)
	case errors.Is(err, mfa.ErrMFARequiredUnenrolled):
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(ctx, "auth.err_mfa_required_unenrolled", nil), nil)
	case errors.Is(err, mfa.ErrPasswordRequired):
		return SendBadRequest(c, i18n.T(ctx, "auth.err_mfa_password_required", nil), nil)
	}
	return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
}

// encodeWebauthnSession base64-encodes the ceremony session bytes for HTTP
// transport. The browser echoes the string back at /finish.
func encodeWebauthnSession(s webauthn.SessionData) string {
	return webauthnB64(s.Raw())
}

// decodeWebauthnSession base64-decodes the session string the browser sent.
func decodeWebauthnSession(s string) (webauthn.SessionData, error) {
	raw, err := webauthnB64Decode(s)
	if err != nil {
		return webauthn.SessionData{}, err
	}
	return webauthn.SessionFromBytes(raw), nil
}

// marshalResponseMap converts an upstream protocol struct into the
// map[string]interface{} the generated OpenAPI type expects. The body is
// already JSON-serialisable; we just need to round-trip it through JSON
// so the type assertion in apigen's generated code matches.
func marshalResponseMap(c *fiber.Ctx, v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}
