// Package compute: networks.go implements the per-tenant network surface.
// Networks are project-scoped Incus networks instances attach to via a nic
// device. Every privileged action emits an audit event; the Incus side is
// updated via the provider driver.
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

// NetworkRow is the API-facing shape returned by Get/List.
type NetworkRow = database.ComputeNetwork

// CreateNetworkParams carries the user-controlled fields of a network-create.
type CreateNetworkParams struct {
	Name        string
	Description string
	Type        string
	Config      map[string]string
}

// CreateNetwork persists the row + creates the Incus network within the
// tenant's project.
func (s *Service) CreateNetwork(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params CreateNetworkParams,
) (NetworkRow, error) {
	if params.Name == "" {
		return NetworkRow{}, ErrInvalidName
	}
	netType := params.Type
	if netType == "" {
		netType = database.NetworkTypeBridge
	}
	configJSON, _ := json.Marshal(params.Config)
	row, err := s.repos.ComputeNetworks.Create(ctx, database.CreateComputeNetworkParams{
		Name:        params.Name,
		Description: params.Description,
		Type:        netType,
		ConfigJson:  configJSON,
	})
	if err != nil {
		if database.IsUniqueViolation(err) {
			return NetworkRow{}, fmt.Errorf("%w: name=%s", ErrNameTaken, params.Name)
		}
		return NetworkRow{}, fmt.Errorf("compute: create network row: %w", err)
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.network.create",
		ResourceType: ResourceNetwork,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata:     map[string]any{"name": params.Name, "type": netType},
	})
	if s.provider != nil {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.EnsureProject(ctx, tenantID); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			_ = s.repos.ComputeNetworks.SoftDelete(ctx, row.ID)
			return NetworkRow{}, fmt.Errorf("compute: ensure project: %w", err)
		}
		if err := s.provider.CreateNetwork(ctx, project, incus.NetworksPost{
			Name:        params.Name,
			Description: params.Description,
			Type:        netType,
			Config:      params.Config,
			Project:     project,
		}); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			_ = s.repos.ComputeNetworks.SoftDelete(ctx, row.ID)
			return NetworkRow{}, fmt.Errorf("compute: incus create network: %w", err)
		}
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return row, nil
}

// GetNetwork returns the network with id within the tenant.
func (s *Service) GetNetwork(ctx context.Context, id uuid.UUID) (NetworkRow, error) {
	row, err := s.repos.ComputeNetworks.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return NetworkRow{}, ErrNetworkNotFound
		}
		return NetworkRow{}, fmt.Errorf("compute: get network: %w", err)
	}
	return row, nil
}

// ListNetworks returns a paginated list of the tenant's networks.
func (s *Service) ListNetworks(
	ctx context.Context,
	limit, offset int32,
) ([]NetworkRow, error) {
	return s.repos.ComputeNetworks.List(ctx, limit, offset)
}

// DeleteNetwork soft-deletes the row + deletes the Incus network.
func (s *Service) DeleteNetwork(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	id uuid.UUID,
) error {
	row, err := s.repos.ComputeNetworks.Get(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return ErrNetworkNotFound
		}
		return fmt.Errorf("compute: get network: %w", err)
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.network.delete",
		ResourceType: ResourceNetwork,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
	})
	if s.provider != nil {
		project := s.provider.ProjectName(tenantID)
		if err := s.provider.DeleteNetwork(ctx, project, row.Name); err != nil {
			_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
			return fmt.Errorf("compute: incus delete network: %w", err)
		}
	}
	if err := s.repos.ComputeNetworks.SoftDelete(ctx, id); err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{"error": err.Error()}})
		return fmt.Errorf("compute: soft delete network: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: nil})
	return nil
}
