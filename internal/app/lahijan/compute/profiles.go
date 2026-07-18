// Package compute: profiles.go implements the profile CRUD surface. Every
// privileged action emits an audit event; the Incus side is updated via the
// provider driver (Phase 3) so the live profile picks up the change.
package compute

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// ProfileRow is the API-facing shape returned by Get/List.
type ProfileRow = database.ComputeProfile

// CreateProfileParams carries the user-controlled fields of a profile-create call.
type CreateProfileParams struct {
	Name        string
	Description string
	Config      map[string]string
	Devices     map[string]map[string]string
}

// CreateProfile persists the profile row + creates the Incus profile within
// the tenant's project. The audit row covers both halves.
func (s *Service) CreateProfile(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params CreateProfileParams,
) (ProfileRow, error) {
	if params.Name == "" {
		return ProfileRow{}, ErrInvalidName
	}
	parsedConfig := InstanceConfig{
		Config: params.Config, Devices: params.Devices,
	}
	configJSON, _ := json.Marshal(parsedConfig)
	row, err := s.repos.ComputeProfiles.Create(ctx, database.CreateComputeProfileParams{
		Name:        params.Name,
		Description: params.Description,
		ConfigJson:  configJSON,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return ProfileRow{}, fmt.Errorf("%w: name=%s", ErrNameTaken, params.Name)
		}
		return ProfileRow{}, fmt.Errorf("compute: create profile row: %w", err)
	}

	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.profile.create",
		ResourceType: ResourceProfile,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata:     map[string]any{"name": params.Name},
	})

	if s.provider != nil {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.EnsureProject(ctx, tenantID); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			_ = s.repos.ComputeProfiles.SoftDelete(ctx, row.ID)
			return ProfileRow{}, fmt.Errorf("compute: ensure project: %w", err)
		}
		if err := s.provider.CreateProfile(ctx, incus.CreateProfileParams{
			Project:     project,
			Name:        params.Name,
			Description: params.Description,
			Config:      params.Config,
			Devices:     params.Devices,
		}); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			_ = s.repos.ComputeProfiles.SoftDelete(ctx, row.ID)
			return ProfileRow{}, fmt.Errorf("compute: incus create profile: %w", err)
		}
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// GetProfile returns the profile with id within the tenant.
func (s *Service) GetProfile(ctx context.Context, id uuid.UUID) (ProfileRow, error) {
	row, err := s.repos.ComputeProfiles.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return ProfileRow{}, ErrProfileNotFound
		}
		return ProfileRow{}, fmt.Errorf("compute: get profile: %w", err)
	}
	return row, nil
}

// ListProfiles returns a paginated list of the tenant's profiles.
func (s *Service) ListProfiles(
	ctx context.Context,
	limit, offset int32,
) ([]ProfileRow, error) {
	return s.repos.ComputeProfiles.List(ctx, limit, offset)
}

// CountProfiles returns the number of non-deleted profiles in the tenant.
func (s *Service) CountProfiles(ctx context.Context) (int64, error) {
	return s.repos.ComputeProfiles.Count(ctx)
}

// DeleteProfile soft-deletes the profile row + deletes the Incus profile.
func (s *Service) DeleteProfile(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	id uuid.UUID,
) error {
	row, err := s.repos.ComputeProfiles.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("compute: get profile: %w", err)
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.profile.delete",
		ResourceType: ResourceProfile,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})
	if s.provider != nil {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.DeleteProfile(ctx, project, row.Name); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			return fmt.Errorf("compute: incus delete profile: %w", err)
		}
	}
	if err := s.repos.ComputeProfiles.SoftDelete(ctx, id); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return fmt.Errorf("compute: soft delete profile: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}
