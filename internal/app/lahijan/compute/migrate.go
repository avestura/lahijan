// Package compute: migrate.go implements the WS-26 cluster admin surface
// on the compute Service. Two operations:
//
//  1. ListClusterMembers wraps incus.Provider.ListClusterMembers and
//     returns the raw members list. The compute service is a thin
//     pass-through here; the audit + RBAC gates live at the api layer
//     (RequirePerm + the audit gate).
//  2. MigrateInstance orchestrates a live-migrate: audit emit ->
//     placement.MigrateInstance -> reconcile the cluster_member column
//     from the daemon's post-migrate Location -> audit outcome.
//
// The cluster admin surface is admin-only by default (the WS-26 doc
// grants list/migrate to tenant.admin and member-list to tenant.viewer
// so the UI can render the cluster status panel without elevating).
package compute

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// ClusterMember is the API-facing cluster member shape. Mirrors
// incus.ClusterMember so the compute service can stay non-leaky
// (pillar 1: end users see "compute nodes", not "Incus members").
type ClusterMember struct {
	ServerName    string
	URL           string
	Database      bool
	Status        string
	Message       string
	Roles         []string
	Architecture  string
	FailureDomain string
	Description   string
	Config        map[string]string
}

// clusterLister is the narrow seam the service needs for listing
// members. *incus.Provider satisfies it; tests stub a fake.
type clusterLister interface {
	ListClusterMembers(ctx context.Context) ([]incus.ClusterMember, error)
	GetClusterMember(ctx context.Context, name string) (*incus.ClusterMember, error)
}

// clusterMigrator is the narrow seam the service needs for the
// instance-migrate path. The compute provider (full incusProvider)
// satisfies it; the placement driver's MigrateInstance is the
// forwarding path.
//
// The seam is split from clusterLister so a test that exercises only
// the migrate path can stub just the methods it needs. Kept for
// future use (today the migrate path uses the placement driver + the
// provider's GetInstance directly); the lint suppression documents
// why the type is retained.
//
//nolint:unused // future-proofing seam; see comment above
type clusterMigrator interface {
	GetInstance(ctx context.Context, project, name string) (*incus.Instance, error)
}

// ListClusterMembers returns the cluster's members. Returns an empty
// slice on a non-clustered daemon (the driver's default seed carries
// one synthetic "local" member so the UI still renders).
//
// The audit row is emitted by the api handler's audit gate
// (compute.cluster.member.list); this method does not emit again so
// the audit row count matches the user action.
func (s *Service) ListClusterMembers(ctx context.Context) ([]ClusterMember, error) {
	lister, ok := s.provider.(clusterLister)
	if !ok {
		return nil, ErrProviderDisabled
	}
	members, err := lister.ListClusterMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("compute: list cluster members: %w", err)
	}
	out := make([]ClusterMember, 0, len(members))
	for _, m := range members {
		out = append(out, ClusterMember{
			ServerName:    m.ServerName,
			URL:           m.URL,
			Database:      m.Database,
			Status:        m.Status,
			Message:       m.Message,
			Roles:         m.Roles,
			Architecture:  m.Architecture,
			FailureDomain: m.FailureDomain,
			Description:   m.Description,
			Config:        m.Config,
		})
	}
	return out, nil
}

// GetClusterMember returns a single cluster member by name. Used by
// the admin detail view; mirrors ListClusterMembers' shape.
func (s *Service) GetClusterMember(ctx context.Context, name string) (ClusterMember, error) {
	lister, ok := s.provider.(clusterLister)
	if !ok {
		return ClusterMember{}, ErrProviderDisabled
	}
	m, err := lister.GetClusterMember(ctx, name)
	if err != nil {
		return ClusterMember{}, fmt.Errorf("compute: get cluster member: %w", err)
	}
	return ClusterMember{
		ServerName:    m.ServerName,
		URL:           m.URL,
		Database:      m.Database,
		Status:        m.Status,
		Message:       m.Message,
		Roles:         m.Roles,
		Architecture:  m.Architecture,
		FailureDomain: m.FailureDomain,
		Description:   m.Description,
		Config:        m.Config,
	}, nil
}

// MigrateInstanceParams is the user-facing shape of a migrate call.
// TenantID + InstanceID identify the instance; TargetMember is the
// destination; Live toggles live migration.
type MigrateInstanceParams struct {
	InstanceID   uuid.UUID
	TargetMember string
	Live         bool
}

// MigrateInstance moves an existing instance to a different cluster
// member. The audit pattern (pending -> success | failure) is the
// same as the lifecycle actions; the placement driver owns the
// Incus-side call (the cluster driver forwards to
// provider.MigrateInstance; the local driver returns
// ErrMigrationNotSupported).
//
// On success the row's cluster_member column is reconciled from the
// daemon's post-migrate Location so the UI reflects the new placement
// immediately. Returns the updated instance row.
func (s *Service) MigrateInstance(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params MigrateInstanceParams,
) (database.ComputeInstance, error) {
	if s.provider == nil {
		return database.ComputeInstance{}, ErrProviderDisabled
	}
	if params.TargetMember == "" {
		return database.ComputeInstance{}, errors.New("compute: migrate requires target member")
	}
	row, err := s.repos.ComputeInstances.Get(ctx, params.InstanceID)
	if err != nil {
		return database.ComputeInstance{}, fmt.Errorf("compute: get instance for migrate: %w", err)
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceMigrated,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"target_member": params.TargetMember,
			"live":          params.Live,
		},
	})

	op, err := s.placement.MigrateInstance(ctx, MigrateParams{
		TenantID:     tenantID,
		Project:      row.ProjectName,
		Instance:     row.Name,
		InstanceID:   row.ID,
		TargetMember: params.TargetMember,
		Live:         params.Live,
	})
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return database.ComputeInstance{}, fmt.Errorf("compute: migrate: %w", err)
	}
	_ = op // the operation's terminal status is checked by the driver

	// Reconcile the cluster_member column from the daemon's view. A
	// live-migrate may take seconds; the daemon flips Location as
	// soon as the migrate completes.
	if inst, errGetInstance := s.provider.GetInstance(ctx, row.ProjectName, row.Name); errGetInstance == nil && inst.Location != "" {
		loc := inst.Location
		_ = s.repos.ComputeInstances.SetClusterMember(ctx, row.ID, &loc)
		row.ClusterMember = &loc
	} else {
		// Fall back to the requested target so the UI reflects intent
		// even when the daemon does not echo back Location
		// immediately.
		t := params.TargetMember
		_ = s.repos.ComputeInstances.SetClusterMember(ctx, row.ID, &t)
		row.ClusterMember = &t
	}

	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"cluster_member": *row.ClusterMember,
	}})
	return row, nil
}

// opIDOrEmpty extracts the operation id from a create/migrate
// operation. Returns "" when op is nil (e.g. the local driver
// short-circuit).
func opIDOrEmpty(op *incus.Operation) string {
	if op == nil {
		return ""
	}
	return op.ID
}

// EvacuateClusterMemberParams is the user-facing shape of an evacuate
// call. Mode is "migrate" (default), "live-migrate", or "stop" — the
// daemon picks the per-member default when empty.
type EvacuateClusterMemberParams struct {
	MemberName string
	Mode       string
}

// EvacuateClusterMember triggers the Incus evacuate workflow on the
// named member. The member's existing instances are live-migrated (or
// stopped, per Mode) to other members; the member is then marked
// "Evacuated" and removed from future scheduling. Used by the
// "Drain member" admin UI action.
//
// Returns the async operation id so the UI can poll status.
func (s *Service) EvacuateClusterMember(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params EvacuateClusterMemberParams,
) (string, error) {
	evacuator, ok := s.provider.(clusterEvacuator)
	if !ok {
		return "", ErrProviderDisabled
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.cluster.member.evacuate",
		ResourceType: "compute_cluster_member",
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"member": params.MemberName,
			"mode":   params.Mode,
		},
	})
	op, err := evacuator.SetClusterMemberState(ctx, params.MemberName, incus.ClusterMemberActionEvacuate, params.Mode)
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return "", fmt.Errorf("compute: evacuate member: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"operation_id": opIDOrEmpty(op),
	}})
	return opIDOrEmpty(op), nil
}

// RestoreClusterMember reverses an earlier evacuate. The member is
// marked "Online" and accepts new placements again. Used by the
// "Return member to service" admin UI action.
func (s *Service) RestoreClusterMember(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	memberName string,
) (string, error) {
	evacuator, ok := s.provider.(clusterEvacuator)
	if !ok {
		return "", ErrProviderDisabled
	}
	auditID, _ := s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       "compute.cluster.member.restore",
		ResourceType: "compute_cluster_member",
		Status:       audit.StatusPending,
		Metadata: map[string]any{
			"member": memberName,
		},
	})
	op, err := evacuator.RestoreClusterMember(ctx, memberName)
	if err != nil {
		_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusFailure, Details: map[string]any{
			"error": err.Error(),
		}})
		return "", fmt.Errorf("compute: restore member: %w", err)
	}
	_ = s.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: audit.StatusSuccess, Details: map[string]any{
		"operation_id": opIDOrEmpty(op),
	}})
	return opIDOrEmpty(op), nil
}

// clusterEvacuator is the narrow seam the evacuate/restore paths need
// from *incus.Provider. Split from clusterLister so a test that does
// not exercise evacuate can stub a smaller interface.
type clusterEvacuator interface {
	SetClusterMemberState(ctx context.Context, name, action, mode string) (*incus.Operation, error)
	RestoreClusterMember(ctx context.Context, name string) (*incus.Operation, error)
}
