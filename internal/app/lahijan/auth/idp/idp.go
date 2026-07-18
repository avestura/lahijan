// Package idp orchestrates external-identity-provider (IdP) flows: account
// linking, "log in with X", and token persistence (WS-07a). It is the layer
// between the OAuth/OIDC clients (which talk to the IdP) and the database
// (which persists identity rows). It does NOT issue Lahijan sessions itself;
// the api handler is responsible for opening one once the identity is
// resolved.
//
// The service is provider-agnostic: it takes a generic ExternalIDP interface
// that both the OAuth and OIDC providers satisfy through a tiny adapter
// defined here, so the api handler has one type to deal with. The actual
// provider instances are looked up via ProviderLookup at request time, which
// keeps the service stateless and lets the bootstrap swap providers in
// without rebuilding the service.
//
// The service encrypts every token (access + refresh) via auth/secrets.Crypto
// before handing it to the repository; raw tokens never enter the database.
// Every login + link + unlink emits an audit event through the audit.Emitter
// seam (best-effort; failures are logged but do not block the flow).
package idp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Sentinel errors. The api handler maps these to localised HTTP envelopes.
var (
	// ErrAlreadyLinked is returned when Link is called for a (provider, subject)
	// pair that already belongs to some user (this one or another).
	ErrAlreadyLinked = errors.New("idp: identity already linked to a user")
	// ErrLinkedElsewhere is returned when a callback resolves an identity
	// that belongs to a different user than the linkUserID asking for it.
	ErrLinkedElsewhere = errors.New("idp: identity belongs to a different user")
	// ErrNotFound is returned by Unlink when no identity matches.
	ErrNotFound = errors.New("idp: identity not found")
	// ErrLastAuthMethod is returned by Unlink when removing the identity would
	// leave the user with no way to log in (no password, no other identities).
	ErrLastAuthMethod = errors.New("idp: cannot remove the last remaining authentication method")
	// ErrTokenEncryption is returned when the AES-GCM envelope fails (a
	// misconfigured encryption key). Surfaced as 500 to the client.
	ErrTokenEncryption = errors.New("idp: token encryption failed")
)

// Profile is the normalized user info returned by the IdP. Mirrors
// oauth.Profile / oidc.IDClaims so the service can treat both flows
// identically.
type Profile struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}

// Tokens is the IdP response surface the service persists: the access token,
// the refresh token (may be empty), the scope the IdP granted, and the
// access token's expiry. The id_token (OIDC) is intentionally NOT persisted
// here; it lives in its own column when the OIDC adapter is wired.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	Scope        string
}

// ExternalIDP is the surface both oauth.Provider and oidc.Provider (via an
// adapter) expose. The service uses it to call Exchange + FetchProfile after
// the api handler has already done BuildAuthURL + VerifyState.
type ExternalIDP interface {
	Key() string
	Exchange(ctx context.Context, code, codeVerifier string) (Tokens, Profile, error)
}

// ProviderLookup resolves a provider key to an ExternalIDP at request time.
// The api handler pulls the path param and calls Lookup; the service stays
// stateless.
type ProviderLookup interface {
	Lookup(provider string) (ExternalIDP, error)
}

// Service orchestrates account linking and login-with-IdP flows. Construct
// one at bootstrap and share it across requests.
type Service struct {
	repos    *database.Repos
	crypto   *secrets.Crypto
	audit    audit.Emitter
	users    *database.UsersRepository
	idps     *database.OAuthIdentitiesRepository
	samlIdps *database.SamlIdentitiesRepository
	session  SessionOpener
}

// SessionOpener opens a Lahijan session once an identity has been resolved.
// It's the same shape auth/session.Service.Register / Login already expose,
// narrowed to "log this user in from this IP/UA". The api handler injects a
// concrete implementation; tests inject a capturing fake.
type SessionOpener interface {
	// OpenForExistingUser issues a session + refresh token for a known user
	// (the result of a successful "log in with X" against an identity that
	// already had a row). Returns the raw cookie values to ship to the
	// client exactly once.
	OpenForExistingUser(ctx context.Context, userID uuid.UUID, ua *string, ip *netip.Addr) (SessionOpen, error)
}

// SessionOpen is the result of opening a session for an existing user.
// Mirrors the subset of session.Session the api handler needs to set cookies.
type SessionOpen struct {
	UserID      uuid.UUID
	SessionID   uuid.UUID
	ExpiresAt   time.Time
	CookieValue string
	Refresh     RefreshIssue
}

// RefreshIssue carries the raw refresh token + its family id.
type RefreshIssue struct {
	Raw       string
	FamilyID  uuid.UUID
	ExpiresAt time.Time
}

// New builds the IdP service. The SamlIdentitiesRepository on repos may be
// nil when SAML is disabled; LinkSAML / ListSAMLIdentities / UnlinkSAML
// must not be called in that case (the api handler short-circuits).
func New(
	repos *database.Repos,
	crypto *secrets.Crypto,
	auditEmitter audit.Emitter,
	session SessionOpener,
) *Service {
	return &Service{
		repos:    repos,
		crypto:   crypto,
		audit:    auditEmitter,
		users:    repos.Users,
		idps:     repos.OAuthIdentities,
		samlIdps: repos.SamlIdentities,
		session:  session,
	}
}

// LinkInput carries the data the api handler collects from the callback
// before calling Link. All fields are required except LinkUserID (which is
// nil for the anonymous "log in with X" path).
type LinkInput struct {
	Provider    string
	Subject     string
	Email       string
	DisplayName string
	Tokens      Tokens
	LinkUserID  *uuid.UUID // present when a logged-in user is LINKING a new IdP
	UserAgent   *string
	IPAddress   *netip.Addr
}

// LinkResult is the outcome of Link. For an anonymous login that resolved a
// NEW identity (no user had that (provider, subject)), ResultKind is
// ResultNewUser and UserID is the freshly-created user's id. For a flow that
// resolved an EXISTING identity, ResultKind is ResultExistingUser and a
// session has been opened. For a link flow (LinkUserID set) that succeeded,
// ResultKind is ResultLinked and UserID is the link user's id.
type LinkResult struct {
	ResultKind ResultKind
	UserID     uuid.UUID
	IdentityID uuid.UUID

	// Session is populated for ResultNewUser and ResultExistingUser (the
	// anonymous-login paths). Empty for ResultLinked (the user is already
	// logged in and did not need a new session).
	Session *SessionOpen
}

// ResultKind enumerates the outcomes of Link.
type ResultKind int

const (
	// ResultNewUser: the (provider, subject) was unknown; a new user was
	// created from the IdP profile and a session opened.
	ResultNewUser ResultKind = iota
	// ResultExistingUser: the (provider, subject) matched an existing
	// identity row; the owning user was logged in.
	ResultExistingUser
	// ResultLinked: the link flow succeeded; the (provider, subject) was
	// bound to the LinkUserID.
	ResultLinked
)

// Link finishes an OAuth/OIDC callback: it looks up an existing identity by
// (provider, subject), and either:
//   - Logs the existing user in (anonymous flow against a known identity),
//   - Creates a new user from the IdP profile (anonymous flow against an
//     unknown identity), or
//   - Binds the identity to the LinkUserID (link flow).
//
// The tokens are encrypted via auth/secrets.Crypto before being persisted.
func (s *Service) Link(ctx context.Context, in LinkInput) (LinkResult, error) {
	// Resolve the existing identity row, if any.
	existing, err := s.idps.GetByProviderSubject(ctx, in.Provider, in.Subject)
	switch {
	case err == nil:
		// Identity already exists.
		return s.resolveExistingIdentity(ctx, existing, in)
	case database.IsNoRows(err):
		// Identity does not exist; create or link.
		return s.resolveNewIdentity(ctx, in)
	default:
		return LinkResult{}, fmt.Errorf("idp: lookup identity: %w", err)
	}
}

// resolveExistingIdentity handles the case where (provider, subject) already
// has a row. If the request is anonymous (no LinkUserID), the owning user is
// logged in. If LinkUserID is set and matches the row's user, we just refresh
// the tokens. If LinkUserID is set but does NOT match, the identity belongs
// to a different user — reject.
func (s *Service) resolveExistingIdentity(
	ctx context.Context,
	row database.UserOauthIdentity,
	in LinkInput,
) (LinkResult, error) {
	if in.LinkUserID != nil && *in.LinkUserID != row.UserID {
		// The caller is logged in as A but the identity is owned by B.
		// Allowing the link would let A take over B's account.
		s.auditFail(ctx, audit.ActionIdpLink, in.LinkUserID, map[string]any{
			"provider": in.Provider, "subject": in.Subject, "owner": row.UserID.String(),
		})
		return LinkResult{}, ErrLinkedElsewhere
	}

	// Rotate the stored tokens (encrypted).
	if err := s.persistTokens(ctx, row.ID, row.UserID, in.Tokens); err != nil {
		return LinkResult{}, err
	}

	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &row.UserID,
		Action:       audit.ActionIdpLink,
		ResourceType: audit.ResourceUser,
		ResourceID:   &row.UserID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"provider": in.Provider, "subject": in.Subject,
			"kind": "existing",
		},
	})

	if in.LinkUserID != nil {
		// Link flow against an already-linked-to-this-user identity: the
		// caller already has access; just refresh tokens + audit.
		return LinkResult{ResultKind: ResultLinked, UserID: row.UserID, IdentityID: row.ID}, nil
	}

	// Anonymous login against an existing identity: open a session.
	sess, err := s.session.OpenForExistingUser(ctx, row.UserID, in.UserAgent, in.IPAddress)
	if err != nil {
		return LinkResult{}, fmt.Errorf("idp: open session for existing user: %w", err)
	}
	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &row.UserID,
		Action:       audit.ActionIdpLogin,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"provider": in.Provider},
	})
	return LinkResult{
		ResultKind: ResultExistingUser,
		UserID:     row.UserID,
		IdentityID: row.ID,
		Session:    &sess,
	}, nil
}

// resolveNewIdentity handles the case where (provider, subject) has no row.
// If LinkUserID is set, bind the identity to that user. Otherwise create a
// fresh user from the IdP profile.
func (s *Service) resolveNewIdentity(ctx context.Context, in LinkInput) (LinkResult, error) {
	var userID uuid.UUID
	if in.LinkUserID != nil {
		// Link flow: bind to the existing user.
		userID = *in.LinkUserID
	} else {
		// Anonymous login: create a new user from the IdP profile. The email
		// is set if the IdP returned one; otherwise leave empty and let the
		// user complete it on first visit.
		//
		// NOTE: a real product would refuse to create a user without an
		// email. The platform policy (require email) is enforced by the api
		// handler, which can return a "completion required" envelope before
		// reaching this service. Here we accept whatever the IdP returned.
		params := database.CreateUserParams{
			Email:       in.Email,
			IsActive:    boolPtr(true),
			DisplayName: strPtrOrNil(in.DisplayName),
			Locale:      "en",
		}
		user, err := s.users.Create(ctx, params)
		if err != nil {
			return LinkResult{}, fmt.Errorf("idp: create user from idp profile: %w", err)
		}
		userID = user.ID
	}

	encAccess, encRefresh, err := s.encryptTokens(in.Tokens)
	if err != nil {
		return LinkResult{}, err
	}
	scopes := splitScope(in.Tokens.Scope)
	row, err := s.idps.Create(ctx, database.CreateOAuthIdentityParams{
		UserID:       userID,
		Provider:     in.Provider,
		Subject:      in.Subject,
		AccessToken:  encAccess,
		RefreshToken: encRefresh,
		Scopes:       scopes,
		ExpiresAt:    expiryPtr(in.Tokens.Expiry),
	})
	if err != nil {
		// A unique violation here means a concurrent link raced us; surface
		// it as ErrAlreadyLinked so the api handler can return a clean 409.
		if database.IsUniqueViolation(err) {
			return LinkResult{}, ErrAlreadyLinked
		}
		return LinkResult{}, fmt.Errorf("idp: persist identity: %w", err)
	}

	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionIdpLink,
		ResourceType: audit.ResourceUser,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"provider": in.Provider, "subject": in.Subject,
			"kind": "new", "identity_id": row.ID.String(),
		},
	})

	if in.LinkUserID != nil {
		return LinkResult{ResultKind: ResultLinked, UserID: userID, IdentityID: row.ID}, nil
	}

	// Anonymous login against a brand-new identity: open a session for the
	// user we just created.
	sess, err := s.session.OpenForExistingUser(ctx, userID, in.UserAgent, in.IPAddress)
	if err != nil {
		return LinkResult{}, fmt.Errorf("idp: open session for new user: %w", err)
	}
	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionIdpLogin,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"provider": in.Provider, "first_login": true},
	})
	return LinkResult{
		ResultKind: ResultNewUser,
		UserID:     userID,
		IdentityID: row.ID,
		Session:    &sess,
	}, nil
}

// persistTokens rotates the stored tokens (encrypted) on an existing
// identity row. Used on every login to keep the access_token fresh.
func (s *Service) persistTokens(
	ctx context.Context,
	identityID, userID uuid.UUID,
	tokens Tokens,
) error {
	encAccess, encRefresh, err := s.encryptTokens(tokens)
	if err != nil {
		return err
	}
	scopes := splitScope(tokens.Scope)
	if err := s.idps.UpdateTokens(ctx, database.UpdateOAuthIdentityTokensParams{
		ID:           identityID,
		UserID:       userID,
		AccessToken:  encAccess,
		RefreshToken: encRefresh,
		Scopes:       scopes,
		ExpiresAt:    expiryPtr(tokens.Expiry),
	}); err != nil {
		return fmt.Errorf("idp: rotate tokens: %w", err)
	}
	return nil
}

// encryptTokens runs both tokens through the AES-GCM envelope. Empty inputs
// pass through (the IdP may not return a refresh_token).
func (s *Service) encryptTokens(tokens Tokens) (*string, *string, error) {
	encAccess, err := s.crypto.SealPtr(strPtrOrNil(tokens.AccessToken))
	if err != nil {
		return nil, nil, errors.Join(ErrTokenEncryption, err)
	}
	encRefresh, err := s.crypto.SealPtr(strPtrOrNil(tokens.RefreshToken))
	if err != nil {
		return nil, nil, errors.Join(ErrTokenEncryption, err)
	}
	return encAccess, encRefresh, nil
}

// Unlink removes a (user, provider) OAuth/OIDC identity row. The "at least
// one auth method remaining" invariant is enforced: removing the last
// identity while the user has no password_hash AND no SAML identities is
// rejected with ErrLastAuthMethod.
//
// SAML identities live in a separate table; use UnlinkSAML for those.
func (s *Service) Unlink(ctx context.Context, userID, identityID uuid.UUID) error {
	// Load the identity first so we can scope the delete to (id, user_id).
	row, err := s.idps.Get(ctx, identityID)
	if err != nil {
		if database.IsNoRows(err) {
			// Fall through to SAML if the OAuth repo did not find it; this
			// lets the api handler dispatch by identity id without knowing
			// which table the row lives in.
			if s.samlIdps != nil {
				return s.UnlinkSAML(ctx, userID, identityID)
			}
			return ErrNotFound
		}
		return fmt.Errorf("idp: load identity for unlink: %w", err)
	}
	if row.UserID != userID {
		// Cross-user unlink: behave as not-found so an attacker cannot tell
		// whether the identity exists on another user.
		return ErrNotFound
	}

	if err := s.assertCanRemoveAuthMethod(ctx, userID); err != nil {
		return err
	}

	if err := s.idps.Delete(ctx, identityID, userID); err != nil {
		return fmt.Errorf("idp: delete identity: %w", err)
	}

	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionIdpUnlink,
		ResourceType: audit.ResourceUser,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"identity_id": identityID, "provider": row.Provider,
		},
	})
	return nil
}

// assertCanRemoveAuthMethod enforces the "at least one auth method remaining"
// invariant across BOTH OAuth/OIDC and SAML identity tables. Returns
// ErrLastAuthMethod when removing one more identity would leave the user
// with no way to log in (no password AND only the identity being removed).
func (s *Service) assertCanRemoveAuthMethod(ctx context.Context, userID uuid.UUID) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("idp: load user for unlink: %w", err)
	}
	oauthCount, err := s.idps.CountForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("idp: count oauth identities for unlink: %w", err)
	}
	samlCount := int64(0)
	if s.samlIdps != nil {
		samlCount, err = s.samlIdps.CountForUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("idp: count saml identities for unlink: %w", err)
		}
	}
	totalExternal := oauthCount + samlCount
	if user.PasswordHash == nil && totalExternal <= 1 {
		s.auditFail(ctx, audit.ActionIdpUnlink, &userID, map[string]any{
			"reason": "last_auth_method",
		})
		return ErrLastAuthMethod
	}
	return nil
}

// ListIdentities returns every external identity linked to the user. Tokens
// are NOT decrypted; the response carries only (provider, subject, scopes).
func (s *Service) ListIdentities(ctx context.Context, userID uuid.UUID) ([]database.UserOauthIdentity, error) {
	rows, err := s.idps.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("idp: list identities: %w", err)
	}
	return rows, nil
}

// auditFail records a failed privileged action without blocking the return.
func (s *Service) auditFail(ctx context.Context, action string, userID *uuid.UUID, meta map[string]any) {
	// Best-effort: audit failure is logged but does not block the flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  userID,
		Action:       action,
		ResourceType: audit.ResourceUser,
		ResourceID:   userID,
		Status:       audit.StatusFailure,
		Metadata:     meta,
	})
}

// splitScope splits a space-separated OAuth2 scope string into a slice. Empty
// input yields an empty slice (the DB column is TEXT[] NOT NULL DEFAULT '{}').
func splitScope(s string) []string {
	if s == "" {
		return []string{}
	}
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ' ' || r == ',' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// expiryPtr returns &t when t is non-zero, otherwise nil.
func expiryPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// strPtrOrNil returns &s when s is non-empty, otherwise nil.
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// boolPtr returns &b.
func boolPtr(b bool) *bool { return &b }
