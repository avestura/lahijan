// Package api: idp_handlers.go implements the OpenAPI-derived external-IdP
// endpoints (WS-07a): OAuth/OIDC start + callback + the /me/identities list
// + unlink. Each handler is thin: it parses the request, drives the auth/idp
// service or the underlying oauth/oidc provider, and renders a redirect or a
// JSON envelope.
//
// Cookie shape (set on start, read on callback, cleared after use):
//
//	lahijan_oauth_state  - the auth/state double-submit nonce
//	lahijan_oauth_pkce   - the PKCE code_verifier
//	lahijan_oidc_nonce   - the OIDC nonce (OIDC flow only)
//	lahijan_link_uid     - present when a logged-in user is linking a new IdP
//
// The start handlers mint fresh values; the callback handlers verify them
// via the provider + auth/state and clear them after use.
package api

import (
	"errors"
	"net/netip"
	"strings"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/oauth"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/state"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ExternalIDPCookies carries the short-lived cookie names the start/callback
// pair uses. Wired by program.Start from conf so a deployer can rename any of
// them; defaults ship in the CookieConfig block.
type ExternalIDPCookies struct {
	State     string // lahijan_oauth_state
	PKCE      string // lahijan_oauth_pkce
	OIDCNonce string // lahijan_oidc_nonce
	LinkUID   string // lahijan_link_uid
}

// DefaultExternalIDPCookies is the production cookie-name set.
var DefaultExternalIDPCookies = ExternalIDPCookies{
	State:     state.CookieName,
	PKCE:      oauth.PKCECookieName,
	OIDCNonce: "lahijan_oidc_nonce",
	LinkUID:   "lahijan_link_uid",
}

// idpFlowTTL is the cookie MaxAge for the OAuth/OIDC roundtrip. Mirrors
// auth/state.TTL so the cookies expire alongside the state token itself.
const idpFlowTTL = 600 // 10 minutes (seconds)

// StartOAuth handles GET /api/v1/auth/oauth/{provider}/start.
func (s *Server) StartOAuth(c *fiber.Ctx, provider string) error {
	if s.idpOAuth == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpOAuth.Lookup(provider)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_idp_unknown", map[string]any{"Provider": provider}))
	}

	stateToken, nonce, err := s.stateSigner.Issue(provider, s.linkUIDString(c))
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	authURL, verifier, err := p.BuildAuthURL(stateToken)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}

	setIDPCookie(c, s.idpCookies.State, nonce, idpFlowTTL)
	setIDPCookie(c, s.idpCookies.PKCE, verifier, idpFlowTTL)
	if uid, ok := currentUserID(c); ok {
		setIDPCookie(c, s.idpCookies.LinkUID, uid.String(), idpFlowTTL)
	}
	return c.Redirect(authURL, fiber.StatusFound)
}

// CallbackOAuth handles GET /api/v1/auth/oauth/{provider}/callback.
func (s *Server) CallbackOAuth(c *fiber.Ctx, provider string, params apigen.CallbackOAuthParams) error {
	if s.idpOAuth == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpOAuth.Lookup(provider)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_idp_unknown", map[string]any{"Provider": provider}))
	}

	cookieNonce := c.Cookies(s.idpCookies.State)
	verifier := c.Cookies(s.idpCookies.PKCE)
	if vErr := p.VerifyState(params.State, cookieNonce, s.linkUIDString(c)); vErr != nil {
		clearIDPCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_idp_state_invalid", nil), nil)
	}

	// Wrap the oauth.Provider in the idp adapter so the service consumes a
	// single ExternalIDP shape; the adapter does both Exchange + profile
	// fetch in one call.
	adp := &idp.OAuthAdapter{P: p}
	tok, prof, exErr := adp.Exchange(c.UserContext(), params.Code, verifier)
	if exErr != nil {
		clearIDPCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_idp_exchange_failed", nil), nil)
	}

	linkUID := s.linkUIDPtr(c)
	ip := clientIP(c)
	ua := clientUA(c)
	res, lErr := s.idpSvc.Link(c.UserContext(), idp.LinkInput{
		Provider:    adp.Key(),
		Subject:     prof.Subject,
		Email:       prof.Email,
		DisplayName: prof.DisplayName,
		Tokens:      tok,
		LinkUserID:  linkUID,
		UserAgent:   ua,
		IPAddress:   ip,
	})
	if lErr != nil {
		clearIDPCookies(c, s.idpCookies)
		return s.mapIDPError(c, lErr)
	}

	// On the anonymous-login paths the service opened a session; thread the
	// cookies through so the browser ends up logged in.
	if res.Session != nil {
		setSessionCookie(c, s.cookies, res.Session.CookieValue, res.Session.Refresh.Raw)
	}
	clearIDPCookies(c, s.idpCookies)
	return c.Redirect(s.idpSuccessRedirect(), fiber.StatusFound)
}

// StartOIDC handles GET /api/v1/auth/oidc/{provider}/start.
func (s *Server) StartOIDC(c *fiber.Ctx, provider string) error {
	if s.idpOIDC == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpOIDC.Lookup(provider)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_idp_unknown", map[string]any{"Provider": provider}))
	}
	// The provider key on user_oauth_identities is "oidc:<key>"; the state
	// token must carry that namespaced value so a callback to one OIDC
	// provider cannot be replayed against another.
	namespaced := "oidc:" + provider
	stateToken, nonce, err := s.stateSigner.Issue(namespaced, s.linkUIDString(c))
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	authURL, verifier, oidcNonce, err := p.BuildAuthURL(stateToken)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}

	setIDPCookie(c, s.idpCookies.State, nonce, idpFlowTTL)
	setIDPCookie(c, s.idpCookies.PKCE, verifier, idpFlowTTL)
	setIDPCookie(c, s.idpCookies.OIDCNonce, oidcNonce, idpFlowTTL)
	if uid, ok := currentUserID(c); ok {
		setIDPCookie(c, s.idpCookies.LinkUID, uid.String(), idpFlowTTL)
	}
	return c.Redirect(authURL, fiber.StatusFound)
}

// CallbackOIDC handles GET /api/v1/auth/oidc/{provider}/callback.
func (s *Server) CallbackOIDC(c *fiber.Ctx, provider string, params apigen.CallbackOIDCParams) error {
	if s.idpOIDC == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpOIDC.Lookup(provider)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_idp_unknown", map[string]any{"Provider": provider}))
	}
	namespaced := "oidc:" + provider

	cookieNonce := c.Cookies(s.idpCookies.State)
	verifier := c.Cookies(s.idpCookies.PKCE)
	oidcNonce := c.Cookies(s.idpCookies.OIDCNonce)
	if vErr := p.VerifyState(params.State, cookieNonce, s.linkUIDString(c)); vErr != nil {
		clearIDPCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_idp_state_invalid", nil), nil)
	}

	// The OIDC adapter carries the nonce between BuildAuthURL and Exchange
	// internally; here we read it from the cookie the start handler set.
	adp := &idp.OIDCAdapter{P: p, Nonce: oidcNonce}
	tok, prof, exErr := adp.Exchange(c.UserContext(), params.Code, verifier)
	if exErr != nil {
		clearIDPCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_idp_exchange_failed", nil), nil)
	}

	linkUID := s.linkUIDPtr(c)
	ip := clientIP(c)
	ua := clientUA(c)
	res, lErr := s.idpSvc.Link(c.UserContext(), idp.LinkInput{
		Provider:    namespaced,
		Subject:     prof.Subject,
		Email:       prof.Email,
		DisplayName: prof.DisplayName,
		Tokens:      tok,
		LinkUserID:  linkUID,
		UserAgent:   ua,
		IPAddress:   ip,
	})
	if lErr != nil {
		clearIDPCookies(c, s.idpCookies)
		return s.mapIDPError(c, lErr)
	}
	if res.Session != nil {
		setSessionCookie(c, s.cookies, res.Session.CookieValue, res.Session.Refresh.Raw)
	}
	clearIDPCookies(c, s.idpCookies)
	return c.Redirect(s.idpSuccessRedirect(), fiber.StatusFound)
}

// ListMyIdentities handles GET /api/v1/me/identities.
func (s *Server) ListMyIdentities(c *fiber.Ctx) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	if s.idpSvc == nil {
		// IdP subsystem disabled; return an empty list rather than 501 so
		// the dashboard renders gracefully.
		return c.JSON([]apigen.ExternalIdentity{})
	}
	rows, err := s.idpSvc.ListIdentities(c.UserContext(), uid)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	out := make([]apigen.ExternalIdentity, 0, len(rows))
	for _, r := range rows {
		out = append(out, toExternalIdentityDTO(r))
	}
	return c.JSON(out)
}

// DeleteMyIdentity handles DELETE /api/v1/me/identities/{identityId}.
func (s *Server) DeleteMyIdentity(c *fiber.Ctx, identityID openapi_types.UUID) error {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil
	}
	if s.idpSvc == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	if err := s.idpSvc.Unlink(c.UserContext(), uid, identityID); err != nil {
		return s.mapIDPError(c, err)
	}
	return c.JSON(apigen.MessageResponse{Message: i18n.T(c.UserContext(), "auth.msg_identity_unlinked", nil)})
}

// toExternalIdentityDTO converts the database row to the OpenAPI schema.
// Tokens are NEVER included.
func toExternalIdentityDTO(r database.UserOauthIdentity) apigen.ExternalIdentity {
	dto := apigen.ExternalIdentity{
		Id:        r.ID,
		Provider:  r.Provider,
		Subject:   r.Subject,
		Scopes:    r.Scopes,
		CreatedAt: r.CreatedAt,
		UpdatedAt: &r.UpdatedAt,
	}
	if r.ExpiresAt != nil {
		t := *r.ExpiresAt
		dto.ExpiresAt = &t
	}
	return dto
}

// mapIDPError translates an idp service sentinel into a localised envelope.
func (s *Server) mapIDPError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, idp.ErrAlreadyLinked):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "auth.err_idp_already_linked", nil), nil)
	case errors.Is(err, idp.ErrLinkedElsewhere):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "auth.err_idp_linked_elsewhere", nil), nil)
	case errors.Is(err, idp.ErrNotFound):
		return SendNotFound(c, i18n.T(ctx, "auth.err_idp_not_found", nil))
	case errors.Is(err, idp.ErrLastAuthMethod):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(ctx, "auth.err_idp_last_auth_method", nil), nil)
	case errors.Is(err, idp.ErrTokenEncryption):
		return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
	}
	return SendInternal(c, i18n.T(ctx, "auth.err_internal", nil))
}

// setIDPCookie writes a short-lived cookie the callback will read back.
func setIDPCookie(c *fiber.Ctx, name, value string, maxAge int) {
	c.Cookie(&fiber.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HTTPOnly: true,
		SameSite: "lax",
		MaxAge:   maxAge,
	})
}

// clearIDPCookie expires every cookie the start handler set so the browser
// drops them after the callback resolves.
func clearIDPCookies(c *fiber.Ctx, cfg ExternalIDPCookies) {
	for _, name := range []string{cfg.State, cfg.PKCE, cfg.OIDCNonce, cfg.LinkUID} {
		if name == "" {
			continue
		}
		c.Cookie(&fiber.Cookie{
			Name: name, Value: "", Path: "/", HTTPOnly: true, SameSite: "lax", MaxAge: -1,
		})
	}
}

// linkUIDString returns the link_uid cookie value or "" when absent.
func (s *Server) linkUIDString(c *fiber.Ctx) string {
	if s.idpCookies.LinkUID == "" {
		return ""
	}
	return c.Cookies(s.idpCookies.LinkUID)
}

// linkUIDPtr returns *uuid.UUID when the link_uid cookie carries a valid
// uuid, otherwise nil (anonymous login flow).
func (s *Server) linkUIDPtr(c *fiber.Ctx) *uuid.UUID {
	raw := s.linkUIDString(c)
	if raw == "" {
		return nil
	}
	uid, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &uid
}

// clientIP parses c.IP() into a *netip.Addr (nil on parse failure).
func clientIP(c *fiber.Ctx) *netip.Addr {
	if ip, err := netip.ParseAddr(c.IP()); err == nil {
		return &ip
	}
	return nil
}

// clientUA returns a non-nil *string when User-Agent is set.
func clientUA(c *fiber.Ctx) *string {
	ua := c.Get("User-Agent")
	if ua == "" {
		return nil
	}
	return &ua
}

// idpSuccessRedirect returns the dashboard URL the callback bounces to on a
// successful link/login. Today it is the platform root; a future WS will
// make this configurable (mobile deep links, locale-specific roots).
func (s *Server) idpSuccessRedirect() string {
	if s.idpRedirectHome == "" {
		return "/"
	}
	return strings.TrimRight(s.idpRedirectHome, "/")
}
