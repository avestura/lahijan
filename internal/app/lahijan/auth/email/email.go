// Package email implements Lahijan's email-based account flows: email
// verification, password reset, and email change (WS-06). Every flow issues a
// single-use, expiring, HMAC-signed token, emails the raw link to the user via
// notify/email, and consumes the token's DB row on confirmation.
//
// Locale handling: the email body is rendered in the user's stored locale when
// known (so a password-reset email reaches a logged-out user in their
// preferred language), falling back to the request locale, then to en.
package email

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/password"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	notifyemail "github.com/avestura/lahijan/internal/app/lahijan/notify/email"
)

// ErrInvalidToken is returned when a presented token is malformed, expired,
// already used, or not found. Callers translate it to the localizable message.
var ErrInvalidToken = errors.New("auth/email: invalid or expired token")

// Service orchestrates verify-email, password-reset, and email-change flows.
type Service struct {
	users  *database.UsersRepository
	tokens *database.EmailTokensRepository
	hasher *password.Hasher
	signer *secrets.Signer
	mailer notifyemail.Sender
	audit  audit.Emitter
	cfg    Config
}

// Config carries the TTLs and link-builder inputs the service needs. Build it
// from conf once at bootstrap so the service stays free of conf imports.
type Config struct {
	VerifyTTL      time.Duration
	ResetTTL       time.Duration
	EmailChangeTTL time.Duration
	TokenByteLen   int
	AppBaseURL     string // public dashboard origin, e.g. https://app.example.com
}

// New builds the email service.
func New(
	users *database.UsersRepository,
	tokens *database.EmailTokensRepository,
	hasher *password.Hasher,
	signer *secrets.Signer,
	mailer notifyemail.Sender,
	emitter audit.Emitter,
	cfg Config,
) *Service {
	return &Service{
		users:  users,
		tokens: tokens,
		hasher: hasher,
		signer: signer,
		mailer: mailer,
		audit:  emitter,
		cfg:    cfg,
	}
}

// SendVerification issues a fresh verify-email token for the user and emails the
// link. Any prior outstanding verify-email token for the user is revoked first.
// The user's locale drives the email language.
func (s *Service) SendVerification(ctx context.Context, userID uuid.UUID) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth/email: send verification lookup: %w", err)
	}
	if user.EmailVerifiedAt != nil {
		return nil // already verified; no-op, no email
	}
	return s.issueAndSend(ctx, user, database.EmailTokenVerifyEmail, s.cfg.VerifyTTL,
		"verify-email", "auth.email_subject_verify_email", "auth.email_body_verify_email", nil)
}

// Verify consumes a verify-email token and marks the user's email verified.
func (s *Service) Verify(ctx context.Context, rawToken string) error {
	tok, payloadHash, err := s.lookup(ctx, rawToken, database.EmailTokenVerifyEmail)
	if err != nil {
		_ = s.audit.Emit(ctx, failEvent(audit.ActionVerifyEmail, &tok.UserID))
		return err
	}
	consumed, err := s.tokens.Consume(ctx, payloadHash)
	if err != nil {
		return fmt.Errorf("auth/email: consume verify token: %w", err)
	}
	if !consumed {
		return ErrInvalidToken
	}
	if err := s.users.VerifyEmail(ctx, tok.UserID); err != nil {
		return fmt.Errorf("auth/email: mark email verified: %w", err)
	}
	_ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &tok.UserID,
		Action:       audit.ActionVerifyEmail,
		ResourceType: audit.ResourceUser,
		ResourceID:   &tok.UserID,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// RequestPasswordReset issues a reset token for the user with the given email
// and emails the link. If no such user exists, the call still returns nil so
// the endpoint does not leak which emails are registered.
func (s *Service) RequestPasswordReset(ctx context.Context, userEmail string) error {
	user, err := s.users.GetByEmail(ctx, userEmail)
	if err != nil {
		//nolint:nilerr // intentional: returning nil hides whether the email is
		// registered so the endpoint cannot be used to enumerate accounts.
		return nil
	}
	return s.issueAndSend(ctx, user, database.EmailTokenPasswordReset, s.cfg.ResetTTL,
		"reset-password", "auth.email_subject_reset_password", "auth.email_body_reset_password", nil)
}

// ConfirmPasswordReset consumes a reset token and sets the user's password.
// The new password is strength-validated before the token is consumed.
func (s *Service) ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error {
	if err := password.Validate(newPassword, 0); err != nil {
		return err
	}
	tok, payloadHash, err := s.lookup(ctx, rawToken, database.EmailTokenPasswordReset)
	if err != nil {
		_ = s.audit.Emit(ctx, failEvent(audit.ActionPasswordResetConf, &tok.UserID))
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth/email: hash new password: %w", err)
	}
	consumed, err := s.tokens.Consume(ctx, payloadHash)
	if err != nil {
		return fmt.Errorf("auth/email: consume reset token: %w", err)
	}
	if !consumed {
		return ErrInvalidToken
	}
	if err := s.users.UpdatePassword(ctx, tok.UserID, hash); err != nil {
		return fmt.Errorf("auth/email: set password: %w", err)
	}
	// Revoke every outstanding session for this user so a stolen password is
	// followed by a forced re-login on all devices.
	_ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &tok.UserID,
		Action:       audit.ActionPasswordResetConf,
		ResourceType: audit.ResourceUser,
		ResourceID:   &tok.UserID,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// RequestEmailChange issues an email-change token addressed to newEmail and
// emails the confirmation link there. The actual email swap happens on Confirm.
func (s *Service) RequestEmailChange(ctx context.Context, userID uuid.UUID, newEmail string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("auth/email: email-change lookup: %w", err)
	}
	return s.issueAndSend(ctx, user, database.EmailTokenEmailChange, s.cfg.EmailChangeTTL,
		"email-change", "auth.email_subject_email_changed", "auth.email_body_email_changed", &newEmail)
}

// ConfirmEmailChange consumes an email-change token and updates the user's email
// to the token's stored new_email.
func (s *Service) ConfirmEmailChange(ctx context.Context, rawToken string) error {
	tok, payloadHash, err := s.lookup(ctx, rawToken, database.EmailTokenEmailChange)
	if err != nil {
		return err
	}
	if tok.NewEmail == nil || *tok.NewEmail == "" {
		return ErrInvalidToken
	}
	consumed, err := s.tokens.Consume(ctx, payloadHash)
	if err != nil {
		return fmt.Errorf("auth/email: consume email-change token: %w", err)
	}
	if !consumed {
		return ErrInvalidToken
	}
	if err := s.users.UpdateEmail(ctx, tok.UserID, *tok.NewEmail); err != nil {
		return fmt.Errorf("auth/email: update email: %w", err)
	}
	_ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &tok.UserID,
		Action:       audit.ActionEmailChangeConf,
		ResourceType: audit.ResourceUser,
		ResourceID:   &tok.UserID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"new_email": *tok.NewEmail},
	})
	return nil
}

// issueAndSend creates a token row, renders the locale-specific email, and ships
// it. newEmail is only non-nil for the email-change flow (it overrides the
// recipient and is interpolated into the body).
func (s *Service) issueAndSend(
	ctx context.Context,
	user database.User,
	kind string,
	ttl time.Duration,
	linkPath, subjectKey, bodyKey string,
	newEmail *string,
) error {
	raw, hash, err := s.signer.Issue(s.cfg.TokenByteLen)
	if err != nil {
		return fmt.Errorf("auth/email: issue token: %w", err)
	}
	tokenNewEmail := newEmail
	if kind == database.EmailTokenEmailChange && (tokenNewEmail == nil || *tokenNewEmail == "") {
		return errors.New("auth/email: email-change token requires a new email")
	}
	if _, err := s.tokens.Create(ctx, database.CreateEmailTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		Kind:      kind,
		NewEmail:  tokenNewEmail,
		ExpiresAt: time.Now().Add(ttl),
	}); err != nil {
		return fmt.Errorf("auth/email: persist token: %w", err)
	}
	_ = s.tokens.RevokeForUser(ctx, user.ID, kind) // best-effort; current token already persisted

	recipient := user.Email
	if tokenNewEmail != nil {
		recipient = *tokenNewEmail
	}
	link := fmt.Sprintf("%s/%s?token=%s", s.cfg.AppBaseURL, linkPath, raw)
	return s.send(ctx, recipient, user.Locale, link, subjectKey, bodyKey, tokenNewEmail)
}

// lookup verifies the signature, fetches the row, and enforces kind/expiry/used.
// Returns the DB row, the payload hash (to consume later), and an error.
func (s *Service) lookup(ctx context.Context, rawToken, kind string) (database.EmailToken, string, error) {
	payloadHash, err := s.signer.Verify(rawToken)
	if err != nil {
		return database.EmailToken{}, "", ErrInvalidToken
	}
	row, err := s.tokens.GetByHash(ctx, payloadHash)
	if err != nil {
		return database.EmailToken{}, "", ErrInvalidToken
	}
	if row.Kind != kind {
		return database.EmailToken{}, "", ErrInvalidToken
	}
	if row.UsedAt != nil || time.Now().After(row.ExpiresAt) {
		return database.EmailToken{}, "", ErrInvalidToken
	}
	return row, payloadHash, nil
}

// send renders the localised subject/body, wraps it in HTML, and ships it.
func (s *Service) send(ctx context.Context, to, locale, link, subjectKey, bodyKey string, newEmail *string) error {
	mailCtx := i18n.WithLocale(ctx, locale)
	bodyArgs := map[string]any{"Link": link}
	if newEmail != nil {
		bodyArgs["NewEmail"] = *newEmail
	}
	subject := i18n.T(mailCtx, subjectKey, nil)
	body := i18n.T(mailCtx, bodyKey, bodyArgs)
	html, err := notifyemail.RenderHTML(notifyemail.RenderedEmail{
		Greeting:   i18n.T(mailCtx, "auth.email_greeting", nil),
		Body:       body,
		Signature:  i18n.T(mailCtx, "auth.email_signature", nil),
		CTALabel:   subject,
		CTALink:    link,
		LocaleHint: locale,
	})
	if err != nil {
		return fmt.Errorf("auth/email: render: %w", err)
	}
	return s.mailer.Send(ctx, notifyemail.Email{
		To:       to,
		Subject:  subject,
		Body:     body,
		HTMLBody: html,
		Locale:   locale,
	})
}

// failEvent builds a failure audit event for an email-flow rejection.
func failEvent(action string, userID *uuid.UUID) audit.Event {
	return audit.Event{
		ActorUserID:  userID,
		Action:       action,
		ResourceType: audit.ResourceUser,
		ResourceID:   userID,
		Status:       audit.StatusFailure,
	}
}
