// Package pat implements personal access tokens (WS-06): fine-grained, scoped,
// expiring API credentials a user issues for programmatic access. A PAT is
// hashed at rest (its raw form shown exactly once at creation), carries a list
// of permission slugs enforced by RequirePerm in WS-08, and is revoked (never
// deleted) on user request.
package pat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/secrets"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// ErrNameRequired is returned when a PAT is created without a name.
var ErrNameRequired = errors.New("auth/pat: name required")

// ErrInvalidScope is returned when a requested scope is not a valid scope.action slug.
var ErrInvalidScope = errors.New("auth/pat: invalid scope")

// ErrNotFound is returned when a PAT id does not exist for the user.
var ErrNotFound = errors.New("auth/pat: not found")

// Service issues, lists, revokes, and authenticates personal access tokens.
type Service struct {
	tokens *database.TokensRepository
	signer *secrets.Signer
	audit  audit.Emitter
	cfg    Config
}

// Config carries the PAT prefix and entropy length.
type Config struct {
	Prefix    string
	ByteLen   int
	MaxExpiry time.Duration // 0 = no cap
}

// New builds the PAT service.
func New(tokens *database.TokensRepository, signer *secrets.Signer, emitter audit.Emitter, cfg Config) *Service {
	return &Service{tokens: tokens, signer: signer, audit: emitter, cfg: cfg}
}

// CreateInput carries the user-controlled fields of a new PAT.
type CreateInput struct {
	UserID    uuid.UUID
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

// CreateResult is returned from Create: the persisted row plus the raw token,
// shown exactly once. Callers must hand it to the user and never store it.
type CreateResult struct {
	database.PersonalAccessToken
	Raw string
}

// Create issues a PAT and returns it with the raw token (shown once).
func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateResult{}, ErrNameRequired
	}
	if err := validateScopes(in.Scopes); err != nil {
		return CreateResult{}, err
	}
	if in.ExpiresAt != nil && s.cfg.MaxExpiry > 0 {
		deadline := time.Now().Add(s.cfg.MaxExpiry)
		if in.ExpiresAt.After(deadline) {
			in.ExpiresAt = &deadline
		}
	}

	raw, hash, err := s.signer.Issue(s.cfg.ByteLen)
	if err != nil {
		return CreateResult{}, fmt.Errorf("auth/pat: issue token: %w", err)
	}
	display := raw
	if s.cfg.Prefix != "" {
		display = s.cfg.Prefix + raw
	}
	row, err := s.tokens.CreatePersonalAccessToken(ctx, database.CreatePersonalAccessTokenParams{
		UserID:    in.UserID,
		Name:      name,
		TokenHash: hash,
		ExpiresAt: in.ExpiresAt,
		Scopes:    in.Scopes,
	})
	if err != nil {
		return CreateResult{}, fmt.Errorf("auth/pat: persist token: %w", err)
	}
	_ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &in.UserID,
		Action:       audit.ActionPATCreate,
		ResourceType: audit.ResourcePAT,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata:     map[string]any{"name": name, "scopes": in.Scopes},
	})
	return CreateResult{PersonalAccessToken: row, Raw: display}, nil
}

// List returns the user's non-revoked PATs, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]database.PersonalAccessToken, error) {
	rows, err := s.tokens.ListPersonalAccessTokensForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth/pat: list: %w", err)
	}
	return rows, nil
}

// Revoke marks a PAT revoked by id, scoped to the owning user.
func (s *Service) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	row, err := s.tokens.GetPersonalAccessTokenByID(ctx, id)
	if err != nil || row.UserID != userID {
		return ErrNotFound
	}
	if err := s.tokens.RevokePersonalAccessTokenByID(ctx, id, userID); err != nil {
		return fmt.Errorf("auth/pat: revoke: %w", err)
	}
	_ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &userID,
		Action:       audit.ActionPATRevoke,
		ResourceType: audit.ResourcePAT,
		ResourceID:   &id,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// AuthenticateResult is the outcome of validating a presented PAT.
type AuthenticateResult struct {
	UserID uuid.UUID
	Scopes []string
}

// Authenticate validates a raw PAT (without the prefix), updates last-used-at,
// and returns the principal. A missing, revoked, or expired PAT returns an
// error; the handler maps that to 401.
func (s *Service) Authenticate(ctx context.Context, displayToken string) (AuthenticateResult, error) {
	raw := strings.TrimPrefix(displayToken, s.cfg.Prefix)
	hash, err := s.signer.Verify(raw)
	if err != nil {
		return AuthenticateResult{}, ErrNotFound
	}
	row, err := s.tokens.GetPersonalAccessTokenByHash(ctx, hash)
	if err != nil {
		return AuthenticateResult{}, ErrNotFound
	}
	if row.RevokedAt != nil {
		return AuthenticateResult{}, ErrNotFound
	}
	if row.ExpiresAt != nil && time.Now().After(*row.ExpiresAt) {
		return AuthenticateResult{}, ErrNotFound
	}
	_ = s.tokens.TouchPersonalAccessToken(ctx, hash)
	_ = s.audit.Emit(ctx, audit.Event{
		ActorUserID:  &row.UserID,
		Action:       audit.ActionPATUse,
		ResourceType: audit.ResourcePAT,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
	})
	return AuthenticateResult{UserID: row.UserID, Scopes: row.Scopes}, nil
}

// validateScopes enforces that every scope is a non-empty "scope.action" slug.
// The full permission catalog is enforced by RequirePerm in WS-08; here we only
// reject obvious garbage so a typo does not create an unenforceable PAT.
func validateScopes(scopes []string) error {
	for _, sc := range scopes {
		sc = strings.TrimSpace(sc)
		if sc == "" {
			return ErrInvalidScope
		}
		if !strings.Contains(sc, ".") {
			return ErrInvalidScope
		}
	}
	return nil
}
