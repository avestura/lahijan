// Package idp: saml.go is the SAML-specific account-linking surface for
// WS-07b. It mirrors the OAuth Link flow but persists to
// user_saml_identities (no tokens to encrypt; SAML assertions are
// short-lived) and carries the attribute snapshot the IdP asserted.
//
// The service stays provider-agnostic at the OAuth/OIDC layer; this file
// adds the SAML branches the ACS handler calls after the auth/saml package
// has verified the signed assertion.
package idp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// SAMLLinkInput carries the verified SAML assertion data the ACS handler
// collects before calling LinkSAML. All fields are required except
// LinkUserID (which is nil for the anonymous "log in with SSO" path) and
// the attribute-derived Email / DisplayName (which the IdP may not assert).
type SAMLLinkInput struct {
	// Provider is the namespaced SAML provider key ("saml:<config_key>"). It
	// is the value stored on user_saml_identities.provider and MUST match
	// the SAML Provider.NamespacedKey() so ListIdentities can group rows
	// from the same IdP.
	Provider    string
	NameID      string
	IDPEntityID string
	Email       string
	DisplayName string
	Attributes  map[string]any

	// LinkUserID is present when a logged-in user is LINKING a new SAML IdP
	// (vs anonymous "log in with SSO").
	LinkUserID *uuid.UUID
	UserAgent  *string
	IPAddress  *netip.Addr
}

// SAMLLinkResult is the outcome of LinkSAML. Mirrors LinkResult so the api
// handler can render the same redirect for OAuth, OIDC, and SAML flows.
type SAMLLinkResult struct {
	ResultKind ResultKind
	UserID     uuid.UUID
	IdentityID uuid.UUID

	// Session is populated for ResultNewUser and ResultExistingUser (the
	// anonymous-login paths). Empty for ResultLinked (the user is already
	// logged in and did not need a new session).
	Session *SessionOpen
}

// LinkSAML finishes a SAML ACS callback: it looks up an existing identity by
// (provider, name_id), and either:
//   - Logs the existing user in (anonymous flow against a known identity),
//   - Creates a new user from the IdP profile (anonymous flow against an
//     unknown identity, when JIT provisioning is on), or
//   - Binds the identity to the LinkUserID (link flow).
//
// The attribute snapshot is always refreshed (existing identity) or inserted
// (new identity) so the user's profile reflects the latest IdP-asserted
// claims.
//
// Returns ErrJITDisabled when the (provider, name_id) is unknown AND no
// LinkUserID was supplied AND just-in-time provisioning is off (the default
// for production deployments that require admin pre-registration).
func (s *Service) LinkSAML(ctx context.Context, in SAMLLinkInput) (SAMLLinkResult, error) {
	if s.samlIdps == nil {
		return SAMLLinkResult{}, errors.New("idp: saml identities repository not wired (SAML disabled)")
	}
	existing, err := s.samlIdps.GetByProviderNameID(ctx, in.Provider, in.NameID)
	switch {
	case err == nil:
		return s.resolveExistingSAMLIdentity(ctx, existing, in)
	case database.IsNoRows(err):
		return s.resolveNewSAMLIdentity(ctx, in)
	default:
		return SAMLLinkResult{}, fmt.Errorf("idp: lookup saml identity: %w", err)
	}
}

// resolveExistingSAMLIdentity mirrors resolveExistingIdentity but for SAML.
// The attribute snapshot is always refreshed on a successful login so the
// user's profile reflects the latest IdP claims.
func (s *Service) resolveExistingSAMLIdentity(
	ctx context.Context,
	row gen.UserSamlIdentity,
	in SAMLLinkInput,
) (SAMLLinkResult, error) {
	if in.LinkUserID != nil && *in.LinkUserID != row.UserID {
		// Caller is logged in as A but the SAML identity is owned by B.
		// Allowing the link would let A take over B's account.
		s.auditFail(ctx, audit.ActionIdpLink, in.LinkUserID, map[string]any{
			"provider": in.Provider, "name_id": in.NameID, "owner": row.UserID.String(),
		})
		return SAMLLinkResult{}, ErrLinkedElsewhere
	}

	// Refresh the stored attribute snapshot + IdP entity id.
	if err := s.samlIdps.UpdateAttributes(ctx, database.UpdateSAMLIdentityAttributesParams{
		ID:          row.ID,
		UserID:      row.UserID,
		Attributes:  in.Attributes,
		IdpEntityID: in.IDPEntityID,
	}); err != nil {
		return SAMLLinkResult{}, fmt.Errorf("idp: refresh saml attributes: %w", err)
	}

	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &row.UserID,
		Action:       audit.ActionIdpLink,
		ResourceType: audit.ResourceUser,
		ResourceID:   &row.UserID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"provider": in.Provider, "name_id": in.NameID,
			"kind": "existing_saml",
		},
	})

	if in.LinkUserID != nil {
		return SAMLLinkResult{ResultKind: ResultLinked, UserID: row.UserID, IdentityID: row.ID}, nil
	}

	// Anonymous login against an existing identity: open a session.
	sess, err := s.session.OpenForExistingUser(ctx, row.UserID, in.UserAgent, in.IPAddress)
	if err != nil {
		return SAMLLinkResult{}, fmt.Errorf("idp: open session for existing saml user: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &row.UserID,
		Action:       audit.ActionIdpLogin,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"provider": in.Provider, "kind": "saml"},
	})
	return SAMLLinkResult{
		ResultKind: ResultExistingUser,
		UserID:     row.UserID,
		IdentityID: row.ID,
		Session:    &sess,
	}, nil
}

// resolveNewSAMLIdentity handles the case where (provider, name_id) has no
// row. If LinkUserID is set, bind the identity to that user. Otherwise, when
// JIT provisioning is on, create a fresh user from the IdP profile.
//
// JIT is OFF by default in the LinkSAML path; the api handler must opt in
// via EnableJIT. This keeps production defaults conservative (admin
// pre-registration) while letting homelab / small-team deployments turn JIT
// on via config.
func (s *Service) resolveNewSAMLIdentity(ctx context.Context, in SAMLLinkInput) (SAMLLinkResult, error) {
	var userID uuid.UUID
	if in.LinkUserID != nil {
		userID = *in.LinkUserID
	} else {
		if !s.jitEnabled {
			s.auditFail(ctx, audit.ActionIdpLink, nil, map[string]any{
				"provider": in.Provider, "name_id": in.NameID, "reason": "jit_disabled",
			})
			return SAMLLinkResult{}, ErrJITDisabled
		}
		params := database.CreateUserParams{
			Email:       in.Email,
			IsActive:    boolPtr(true),
			DisplayName: strPtrOrNil(in.DisplayName),
			Locale:      "en",
		}
		user, err := s.users.Create(ctx, params)
		if err != nil {
			return SAMLLinkResult{}, fmt.Errorf("idp: create user from saml profile: %w", err)
		}
		userID = user.ID
	}

	row, err := s.samlIdps.Create(ctx, database.CreateSAMLIdentityParams{
		UserID:      userID,
		Provider:    in.Provider,
		NameID:      in.NameID,
		IdpEntityID: in.IDPEntityID,
		Attributes:  in.Attributes,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return SAMLLinkResult{}, ErrAlreadyLinked
		}
		return SAMLLinkResult{}, fmt.Errorf("idp: persist saml identity: %w", err)
	}

	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionIdpLink,
		ResourceType: audit.ResourceUser,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"provider": in.Provider, "name_id": in.NameID,
			"kind": "new_saml", "identity_id": row.ID.String(),
		},
	})

	if in.LinkUserID != nil {
		return SAMLLinkResult{ResultKind: ResultLinked, UserID: userID, IdentityID: row.ID}, nil
	}

	sess, err := s.session.OpenForExistingUser(ctx, userID, in.UserAgent, in.IPAddress)
	if err != nil {
		return SAMLLinkResult{}, fmt.Errorf("idp: open session for new saml user: %w", err)
	}
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionIdpLogin,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"provider": in.Provider, "first_login": true, "kind": "saml"},
	})
	return SAMLLinkResult{
		ResultKind: ResultNewUser,
		UserID:     userID,
		IdentityID: row.ID,
		Session:    &sess,
	}, nil
}

// ListSAMLIdentities returns every SAML identity linked to the user. The
// api handler merges this with ListIdentities to render a unified /me/
// identities response.
func (s *Service) ListSAMLIdentities(ctx context.Context, userID uuid.UUID) ([]gen.UserSamlIdentity, error) {
	if s.samlIdps == nil {
		return nil, nil
	}
	rows, err := s.samlIdps.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("idp: list saml identities: %w", err)
	}
	return rows, nil
}

// UnlinkSAML removes a SAML identity row. Mirrors Unlink's "last auth method"
// invariant (counts OAuth + SAML + password before deleting). Used by the
// api handler when the identity id did not match an OAuth/OIDC row.
func (s *Service) UnlinkSAML(ctx context.Context, userID, identityID uuid.UUID) error {
	if s.samlIdps == nil {
		return ErrNotFound
	}
	row, err := s.samlIdps.Get(ctx, identityID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrNotFound
		}
		return fmt.Errorf("idp: load saml identity for unlink: %w", err)
	}
	if row.UserID != userID {
		return ErrNotFound
	}

	if err := s.assertCanRemoveAuthMethod(ctx, userID); err != nil {
		return err
	}

	if err := s.samlIdps.Delete(ctx, identityID, userID); err != nil {
		return fmt.Errorf("idp: delete saml identity: %w", err)
	}

	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionIdpUnlink,
		ResourceType: audit.ResourceUser,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"identity_id": identityID, "provider": row.Provider, "kind": "saml",
		},
	})
	return nil
}

// ErrJITDisabled is returned by LinkSAML when the (provider, name_id) is
// unknown, no LinkUserID was supplied, and just-in-time user creation is
// disabled. The api handler maps this to a 403 with a localised "contact
// your operator to be pre-registered" message.
var ErrJITDisabled = errors.New("idp: just-in-time user creation is disabled for this provider")

// SetJITEnabled flips the package-level default JIT toggle. Called once
// from program.Start after conf is loaded; the Service constructor also
// accepts a per-instance toggle (Service.JITEnabled) so tests with parallel
// services can have independent JIT settings without the race a package-
// level var would introduce.
func SetJITEnabled(on bool) { jitEnabled = on }

// jitEnabled is the package-level default JIT toggle. The Service struct
// carries its own copy (initialized from this default at New time) so
// per-test instances can override without racing.
var jitEnabled = false
