// Package api: saml_handlers.go implements the OpenAPI-derived SAML 2.0
// service-provider endpoints (WS-07b): the SP metadata endpoint, the SP-
// initiated start endpoint, and the Assertion Consumer Service (POST).
//
// Cookie shape (set on start, read on callback, cleared after use):
//
//	lahijan_oauth_state  - the auth/state double-submit nonce (shared with
//	                       the OAuth + OIDC flows; the state token's provider
//	                       claim is "saml:<key>" so a callback to one SAML
//	                       provider cannot be replayed against another)
//	lahijan_saml_reqid   - the SAML AuthnRequest ID the SP emitted; the ACS
//	                       reads it back so crewjam's ParseResponse can
//	                       enforce the InResponseTo replay check
//	lahijan_link_uid     - present when a logged-in user is linking a new
//	                       SAML IdP (mirrors the OAuth/OIDC link flow)
//
// The ACS handler is POST (not GET like the OAuth/OIDC callbacks) because the
// SAML HTTP-POST binding formulates the response as an HTML auto-submit form
// whose body carries the base64-encoded XML.
package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/idp"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
)

// samlRequestIDCookie is the cookie the SAML start handler sets carrying the
// AuthnRequest ID, and the ACS reads to populate the possibleRequestIDs slice
// for the InResponseTo replay check. Mirrors state.CookieName / oauth.PKCECookieName.
const samlRequestIDCookie = "lahijan_saml_reqid"

// MetadataSAML handles GET /api/v1/auth/saml/metadata.
//
// Returns the SP metadata XML the IdP registers Lahijan under. When more
// than one SAML provider is configured, the metadata is for the first one
// (the SP metadata is process-wide: one entity ID, one ACS URL — the per-
// provider routing happens via the {provider} path parameter, not via
// separate entity IDs).
func (s *Server) MetadataSAML(c *fiber.Ctx) error {
	if s.idpSAML == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	// Pick the first configured SAML provider to render metadata for. The
	// SP's signing cert + entity ID are process-wide; multiple SAML IdPs
	// share the same SP metadata and route via the {provider} path param.
	keys := s.idpSAML.Keys()
	if len(keys) == 0 {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpSAML.Lookup(keys[0])
	if err != nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	raw := p.Metadata()
	if len(raw) == 0 {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	c.Set("Content-Type", "application/xml")
	return c.Send(raw)
}

// StartSAML handles GET /api/v1/auth/saml/{provider}/start.
func (s *Server) StartSAML(c *fiber.Ctx, provider string) error {
	if s.idpSAML == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpSAML.Lookup(provider)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_idp_unknown", map[string]any{"Provider": provider}))
	}
	namespaced := p.NamespacedKey()
	linkUID := ""
	if uid, ok := currentUserID(c); ok {
		linkUID = uid.String()
		setIDPCookie(c, s.idpCookies.LinkUID, linkUID, idpFlowTTL)
	}
	stateToken, nonce, err := s.stateSigner.Issue(namespaced, linkUID)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	authURL, requestID, err := p.BuildAuthRequest(stateToken)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}

	setIDPCookie(c, s.idpCookies.State, nonce, idpFlowTTL)
	setIDPCookie(c, samlRequestIDCookie, requestID, idpFlowTTL)
	return c.Redirect(authURL, fiber.StatusFound)
}

// AssertionConsumerServiceSAML handles POST /api/v1/auth/saml/{provider}/acs.
//
// The request body is application/x-www-form-urlencoded with SAMLResponse
// (base64-encoded XML) and RelayState (the SP-emitted state token). The
// handler verifies state, verifies the signed assertion via the SP, and
// either logs the user in or links the SAML identity to the logged-in user.
func (s *Server) AssertionConsumerServiceSAML(c *fiber.Ctx, provider string) error {
	if s.idpSAML == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "auth.err_idp_disabled", nil))
	}
	p, err := s.idpSAML.Lookup(provider)
	if err != nil {
		return SendNotFound(c, i18n.T(c.UserContext(), "auth.err_idp_unknown", map[string]any{"Provider": provider}))
	}

	var body apigen.AssertionConsumerServiceSAMLFormdataBody
	if err := c.BodyParser(&body); err != nil {
		clearIDPCookies(c, s.idpCookies)
		clearSAMLCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}

	// Verify the state token against the cookie nonce. RelayState carries
	// our CSRF state token; the cookie carries the matching nonce.
	relayState := ""
	if body.RelayState != nil {
		relayState = *body.RelayState
	}
	cookieNonce := c.Cookies(s.idpCookies.State)
	if vErr := p.VerifyState(relayState, cookieNonce, s.linkUIDString(c)); vErr != nil {
		clearIDPCookies(c, s.idpCookies)
		clearSAMLCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_idp_state_invalid", nil), nil)
	}

	// The AuthnRequest ID was stored as a cookie on /start; pass it to
	// ProcessResponse so crewjam's InResponseTo replay check can fire.
	requestID := c.Cookies(samlRequestIDCookie)

	// Build the synthetic *http.Request crewjam's ParseResponse expects.
	// crewjam reads from req.PostForm, so we re-encode the body and call
	// ParseForm on the synthetic request before handing it over.
	httpReq := buildSAMLHTTPReq(body)

	prof, pErr := p.ProcessResponse(c.UserContext(), httpReq, requestID)
	if pErr != nil {
		clearIDPCookies(c, s.idpCookies)
		clearSAMLCookies(c, s.idpCookies)
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_idp_exchange_failed", nil), nil)
	}

	linkUID := s.linkUIDPtr(c)
	ip := clientIP(c)
	ua := clientUA(c)
	res, lErr := s.idpSvc.LinkSAML(c.UserContext(), idp.SAMLLinkInput{
		Provider:    p.NamespacedKey(),
		NameID:      prof.Subject,
		IDPEntityID: prof.IDPEntityID,
		Email:       prof.Email,
		DisplayName: prof.DisplayName,
		Attributes:  prof.Attributes,
		LinkUserID:  linkUID,
		UserAgent:   ua,
		IPAddress:   ip,
	})
	if lErr != nil {
		clearIDPCookies(c, s.idpCookies)
		clearSAMLCookies(c, s.idpCookies)
		return s.mapSAMLError(c, lErr)
	}

	if res.Session != nil {
		setSessionCookie(c, s.cookies, res.Session.CookieValue, res.Session.Refresh.Raw)
	}
	clearIDPCookies(c, s.idpCookies)
	clearSAMLCookies(c, s.idpCookies)
	return c.Redirect(s.idpSuccessRedirect(), fiber.StatusFound)
}

// mapIDPError translates idp service sentinels into localised envelopes.
// Mirrors the OAuth/OIDC version and adds SAML-specifically the JIT-disabled
// rejection.
func (s *Server) mapSAMLError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	if errors.Is(err, idp.ErrJITDisabled) {
		return SendError(c, fiber.StatusForbidden, CodeForbidden,
			i18n.T(ctx, "auth.err_idp_jit_disabled", nil), nil)
	}
	return s.mapIDPError(c, err)
}

// clearSAMLCookies expires the SAML-only cookie (the request id). The shared
// state + link_uid cookies are cleared by clearIDPCookies.
func clearSAMLCookies(c *fiber.Ctx, _ ExternalIDPCookies) {
	c.Cookie(&fiber.Cookie{
		Name: samlRequestIDCookie, Value: "", Path: "/", HTTPOnly: true, SameSite: "lax", MaxAge: -1,
	})
}

// buildSAMLHTTPReq constructs a synthetic *http.Request that carries the
// form body crewjam's ParseResponse consumes. crewjam reads
// req.PostForm.Get("SAMLResponse") + req.PostForm.Get("RelayState"); we
// pre-populate PostForm so the synthetic request round-trips through the
// standard library's form parser without a second parse.
func buildSAMLHTTPReq(body apigen.AssertionConsumerServiceSAMLFormdataBody) *http.Request {
	form := url.Values{}
	form.Set("SAMLResponse", body.SAMLResponse)
	if body.RelayState != nil && *body.RelayState != "" {
		form.Set("RelayState", *body.RelayState)
	}
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/auth/saml/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// ParseForm reads the body and populates PostForm. The error is ignored
	// because the body is well-formed (we just encoded it); if it ever did
	// fail, crewjam's ParseResponse would surface a clearer downstream error.
	_ = req.ParseForm()
	return req
}
