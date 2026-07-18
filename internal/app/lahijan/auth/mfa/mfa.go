// Package mfa orchestrates Lahijan's multi-factor authentication flows
// (WS-07c). It is the layer between the api handlers (which translate
// HTTP) and the three factor packages (totp, webauthn, recovery) + the
// session service (which issues the real session once a factor succeeds).
//
// The service owns four responsibilities:
//
//  1. Factor enrollment (per-user):
//     TOTP:   EnrollTOTP / VerifyTOTP / DisableTOTP
//     WebAuthn: BeginWebAuthnRegistration / FinishWebAuthnRegistration
//     Recovery: RegenerateRecoveryCodes
//
//  2. Login flow integration:
//     BeginLoginFromPassword / BeginLoginFromIdP — issue a pending_session_token
//     Challenge — verify a TOTP / recovery / WebAuthn code against the
//     pending session and, on success, issue the real session.
//
//  3. Policy enforcement:
//     IsMFARequired — does this user need an MFA challenge before the real
//     session is issued? (per-user opt-in OR per-tenant
//     required OR per-role required)
//
//  4. Audit emission for every privileged MFA action (enroll, verify,
//     disable, regenerate, challenge success/failure).
//
// The service never imports the upstream WebAuthn library directly — it
// goes through auth/mfa/webauthn.RelyingParty so the WS-07c surface stays
// replaceable. TOTP and recovery are pure Go and have no DB / upstream
// surface, so they are imported as-is.
package mfa

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/recovery"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/totp"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/mfa/webauthn"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Sentinel errors. The api handler maps these to localised envelopes.
var (
	ErrNotFound              = errors.New("mfa: factor not found")
	ErrAlreadyEnrolled       = errors.New("mfa: factor already enrolled")
	ErrNotEnrolled           = errors.New("mfa: factor not enrolled")
	ErrPendingNotFound       = errors.New("mfa: pending session not found")
	ErrPendingExpired        = errors.New("mfa: pending session expired")
	ErrPendingConsumed       = errors.New("mfa: pending session already used")
	ErrPendingRevoked        = errors.New("mfa: pending session revoked")
	ErrTooManyAttempts       = errors.New("mfa: too many failed attempts; pending session revoked")
	ErrInvalidChallenge      = errors.New("mfa: invalid challenge code")
	ErrMFARequiredUnenrolled = errors.New("mfa: policy requires MFA but the user has no factor enrolled")
	ErrPasswordRequired      = errors.New("mfa: current password is required to disable MFA")
)

// Audit action constants are registered centrally in audit/audit.go;
// the package uses audit.ActionMFA* constants directly so the strings
// never drift.

// Standard MFA resource types recorded on audit_log.resource_type.
const (
	ResourceMFAFactor  = "mfa_factor"
	ResourceMFAPending = "mfa_pending_session"
)

// Config carries the deployer-controlled MFA parameters. Build it from
// conf once at bootstrap.
type Config struct {
	TOTP            totp.Config
	Recovery        recovery.Config
	WebAuthn        webauthn.Config
	PendingLifetime time.Duration
	MaxAttempts     int
}

// DefaultConfig returns the WS-07c-scope defaults. The bootstrap overrides
// the WebAuthn RPID + RPOrigins from conf.auth.mfa.webauthn.*.
func DefaultConfig() Config {
	return Config{
		TOTP:            totp.DefaultConfig(),
		Recovery:        recovery.DefaultConfig(),
		WebAuthn:        webauthn.DefaultConfig(),
		PendingLifetime: 5 * time.Minute,
		MaxAttempts:     5,
	}
}

// SessionOpener issues the real Lahijan session once an MFA challenge
// succeeds. Mirrors auth/idp.SessionOpener so the same shape is reused.
type SessionOpener interface {
	OpenForExistingUser(
		ctx context.Context,
		userID uuid.UUID,
		ua *string,
		ip *netip.Addr,
	) (SessionOpen, error)
}

// SessionOpen is the result of opening a session for an existing user.
// Mirrors the subset of session.Session the api handler needs.
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

// Service is the MFA orchestrator. Build one at bootstrap with New and
// share it across requests.
type Service struct {
	repos    *database.Repos
	crypto   *secrets.Crypto
	signer   *secrets.Signer
	rp       webauthn.RelyingParty
	sessions SessionOpener
	audit    audit.Emitter
	cfg      Config
}

// New builds the MFA service. The repos + crypto + signer + audit + sessions
// come from the same authDeps the session service uses. The webauthn.RP
// is built by the bootstrap from conf.auth.mfa.webauthn.* and injected as
// a RelyingParty interface so tests can pass a fake.
func New(
	repos *database.Repos,
	crypto *secrets.Crypto,
	signer *secrets.Signer,
	rp webauthn.RelyingParty,
	sessions SessionOpener,
	emitter audit.Emitter,
	cfg Config,
) *Service {
	return &Service{
		repos:    repos,
		crypto:   crypto,
		signer:   signer,
		rp:       rp,
		sessions: sessions,
		audit:    emitter,
		cfg:      cfg,
	}
}

// =====================================================================
// Factor enrollment: TOTP
// =====================================================================

// EnrollTOTPInput carries the fields EnrollTOTP needs.
type EnrollTOTPInput struct {
	UserID    uuid.UUID
	Email     string
	Challenge string // current password or pending-session challenge
}

// TOTPSecret is the result of EnrollTOTP: the raw base32 secret (shown
// once so the dashboard can render the QR) and the provisioning URI.
// Until the user calls VerifyTOTP with a valid 6-digit code, the secret
// does NOT count as an enrolled factor.
type TOTPSecret struct {
	Raw             string
	ProvisioningURI string
}

// EnrollTOTP generates a fresh TOTP secret, AES-GCM-encrypts it, persists
// the ciphertext in user_totp_secrets (replacing any previous row), and
// returns the raw secret + provisioning URI for the dashboard to display.
//
// If the user already has a confirmed TOTP factor, returns
// ErrAlreadyEnrolled — the user must explicitly DisableTOTP first.
func (s *Service) EnrollTOTP(ctx context.Context, in EnrollTOTPInput) (TOTPSecret, error) {
	// Reject re-enroll over a confirmed factor — the user must explicitly
	// disable first. This makes the dashboard flow safer (an attacker who
	// steals the session cookie cannot silently swap the user's TOTP).
	if existing, err := s.repos.TOTPSecrets.Get(ctx, in.UserID); err == nil && existing.ConfirmedAt != nil {
		return TOTPSecret{}, ErrAlreadyEnrolled
	} else if err != nil && !database.IsNoRows(err) {
		return TOTPSecret{}, fmt.Errorf("mfa: lookup existing totp: %w", err)
	}
	gen, err := totp.Generate(s.cfg.TOTP, in.Email)
	if err != nil {
		return TOTPSecret{}, fmt.Errorf("mfa: generate totp: %w", err)
	}
	enc, err := s.crypto.Seal(gen.Raw)
	if err != nil {
		return TOTPSecret{}, fmt.Errorf("mfa: encrypt totp secret: %w", err)
	}
	if _, err := s.repos.TOTPSecrets.Upsert(ctx, database.UpsertTOTPSecretParams{
		UserID:           in.UserID,
		SecretCiphertext: enc,
	}); err != nil {
		return TOTPSecret{}, fmt.Errorf("mfa: persist totp secret: %w", err)
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &in.UserID,
		Action:       audit.ActionMFAEnroll,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &in.UserID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": "totp", "step": "enroll"},
	})
	return TOTPSecret{Raw: gen.Raw, ProvisioningURI: gen.ProvisioningURI}, nil
}

// VerifyTOTP marks the user's pending TOTP secret as confirmed. The user
// supplies a 6-digit code from their authenticator app; if it validates,
// confirmed_at is set and the factor becomes effective.
func (s *Service) VerifyTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	row, err := s.repos.TOTPSecrets.Get(ctx, userID)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrNotEnrolled
		}
		return fmt.Errorf("mfa: load totp secret: %w", err)
	}
	raw, err := s.crypto.Open(row.Secret)
	if err != nil {
		return fmt.Errorf("mfa: decrypt totp secret: %w", err)
	}
	if err := totp.Validate(s.cfg.TOTP, raw, code); err != nil {
		auditEmit(s, ctx, audit.Event{
			ActorUserID:  &userID,
			Action:       audit.ActionMFAVerify,
			ResourceType: ResourceMFAFactor,
			ResourceID:   &userID,
			Status:       audit.StatusFailure,
			Metadata:     map[string]any{"factor": "totp"},
		})
		return ErrInvalidChallenge
	}
	if err := s.repos.TOTPSecrets.Confirm(ctx, userID); err != nil {
		return fmt.Errorf("mfa: confirm totp: %w", err)
	}
	// First successful factor enrollment: mint a fresh batch of recovery
	// codes so the user always has a recovery path.
	if _, err := s.regenerateRecoveryCodes(ctx, userID); err != nil {
		// Non-fatal: the user can still use TOTP, and the dashboard will
		// prompt them to generate recovery codes later. Log via audit.
		auditEmit(s, ctx, audit.Event{
			ActorUserID:  &userID,
			Action:       audit.ActionMFARecoveryRefresh,
			ResourceType: ResourceMFAFactor,
			ResourceID:   &userID,
			Status:       audit.StatusFailure,
			Metadata:     map[string]any{"reason": "post_totp_verify"},
		})
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionMFAVerify,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": "totp"},
	})
	return nil
}

// DisableTOTP removes the user's TOTP factor. Per WS-07c open question the
// default is "require current password to disable"; the caller (api
// handler) verifies the password before calling this method.
func (s *Service) DisableTOTP(ctx context.Context, userID uuid.UUID) error {
	if err := s.repos.TOTPSecrets.Delete(ctx, userID); err != nil {
		return fmt.Errorf("mfa: delete totp secret: %w", err)
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionMFADisable,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": "totp"},
	})
	return nil
}

// =====================================================================
// Factor enrollment: WebAuthn
// =====================================================================

// webauthnUser adapts a Lahijan user + their stored WebAuthn credentials
// into the webauthn.User interface the RP expects.
type webauthnUser struct {
	id    uuid.UUID
	email string
	creds []webauthn.Credential
}

func (u *webauthnUser) WebAuthnID() []byte                         { return u.id[:] }
func (u *webauthnUser) WebAuthnName() string                       { return u.email }
func (u *webauthnUser) WebAuthnDisplayName() string                { return u.email }
func (u *webauthnUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// loadWebauthnUser builds the webauthnUser for the given userID by
// fetchinging the user's email and every stored credential.
func (s *Service) loadWebauthnUser(ctx context.Context, userID uuid.UUID) (*webauthnUser, error) {
	user, err := s.repos.Users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("mfa: load user for webauthn: %w", err)
	}
	rows, err := s.repos.WebauthnCreds.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("mfa: list webauthn creds: %w", err)
	}
	creds := make([]webauthn.Credential, 0, len(rows))
	for _, r := range rows {
		credID, err := base64.RawURLEncoding.DecodeString(r.CredentialID)
		if err != nil {
			// Skip rows with malformed credential_id rather than failing
			// the whole load — the data was written by Lahijan itself,
			// so this should never trigger; if it does the user can
			// revoke the bad row from the dashboard.
			continue
		}
		creds = append(creds, webauthn.Credential{
			ID:        credID,
			PublicKey: r.PublicKey,
			SignCount: uint32(r.SignCount),
		})
	}
	return &webauthnUser{id: user.ID, email: user.Email, creds: creds}, nil
}

// BeginWebAuthnRegistration starts the registration ceremony. Returns the
// JSON the browser needs to call navigator.credentials.create() plus the
// ceremony session the RP needs to remember between this call and
// FinishWebAuthnRegistration.
//
// The api handler MUST persist ceremonySession (in mfa_pending_sessions or
// a dedicated row) and hand it back unchanged at /finish.
func (s *Service) BeginWebAuthnRegistration(
	ctx context.Context,
	userID uuid.UUID,
) (creation *webauthn.CredentialCreation, ceremonySession webauthn.SessionData, err error) {
	user, err := s.loadWebauthnUser(ctx, userID)
	if err != nil {
		return nil, webauthn.SessionData{}, err
	}
	if s.rp == nil {
		return nil, webauthn.SessionData{}, errors.New("mfa: webauthn is not configured")
	}
	c, sess, err := s.rp.BeginRegistration(user)
	if err != nil {
		return nil, webauthn.SessionData{}, err
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionMFAEnroll,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": "webauthn", "step": "begin_registration"},
	})
	return c, sess, nil
}

// FinishWebAuthnRegistration completes the registration ceremony. The
// api handler runs webauthn.ParseCreationResponseBody on the request body
// and passes the result here along with the ceremony session from
// BeginWebAuthnRegistration.
func (s *Service) FinishWebAuthnRegistration(
	ctx context.Context,
	userID uuid.UUID,
	ceremonySession webauthn.SessionData,
	parsed *webauthn.ParsedCreation,
	name string,
) error {
	user, err := s.loadWebauthnUser(ctx, userID)
	if err != nil {
		return err
	}
	if s.rp == nil {
		return errors.New("mfa: webauthn is not configured")
	}
	cred, err := s.rp.FinishRegistration(user, ceremonySession, parsed)
	if err != nil {
		return fmt.Errorf("mfa: finish webauthn registration: %w", err)
	}
	if _, err := s.repos.WebauthnCreds.Create(ctx, database.CreateWebauthnCredentialParams{
		UserID:       userID,
		CredentialID: base64.RawURLEncoding.EncodeToString(cred.ID),
		PublicKey:    cred.PublicKey,
		SignCount:    int64(cred.SignCount),
		Transports:   cred.Transport,
		Name:         name,
	}); err != nil {
		return fmt.Errorf("mfa: persist webauthn credential: %w", err)
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionMFAEnroll,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": "webauthn", "step": "finish_registration"},
	})
	return nil
}

// BeginWebAuthnLogin starts the WebAuthn authentication ceremony for the
// user identified by the pending session token. Used during a challenge
// flow when the user picked WebAuthn as the factor.
func (s *Service) BeginWebAuthnLogin(
	ctx context.Context,
	userID uuid.UUID,
) (assertion *webauthn.CredentialAssertion, ceremonySession webauthn.SessionData, err error) {
	user, err := s.loadWebauthnUser(ctx, userID)
	if err != nil {
		return nil, webauthn.SessionData{}, err
	}
	if s.rp == nil {
		return nil, webauthn.SessionData{}, errors.New("mfa: webauthn is not configured")
	}
	return s.rp.BeginLogin(user)
}

// FinishWebAuthnLogin completes the WebAuthn authentication ceremony,
// bumps the stored sign counter, and returns the credential id of the
// factor that was used.
func (s *Service) FinishWebAuthnLogin(
	ctx context.Context,
	userID uuid.UUID,
	ceremonySession webauthn.SessionData,
	parsed *webauthn.ParsedAssertion,
) (uuid.UUID, error) {
	user, err := s.loadWebauthnUser(ctx, userID)
	if err != nil {
		return uuid.Nil, err
	}
	if s.rp == nil {
		return uuid.Nil, errors.New("mfa: webauthn is not configured")
	}
	cred, err := s.rp.FinishLogin(user, ceremonySession, parsed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("mfa: finish webauthn login: %w", err)
	}
	credIDStr := base64.RawURLEncoding.EncodeToString(cred.ID)
	row, err := s.repos.WebauthnCreds.GetByUserAndID(ctx, userID, credIDStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("mfa: load webauthn credential after login: %w", err)
	}
	if err := s.repos.WebauthnCreds.UpdateSignCount(ctx, row.ID, userID, int64(cred.SignCount)); err != nil {
		return uuid.Nil, fmt.Errorf("mfa: bump webauthn sign count: %w", err)
	}
	return row.ID, nil
}

// DeleteWebAuthnCredential revokes one credential (revoke factor).
func (s *Service) DeleteWebAuthnCredential(ctx context.Context, userID, credID uuid.UUID) error {
	if err := s.repos.WebauthnCreds.Delete(ctx, credID, userID); err != nil {
		return fmt.Errorf("mfa: delete webauthn credential: %w", err)
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionMFADisable,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &credID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": "webauthn"},
	})
	return nil
}

// =====================================================================
// Recovery codes
// =====================================================================

// regenerateRecoveryCodes wipes the user's existing codes and issues a
// fresh batch. The raw codes are returned to the caller exactly once.
// Called by:
//   - VerifyTOTP (first successful factor enrollment)
//   - RegenerateRecoveryCodes (explicit user action)
func (s *Service) regenerateRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if err := s.repos.RecoveryCodes.DeleteAllForUser(ctx, userID); err != nil {
		return nil, fmt.Errorf("mfa: wipe recovery codes: %w", err)
	}
	batch, err := recovery.Generate(s.cfg.Recovery)
	if err != nil {
		return nil, fmt.Errorf("mfa: generate recovery codes: %w", err)
	}
	for _, hash := range batch.Hashes {
		if _, err := s.repos.RecoveryCodes.Create(ctx, userID, hash); err != nil {
			return nil, fmt.Errorf("mfa: persist recovery code: %w", err)
		}
	}
	return batch.Raw, nil
}

// RegenerateRecoveryCodes is the user-facing entry point: wipes the old
// codes and issues a fresh batch. The raw codes are returned to the api
// handler exactly once.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	raw, err := s.regenerateRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionMFARecoveryRefresh,
		ResourceType: ResourceMFAFactor,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
	})
	return raw, nil
}

// ListRecoveryCodes returns the user's recovery code rows (without the
// raw codes, which are not stored). The dashboard uses this to show the
// "X of 10 codes remaining" counter.
func (s *Service) ListRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]database.UserRecoveryCodeRow, error) {
	rows, err := s.repos.RecoveryCodes.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("mfa: list recovery codes: %w", err)
	}
	return rows, nil
}

// =====================================================================
// Policy
// =====================================================================

// IsMFARequired reports whether the user must complete an MFA challenge
// before the real session is issued. Returns true if any of:
//   - The user has at least one enrolled factor (opt-in).
//   - Any of the user's tenant memberships has mfa_required = TRUE.
func (s *Service) IsMFARequired(ctx context.Context, userID uuid.UUID) (bool, error) {
	// Per-user opt-in: has any factor?
	if s.hasEnrolledFactor(ctx, userID) {
		return true, nil
	}
	// Per-tenant policy: any tenant requires MFA?
	tenantRequired, err := s.repos.Memberships.AnyTenantRequiresMFA(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("mfa: check tenant mfa policy: %w", err)
	}
	return tenantRequired, nil
}

// hasEnrolledFactor reports whether the user has at least one confirmed factor.
// Used by both IsMFARequired (decide if challenge is needed) and the login
// flow (decide if MFA-required-by-policy can be satisfied at all).
func (s *Service) hasEnrolledFactor(ctx context.Context, userID uuid.UUID) bool {
	// TOTP: confirmed_at must be non-null.
	if row, err := s.repos.TOTPSecrets.Get(ctx, userID); err == nil && row.ConfirmedAt != nil {
		return true
	}
	// WebAuthn: any credential row counts.
	if count, err := s.repos.WebauthnCreds.CountForUser(ctx, userID); err == nil && count > 0 {
		return true
	}
	return false
}

// =====================================================================
// Login flow: pending session + challenge
// =====================================================================

// PendingSession is the result of BeginLogin: the opaque token the client
// carries through the MFA challenge and the row id the service uses
// internally.
type PendingSession struct {
	Token  string // raw opaque token — returned to the client
	UserID uuid.UUID
}

// BeginLogin issues a short-lived, single-use pending_session_token for
// the given user. The real session + refresh token are NOT issued yet;
// the user must complete Challenge() to convert this token into a real
// session. Used by the api handler at the end of a successful password
// or external-IdP step when IsMFARequired returned true.
func (s *Service) BeginLogin(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (PendingSession, error) {
	// If the policy requires MFA but the user has NO factor enrolled,
	// short-circuit: the login is rejected with a clear message so the
	// dashboard can prompt for enrollment.
	if !s.hasEnrolledFactor(ctx, userID) {
		required, err := s.IsMFARequired(ctx, userID)
		if err != nil {
			return PendingSession{}, err
		}
		if required {
			return PendingSession{}, ErrMFARequiredUnenrolled
		}
		// Neither enrolled nor required: no MFA needed at all.
		return PendingSession{}, nil
	}
	raw, hash, err := s.signer.Issue(s.PendingTokenByteLen())
	if err != nil {
		return PendingSession{}, fmt.Errorf("mfa: issue pending token: %w", err)
	}
	expires := time.Now().Add(s.cfg.PendingLifetime)
	if _, err := s.repos.MFAPending.Create(ctx, database.CreateMFAPendingSessionParams{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: expires,
		UserAgent: ua,
		IPAddress: ip,
	}); err != nil {
		return PendingSession{}, fmt.Errorf("mfa: persist pending session: %w", err)
	}
	return PendingSession{Token: raw, UserID: userID}, nil
}

// ChallengeInput carries the fields the challenge endpoint needs. The
// pending token identifies the user (and the brute-force counter); kind
// selects which factor to verify; code carries the user-typed value for
// TOTP / recovery; the webauthn fields carry the ceremony artifacts.
//
// On success, the Challenge method consumes the pending token and issues
// the real session + refresh token via the SessionOpener. On failure, it
// bumps the failure counter; at MaxAttempts the pending token is revoked
// (brute-force lockout).
//
// kind selects which factor to verify:
//   - "totp"     — code is a 6-digit TOTP value
//   - "recovery" — code is a recovery code in canonical form
//   - "webauthn" — webauthnData carries the ceremony session + parsed assertion
//
// The webauthn ceremony session is passed by value because the api handler
// keeps it in a short-lived cookie / request body, not on the pending row.
type ChallengeInput struct {
	Token             string
	Kind              string
	Code              string
	WebAuthnSession   webauthn.SessionData
	WebAuthnAssertion *webauthn.ParsedAssertion
}

// ChallengeResult is the outcome of a successful Challenge: the real
// session + refresh token values to ship to the client.
type ChallengeResult struct {
	Session SessionOpen
}

// Challenge verifies the MFA code against the pending session.
func (s *Service) Challenge(ctx context.Context, in ChallengeInput) (ChallengeResult, error) {
	// Resolve + validate the pending token.
	hash, err := s.signer.Verify(in.Token)
	if err != nil {
		return ChallengeResult{}, ErrPendingNotFound
	}
	row, err := s.repos.MFAPending.GetByHash(ctx, hash)
	if err != nil {
		if database.IsNoRows(err) {
			return ChallengeResult{}, ErrPendingNotFound
		}
		return ChallengeResult{}, fmt.Errorf("mfa: load pending session: %w", err)
	}
	if row.RevokedAt != nil {
		return ChallengeResult{}, ErrPendingRevoked
	}
	if row.ConsumedAt != nil {
		return ChallengeResult{}, ErrPendingConsumed
	}
	if time.Now().After(row.ExpiresAt) {
		_ = s.repos.MFAPending.Revoke(ctx, row.ID)
		return ChallengeResult{}, ErrPendingExpired
	}

	// Verify the factor.
	ok, err := s.verifyFactor(ctx, row.UserID, in)
	if err != nil {
		return ChallengeResult{}, err
	}
	if !ok {
		_ = s.repos.MFAPending.IncFailures(ctx, row.ID)
		// Re-fetch to read the post-increment counter.
		updated, refErr := s.repos.MFAPending.GetByHash(ctx, hash)
		if refErr == nil && int(updated.FailedAttempts) >= s.cfg.MaxAttempts {
			_ = s.repos.MFAPending.Revoke(ctx, row.ID)
			auditEmit(s, ctx, audit.Event{
				ActorUserID:  &row.UserID,
				Action:       audit.ActionMFAChallengeFail,
				ResourceType: ResourceMFAPending,
				ResourceID:   &row.ID,
				Status:       audit.StatusFailure,
				Metadata:     map[string]any{"reason": "max_attempts"},
			})
			return ChallengeResult{}, ErrTooManyAttempts
		}
		auditEmit(s, ctx, audit.Event{
			ActorUserID:  &row.UserID,
			Action:       audit.ActionMFAChallengeFail,
			ResourceType: ResourceMFAPending,
			ResourceID:   &row.ID,
			Status:       audit.StatusFailure,
		})
		return ChallengeResult{}, ErrInvalidChallenge
	}

	// Success: consume the pending token and issue the real session.
	if cerr := s.repos.MFAPending.Consume(ctx, row.ID); cerr != nil {
		return ChallengeResult{}, fmt.Errorf("mfa: consume pending session: %w", cerr)
	}
	sess, oerr := s.sessions.OpenForExistingUser(ctx, row.UserID, row.UserAgent, row.IpAddress)
	if oerr != nil {
		return ChallengeResult{}, fmt.Errorf("mfa: open session after challenge: %w", oerr)
	}
	auditEmit(s, ctx, audit.Event{
		ActorUserID:  &row.UserID,
		Action:       audit.ActionMFAChallenge,
		ResourceType: ResourceMFAPending,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"factor": in.Kind},
	})
	return ChallengeResult{Session: sess}, nil
}

// verifyFactor dispatches to the right factor verifier based on kind.
// Returns (true, nil) on success; (false, nil) on a verification failure
// that should be counted toward the brute-force threshold; (false, err)
// on a system-level error that should abort the challenge.
func (s *Service) verifyFactor(ctx context.Context, userID uuid.UUID, in ChallengeInput) (bool, error) {
	switch in.Kind {
	case "totp":
		row, err := s.repos.TOTPSecrets.Get(ctx, userID)
		if err != nil {
			if database.IsNoRows(err) {
				//nolint:nilerr // "no rows" means the user has no TOTP
				// factor — treat as a failed challenge rather than a
				// system error so the brute-force counter ticks.
				return false, nil
			}
			return false, fmt.Errorf("mfa: load totp secret for challenge: %w", err)
		}
		raw, err := s.crypto.Open(row.Secret)
		if err != nil {
			return false, fmt.Errorf("mfa: decrypt totp secret: %w", err)
		}
		if err := totp.Validate(s.cfg.TOTP, raw, in.Code); err != nil {
			//nolint:nilerr // invalid 6-digit code is a failed challenge,
			// not a system error.
			return false, nil
		}
		return true, nil
	case "recovery":
		norm, err := recovery.Normalize(in.Code)
		if err != nil {
			//nolint:nilerr // malformed recovery code is a failed
			// challenge, not a system error.
			return false, nil
		}
		hash := recovery.Hash(norm)
		row, err := s.repos.RecoveryCodes.LookupUnused(ctx, userID, hash)
		if err != nil {
			if database.IsNoRows(err) {
				//nolint:nilerr // unknown / already-used recovery code
				// is a failed challenge, not a system error.
				return false, nil
			}
			return false, fmt.Errorf("mfa: lookup recovery code: %w", err)
		}
		if err := s.repos.RecoveryCodes.Consume(ctx, row.ID, userID); err != nil {
			return false, fmt.Errorf("mfa: consume recovery code: %w", err)
		}
		return true, nil
	case "webauthn":
		if s.rp == nil {
			return false, errors.New("mfa: webauthn is not configured")
		}
		if _, err := s.FinishWebAuthnLogin(ctx, userID, in.WebAuthnSession, in.WebAuthnAssertion); err != nil {
			//nolint:nilerr // assertion verification failure is a failed
			// challenge, not a system error.
			return false, nil
		}
		return true, nil
	default:
		return false, fmt.Errorf("mfa: unknown challenge kind %q", in.Kind)
	}
}

// PendingTokenByteLen exposes the configured byte length for tests.
func (s *Service) PendingTokenByteLen() int { return 32 }

// WebAuthnEnabled reports whether the WebAuthn relying-party is wired.
// The api handler uses this to short-circuit with a 501 "feature disabled"
// envelope when WebAuthn is not configured (dev without RPID).
func (s *Service) WebAuthnEnabled() bool { return s.rp != nil }

// HasTOTP reports whether the user has a CONFIRMED TOTP factor. Used by
// the login flow to populate enrolledFactors in the 202 response so the
// dashboard can pick the right default challenge UI.
func (s *Service) HasTOTP(ctx context.Context, userID uuid.UUID) bool {
	row, err := s.repos.TOTPSecrets.Get(ctx, userID)
	if err != nil {
		return false
	}
	return row.ConfirmedAt != nil
}

// HasWebAuthn reports whether the user has any WebAuthn credential. Used
// by the login flow to populate enrolledFactors in the 202 response.
func (s *Service) HasWebAuthn(ctx context.Context, userID uuid.UUID) bool {
	count, err := s.repos.WebauthnCreds.CountForUser(ctx, userID)
	if err != nil {
		return false
	}
	return count > 0
}

// auditEmit is the best-effort audit emit; failures are logged but do not
// block the MFA flow. Extracted as a helper so callers stay readable.
func auditEmit(s *Service, ctx context.Context, ev audit.Event) {
	if s == nil || s.audit == nil {
		return
	}
	_, _ = s.audit.Emit(ctx, ev)
}
