// Package settings holds the platform settings an administrator changes at
// runtime from the dashboard. Each setting has a configuration default (an
// environment variable or config key) and an optional stored override: the
// override wins for as long as it exists.
package settings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// KeyRegistrationEnabled is the platform_settings key that overrides
// auth.signup.enabled.
const KeyRegistrationEnabled = "registration_enabled"

// Service reads and writes platform settings.
type Service struct {
	repo  *database.PlatformSettingsRepository
	audit audit.Emitter
	// registrationDefault is the configured default (auth.signup.enabled).
	registrationDefault bool
}

// New builds the service. registrationDefault is the value of
// auth.signup.enabled.
func New(repo *database.PlatformSettingsRepository, emitter audit.Emitter, registrationDefault bool) *Service {
	return &Service{repo: repo, audit: emitter, registrationDefault: registrationDefault}
}

// State is the effective settings plus where they come from, for the admin UI.
type State struct {
	// RegistrationEnabled is the effective value.
	RegistrationEnabled bool
	// RegistrationDefault is the configured default (the environment variable
	// or config key).
	RegistrationDefault bool
	// RegistrationOverridden is true when an administrator's choice is stored
	// and overrides the default.
	RegistrationOverridden bool
}

// RegistrationEnabled reports whether self-registration is allowed right now.
// It falls back to the configured default if the stored value cannot be read,
// so a database hiccup never changes the policy.
func (s *Service) RegistrationEnabled(ctx context.Context) bool {
	v, ok, err := s.repo.GetBool(ctx, KeyRegistrationEnabled)
	if err != nil || !ok {
		return s.registrationDefault
	}
	return v
}

// Get returns the effective settings and their sources.
func (s *Service) Get(ctx context.Context) (State, error) {
	v, ok, err := s.repo.GetBool(ctx, KeyRegistrationEnabled)
	if err != nil {
		return State{}, fmt.Errorf("settings.get: %w", err)
	}
	st := State{RegistrationDefault: s.registrationDefault, RegistrationOverridden: ok}
	st.RegistrationEnabled = s.registrationDefault
	if ok {
		st.RegistrationEnabled = v
	}
	return st, nil
}

// SetRegistration stores the administrator's choice, which overrides the
// configured default.
func (s *Service) SetRegistration(ctx context.Context, actor uuid.UUID, enabled bool) (State, error) {
	raw, err := json.Marshal(enabled)
	if err != nil {
		return State{}, fmt.Errorf("settings.set registration: %w", err)
	}
	var by *uuid.UUID
	if actor != uuid.Nil {
		by = &actor
	}
	if err := s.repo.Set(ctx, KeyRegistrationEnabled, raw, by); err != nil {
		return State{}, fmt.Errorf("settings.set registration: %w", err)
	}
	s.emit(ctx, actor, map[string]any{"setting": KeyRegistrationEnabled, "value": enabled})
	return s.Get(ctx)
}

// ClearRegistration removes the override so the configured default applies
// again.
func (s *Service) ClearRegistration(ctx context.Context, actor uuid.UUID) (State, error) {
	if err := s.repo.Delete(ctx, KeyRegistrationEnabled); err != nil {
		return State{}, fmt.Errorf("settings.clear registration: %w", err)
	}
	s.emit(ctx, actor, map[string]any{"setting": KeyRegistrationEnabled, "value": nil})
	return s.Get(ctx)
}

func (s *Service) emit(ctx context.Context, actor uuid.UUID, meta map[string]any) {
	ev := audit.Event{
		Action: audit.ActionAdminSettingsUpdate, ResourceType: audit.ResourceSettings,
		Status: audit.StatusSuccess, ActorType: audit.ActorUser, Metadata: meta,
	}
	if actor != uuid.Nil {
		ev.ActorUserID = &actor
	}
	// Audit must never roll back the change; the emitter already logged.
	_, _ = s.audit.Emit(ctx, ev)
}
