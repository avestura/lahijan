// Package session implements Lahijan's session lifecycle (WS-06): registration,
// login, logout, refresh-token rotation with reuse detection, and the
// current-user (GET /me) read.
//
// Credential model:
//   - The browser carries an opaque signed cookie whose SHA-256 hash is stored
//     in sessions.token_hash; the cookie value is produced by auth/secrets.
//   - A refresh token (its own cookie) rotates within a family on each /refresh;
//     reusing a rotated token revokes the whole family + the owning session.
//
// The SessionService depends on auth/email for verification/reset emails and on
// auth/password for hashing. It never imports gen directly.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/email"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Sentinel errors. Handlers map these to localizable envelope messages.
var (
	ErrInvalidCredentials = errors.New("auth/session: invalid credentials")
	ErrEmailTaken         = errors.New("auth/session: email already registered")
	ErrUserInactive       = errors.New("auth/session: account disabled")
	ErrEmailUnverified    = errors.New("auth/session: email not verified")
	ErrInvalidToken       = errors.New("auth/session: invalid refresh token")
	ErrRefreshReuse       = errors.New("auth/session: refresh token reuse detected")
	ErrEmailInvalid       = errors.New("auth/session: invalid email")
)

// Service orchestrates register/login/logout/refresh/me.
type Service struct {
	users    *database.UsersRepository
	sessions *database.SessionsRepository
	tokens   *database.TokensRepository
	hasher   *password.Hasher
	signer   *secrets.Signer
	mailer   *email.Service
	audit    audit.Emitter
	cfg      Config

	// provisioner, when set, runs right after a self-service account is
	// created (e.g. to give it a personal tenant). Optional.
	provisioner SignupProvisioner
}

// SignupProvisioner sets up what a brand-new self-registered account needs
// beyond its user row. program.Start wires the personal-tenant provisioner.
type SignupProvisioner interface {
	ProvisionSignup(ctx context.Context, userID uuid.UUID, email string) error
}

// SetSignupProvisioner installs the post-registration hook. Call once at
// bootstrap, before serving traffic.
func (s *Service) SetSignupProvisioner(p SignupProvisioner) {
	s.provisioner = p
}

// Config carries the session/refresh lifetimes and token byte length. Build it
// from conf once at bootstrap.
type Config struct {
	SessionLifetime time.Duration
	RefreshLifetime time.Duration
	TokenByteLen    int
	MinPasswordLen  int
	RequireVerified bool // require verified email before issuing a session (configurable)
}

// New builds the session service.
func New(
	users *database.UsersRepository,
	sessions *database.SessionsRepository,
	tokens *database.TokensRepository,
	hasher *password.Hasher,
	signer *secrets.Signer,
	mailer *email.Service,
	emitter audit.Emitter,
	cfg Config,
) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
		tokens:   tokens,
		hasher:   hasher,
		signer:   signer,
		mailer:   mailer,
		audit:    emitter,
		cfg:      cfg,
	}
}

// Session is the result of Register/Login/Refresh: the row created plus the
// raw values to ship to the client as cookies. The raw values are shown here
// exactly once.
type Session struct {
	UserID      uuid.UUID
	SessionID   uuid.UUID
	ExpiresAt   time.Time
	CookieValue string // raw opaque session cookie value
	Refresh     RefreshIssue
}

// RefreshIssue carries the raw refresh token + its family id.
type RefreshIssue struct {
	Raw       string
	FamilyID  uuid.UUID
	ExpiresAt time.Time
}

// Register creates a user, opens a session, issues a refresh token, and emails a
// verification link. It returns the new session (with cookie values). Email is
// case-normalized to lower-case before storage and lookup.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Session, error) {
	emailNorm := normalizeEmail(in.Email)
	if !isValidEmail(emailNorm) {
		return Session{}, ErrEmailInvalid
	}
	if err := password.Validate(in.Password, s.cfg.MinPasswordLen); err != nil {
		return Session{}, err
	}
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return Session{}, fmt.Errorf("auth/session: hash password: %w", err)
	}
	user, err := s.users.Create(ctx, database.CreateUserParams{
		Email:        emailNorm,
		PasswordHash: &hash,
		DisplayName:  in.DisplayName,
		Locale:       in.Locale,
	})
	if err != nil {
		if isUniqueViolationEmail(err) {
			return Session{}, ErrEmailTaken
		}
		return Session{}, fmt.Errorf("auth/session: create user: %w", err)
	}

	// Best-effort: the account exists either way, so a provisioning failure
	// is logged rather than failing a registration that already happened.
	if s.provisioner != nil {
		if perr := s.provisioner.ProvisionSignup(ctx, user.ID, emailNorm); perr != nil {
			slog.WarnContext(ctx, "auth/session: signup provisioning failed",
				"user_id", user.ID, "error", perr.Error())
		}
	}

	sess, err := s.openSession(ctx, user.ID, in.UserAgent, in.IPAddress)
	if err != nil {
		return Session{}, err
	}

	// Best-effort verification email; a failure here must not block registration.
	_ = s.mailer.SendVerification(ctx, user.ID)

	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &user.ID,
		Action:       audit.ActionRegister,
		ResourceType: audit.ResourceUser,
		ResourceID:   &user.ID,
		Status:       audit.StatusSuccess,
	})
	return sess, nil
}

// RegisterInput carries the user-controlled fields of a registration.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName *string
	Locale      string
	UserAgent   *string
	IPAddress   *netip.Addr
}

// Login verifies credentials and opens a session. A disabled account returns
// ErrUserInactive; depending on config, an unverified email blocks login.
func (s *Service) Login(ctx context.Context, in LoginInput) (Session, error) {
	user, err := s.VerifyCredentials(ctx, in.Email, in.Password)
	if err != nil {
		return Session{}, err
	}
	// Lazily rehash on login if parameters were bumped.
	if s.hasher.NeedsRehash(*user.PasswordHash) {
		if newHash, e := s.hasher.Hash(in.Password); e == nil {
			_ = s.users.UpdatePassword(ctx, user.ID, newHash)
		}
	}

	sess, err := s.openSession(ctx, user.ID, in.UserAgent, in.IPAddress)
	if err != nil {
		return Session{}, err
	}
	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &user.ID,
		Action:       audit.ActionLogin,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
	})
	return sess, nil
}

// VerifyCredentials validates the email + password without opening a
// session. Used by the MFA-aware login flow (WS-07c) so the api handler
// can decide between opening the session directly and issuing a pending
// MFA challenge token:
//
//	user, err := sessionSvc.VerifyCredentials(ctx, email, pw)
//	required, _ := mfaSvc.IsMFARequired(ctx, user.ID)
//	if required {
//	    pending, _ := mfaSvc.BeginLogin(ctx, user.ID, ua, ip)
//	    return 202 Accepted { pending_session_token: pending.Token }
//	}
//	sess, _ := sessionSvc.OpenForExistingUser(ctx, user.ID, ua, ip)
//
// Returns the same sentinels as Login: ErrInvalidCredentials,
// ErrUserInactive, ErrEmailUnverified.
//
// On success, the caller is responsible for opening the session via
// OpenForExistingUser (which is the path WS-07a's IdP login also takes).
func (s *Service) VerifyCredentials(ctx context.Context, email, password string) (database.User, error) {
	user, err := s.users.GetByEmail(ctx, normalizeEmail(email))
	if err != nil {
		s.auditFail(ctx, audit.ActionLogin, nil)
		return database.User{}, ErrInvalidCredentials
	}
	if user.PasswordHash == nil {
		s.auditFail(ctx, audit.ActionLogin, &user.ID)
		return database.User{}, ErrInvalidCredentials
	}
	ok, err := s.hasher.Verify(password, *user.PasswordHash)
	if err != nil || !ok {
		s.auditFail(ctx, audit.ActionLogin, &user.ID)
		return database.User{}, ErrInvalidCredentials
	}
	if !user.IsActive {
		s.auditFail(ctx, audit.ActionLogin, &user.ID)
		return database.User{}, ErrUserInactive
	}
	if s.cfg.RequireVerified && user.EmailVerifiedAt == nil {
		s.auditFail(ctx, audit.ActionLogin, &user.ID)
		return database.User{}, ErrEmailUnverified
	}
	return user, nil
}

// LoginInput carries the credentials for a login attempt.
type LoginInput struct {
	Email     string
	Password  string
	UserAgent *string
	IPAddress *netip.Addr
}

// Logout revokes the session that owns the given refresh token and every token
// in its family. It is idempotent and safe to call with an already-revoked token.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	hash, err := s.signer.Verify(rawRefresh)
	if err != nil {
		//nolint:nilerr // idempotent: an unparseable token just means nothing
		// to revoke; logout must succeed so clients can always clean up.
		return nil
	}
	rt, err := s.tokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		//nolint:nilerr // idempotent: an unknown token has nothing to revoke.
		return nil
	}
	_ = s.tokens.RevokeRefreshTokenFamily(ctx, rt.FamilyID)
	_ = s.sessions.Revoke(ctx, rt.SessionID)
	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &rt.UserID,
		Action:       audit.ActionLogout,
		ResourceType: audit.ResourceSession,
		ResourceID:   &rt.SessionID,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// Refresh rotates a refresh token within its family. Reusing a previously
// rotated token revokes the entire family + the session (reuse detection) and
// returns ErrRefreshReuse so the handler can force the client to re-authenticate.
func (s *Service) Refresh(ctx context.Context, rawRefresh string, ua *string, ip *netip.Addr) (Session, error) {
	hash, err := s.signer.Verify(rawRefresh)
	if err != nil {
		return Session{}, ErrInvalidToken
	}
	rt, err := s.tokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return Session{}, ErrInvalidToken
	}
	if rt.RevokedAt != nil {
		// Reuse of a revoked token: the family is compromised. Burn it all down.
		_ = s.tokens.RevokeRefreshTokenFamily(ctx, rt.FamilyID)
		_ = s.sessions.Revoke(ctx, rt.SessionID)
		// Best-effort: audit failure is logged but does not block the auth flow.
		_, _ = s.audit.Emit(ctx, audit.Event{
			ActorUserID:  &rt.UserID,
			Action:       audit.ActionRefreshReuse,
			ResourceType: audit.ResourceSession,
			ResourceID:   &rt.SessionID,
			Status:       audit.StatusFailure,
		})
		return Session{}, ErrRefreshReuse
	}
	if time.Now().After(rt.ExpiresAt) {
		_ = s.tokens.RevokeRefreshToken(ctx, hash)
		return Session{}, ErrInvalidToken
	}

	// Rotate: revoke the presented token and issue a fresh one in the same family.
	_ = s.tokens.RevokeRefreshToken(ctx, hash)
	sess, err := s.continueSession(ctx, rt, ua, ip)
	if err != nil {
		return Session{}, err
	}
	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &rt.UserID,
		Action:       audit.ActionRefresh,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
	})
	return sess, nil
}

// openSession creates a session row, mints the first refresh token of a new
// family, and returns the Session carrying both raw cookie values.
func (s *Service) openSession(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (Session, error) {
	now := time.Now()
	expires := now.Add(s.cfg.SessionLifetime)

	rawCookie, cookieHash, err := s.signer.Issue(s.cfg.TokenByteLen)
	if err != nil {
		return Session{}, fmt.Errorf("auth/session: issue session token: %w", err)
	}
	sessRow, err := s.sessions.Create(ctx, database.CreateSessionParams{
		UserID:    userID,
		TokenHash: cookieHash,
		ExpiresAt: expires,
		UserAgent: ua,
		IPAddress: ip,
	})
	if err != nil {
		return Session{}, fmt.Errorf("auth/session: create session: %w", err)
	}
	refresh, err := s.issueRefresh(ctx, userID, sessRow.ID, uuid.New(), ua, ip)
	if err != nil {
		return Session{}, err
	}
	return Session{
		UserID:      userID,
		SessionID:   sessRow.ID,
		ExpiresAt:   sessRow.ExpiresAt,
		CookieValue: rawCookie,
		Refresh:     refresh,
	}, nil
}

// OpenForExistingUser opens a session + refresh token for a known user.
// Used by the external-IdP (WS-07a) and any future "log this user in without
// re-checking credentials" path (e.g. SSO, passwordless). The caller MUST
// have already authenticated the user through some other channel (OAuth,
// OIDC, etc.); this method does NOT verify credentials.
//
// Emits an audit event with audit.ActionLogin + metadata indicating the
// session was opened via an IdP.
func (s *Service) OpenForExistingUser(
	ctx context.Context,
	userID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (Session, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return Session{}, fmt.Errorf("auth/session: load user for idp login: %w", err)
	}
	if !user.IsActive {
		s.auditFail(ctx, audit.ActionLogin, &user.ID)
		return Session{}, ErrUserInactive
	}
	sess, err := s.openSession(ctx, user.ID, ua, ip)
	if err != nil {
		return Session{}, err
	}
	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &user.ID,
		Action:       audit.ActionIdpLogin,
		ResourceType: audit.ResourceSession,
		ResourceID:   &sess.SessionID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"via": "external_idp"},
	})
	return sess, nil
}

// continueSession reuses the existing session for a refresh rotation: it issues
// a new refresh token in the same family and leaves the session cookie alone.
//
// We deliberately do NOT rotate sessions.token_hash here. Rotating it would
// require persisting the new hash via a dedicated repo method; until that lands
// the security-critical rotation already happens on the refresh token (the
// credential /refresh actually consumes), and reuse detection revokes the whole
// family + the owning session regardless. Leaving the session cookie stable
// means the browser keeps sending a cookie that still validates.
func (s *Service) continueSession(
	ctx context.Context,
	rt database.RefreshToken,
	ua *string,
	ip *netip.Addr,
) (Session, error) {
	sessRow, err := s.sessions.Get(ctx, rt.SessionID)
	if err != nil {
		return Session{}, fmt.Errorf("auth/session: load session on refresh: %w", err)
	}
	refresh, err := s.issueRefresh(ctx, rt.UserID, rt.SessionID, rt.FamilyID, ua, ip)
	if err != nil {
		return Session{}, err
	}
	return Session{
		UserID:      rt.UserID,
		SessionID:   sessRow.ID,
		ExpiresAt:   sessRow.ExpiresAt,
		CookieValue: "", // unchanged; the browser keeps the existing valid cookie
		Refresh:     refresh,
	}, nil
}

// issueRefresh mints a refresh token in the given family + session.
func (s *Service) issueRefresh(
	ctx context.Context,
	userID, sessionID, familyID uuid.UUID,
	ua *string,
	ip *netip.Addr,
) (RefreshIssue, error) {
	raw, hash, err := s.signer.Issue(s.cfg.TokenByteLen)
	if err != nil {
		return RefreshIssue{}, fmt.Errorf("auth/session: issue refresh token: %w", err)
	}
	expires := time.Now().Add(s.cfg.RefreshLifetime)
	if _, err := s.tokens.CreateRefreshToken(ctx, database.CreateRefreshTokenParams{
		UserID:    userID,
		SessionID: sessionID,
		FamilyID:  familyID,
		TokenHash: hash,
		ExpiresAt: expires,
		UserAgent: ua,
		IPAddress: ip,
	}); err != nil {
		return RefreshIssue{}, fmt.Errorf("auth/session: persist refresh token: %w", err)
	}
	return RefreshIssue{Raw: raw, FamilyID: familyID, ExpiresAt: expires}, nil
}

// auditFail records a failed privileged auth action without blocking the return.
func (s *Service) auditFail(ctx context.Context, action string, userID *uuid.UUID) {
	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  userID,
		Action:       action,
		ResourceType: audit.ResourceUser,
		ResourceID:   userID,
		Status:       audit.StatusFailure,
	})
}

// ChangePassword verifies the current password, strength-validates the new one,
// and persists a fresh argon2id hash. Used by PATCH /me. A wrong current
// password returns ErrInvalidCredentials.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	if err := password.Validate(newPassword, s.cfg.MinPasswordLen); err != nil {
		return err
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return ErrInvalidCredentials
	}
	if user.PasswordHash == nil {
		return ErrInvalidCredentials
	}
	ok, err := s.hasher.Verify(currentPassword, *user.PasswordHash)
	if err != nil || !ok {
		return ErrInvalidCredentials
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth/session: hash new password: %w", err)
	}
	if err := s.users.UpdatePassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("auth/session: set password: %w", err)
	}
	// Best-effort: audit failure is logged but does not block the auth flow.
	_, _ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionPasswordChange,
		ResourceType: audit.ResourceUser,
		ResourceID:   &userID,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// normalizeEmail lower-cases and trims an email for storage and lookup.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// isValidEmail is a deliberately permissive RFC-5322-ish check: we trust the DB
// unique constraint for real validation; this only rejects obviously-bad input.
func isValidEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	return strings.IndexByte(s[at+1:], '.') >= 0
}

// isUniqueViolationEmail reports whether err is a Postgres unique-violation on
// the users email index. pgconn.PgError is checked via its Error() text so this
// package does not import pgconn (keeps the auth layer DB-driver-agnostic).
func isUniqueViolationEmail(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "uq_users_email")
}
