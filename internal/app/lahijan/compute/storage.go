// Package compute: storage.go implements the per-tenant custom storage
// volume surface. Volumes are project-scoped disks instances attach to via
// a disk device. Every privileged action emits an audit event.
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

// VolumeRow is the API-facing shape returned by Get/List.
type VolumeRow = database.ComputeStorageVolume

// CreateVolumeParams carries the user-controlled fields of a volume-create.
type CreateVolumeParams struct {
	Name        string
	Description string
	PoolName    string
	Config      map[string]string
}

// CreateVolume persists the row + creates the Incus volume.
func (s *Service) CreateVolume(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params CreateVolumeParams,
) (VolumeRow, error) {
	if params.Name == "" {
		return VolumeRow{}, ErrInvalidName
	}
	pool := params.PoolName
	if pool == "" {
		pool = "default"
	}
	configJSON, _ := json.Marshal(params.Config)
	row, err := s.repos.ComputeStorageVolumes.Create(ctx, database.CreateComputeStorageVolumeParams{
		Name:        params.Name,
		Description: params.Description,
		PoolName:    pool,
		Config:      configJSON,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return VolumeRow{}, fmt.Errorf("%w: name=%s", ErrNameTaken, params.Name)
		}
		return VolumeRow{}, fmt.Errorf("compute: create volume row: %w", err)
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.storage_volume.create",
		ResourceType: ResourceVolume,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata:     map[string]any{"name": params.Name, "pool": pool},
	})
	if s.provider != nil {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.EnsureProject(ctx, tenantID); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			_ = s.repos.ComputeStorageVolumes.SoftDelete(ctx, row.ID)
			return VolumeRow{}, fmt.Errorf("compute: ensure project: %w", err)
		}
		if err := s.provider.CreateStorageVolume(ctx, pool, incus.StorageVolumesPost{
			Name:        params.Name,
			Type:        "custom",
			Description: params.Description,
			Config:      params.Config,
			Project:     project,
			Pool:        pool,
		}); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			_ = s.repos.ComputeStorageVolumes.SoftDelete(ctx, row.ID)
			return VolumeRow{}, fmt.Errorf("compute: incus create volume: %w", err)
		}
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// GetVolume returns the volume with id within the tenant.
func (s *Service) GetVolume(ctx context.Context, id uuid.UUID) (VolumeRow, error) {
	row, err := s.repos.ComputeStorageVolumes.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return VolumeRow{}, ErrVolumeNotFound
		}
		return VolumeRow{}, fmt.Errorf("compute: get volume: %w", err)
	}
	return row, nil
}

// ListVolumes returns a paginated list of the tenant's volumes.
func (s *Service) ListVolumes(
	ctx context.Context,
	limit, offset int32,
) ([]VolumeRow, error) {
	return s.repos.ComputeStorageVolumes.List(ctx, limit, offset)
}

// CountVolumes returns the number of non-deleted storage volumes in the tenant.
func (s *Service) CountVolumes(ctx context.Context) (int64, error) {
	return s.repos.ComputeStorageVolumes.Count(ctx)
}

// DeleteVolume soft-deletes the row + deletes the Incus volume.
func (s *Service) DeleteVolume(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	id uuid.UUID,
) error {
	row, err := s.repos.ComputeStorageVolumes.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrVolumeNotFound
		}
		return fmt.Errorf("compute: get volume: %w", err)
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.storage_volume.delete",
		ResourceType: ResourceVolume,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})
	if s.provider != nil {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.DeleteStorageVolume(ctx, row.PoolName, project, "custom", row.Name); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			return fmt.Errorf("compute: incus delete volume: %w", err)
		}
	}
	if err := s.repos.ComputeStorageVolumes.SoftDelete(ctx, id); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return fmt.Errorf("compute: soft delete volume: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}
