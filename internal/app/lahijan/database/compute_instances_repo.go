// Package database: compute_instances_repo.go wraps the sqlc-generated
// compute_instances queries (WS-14). Every query is tenant-scoped via
// WithTenant at the repository seam — callers cannot pass a tenant id
// directly. The row mirrors Incus state but the daemon is the source of
// truth; the cached status + status_code fields are reconciled on read.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// InstanceType values mirror the Incus instance "type" field. Empty string
// is treated as "container" by the daemon.
const (
	InstanceTypeContainer    = "container"
	InstanceTypeVirtualMachine = "virtual-machine"
)

// InstanceStatus values mirror the Incus instance "status" field. The
// "deleted" pseudo-status is set by the soft-delete path so the API can
// distinguish a row that was hard-deleted from one that was soft-deleted.
const (
	InstanceStatusStopped  = "Stopped"
	InstanceStatusRunning  = "Running"
	InstanceStatusFrozen   = "Frozen"
	InstanceStatusStarting = "Starting"
	InstanceStatusStopping = "Stopping"
	InstanceStatusDeleted  = "deleted"
)

// StatusCode values mirror the Incus numeric status codes used by the
// daemon (Incus types.StatusCode). Kept here so the cache column matches
// the daemon's own values; see statusFromIncus helper in the service layer.
const (
	StatusCodeStopped    = 102
	StatusCodeRunning    = 103
	StatusCodeFrozen     = 110
	StatusCodeStarting   = 111
	StatusCodeStopping   = 112
	StatusCodeDeleted    = 0
)

// ComputeInstancesRepository is the persistence boundary for the
// compute_instances table.
type ComputeInstancesRepository struct {
	q *gen.Queries
}

// NewComputeInstancesRepository wraps the given sqlc queries.
func NewComputeInstancesRepository(q *gen.Queries) *ComputeInstancesRepository {
	return &ComputeInstancesRepository{q: q}
}

// CreateComputeInstanceParams carries the user-controlled fields of a new
// compute_instances row. TenantID is taken from the request context, NOT
// from the caller.
type CreateComputeInstanceParams struct {
	ProjectName      string
	Name             string
	Type             string
	Status           string
	StatusCode       int32
	ImageAlias       string
	ImageFingerprint string
	Profiles         []string
	ConfigJson       json.RawMessage
	Description      string
}

// Create inserts a new compute_instances row scoped to the tenant in ctx.
func (r *ComputeInstancesRepository) Create(
	ctx context.Context,
	arg CreateComputeInstanceParams,
) (gen.ComputeInstance, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeInstance{}, err
	}
	instType := arg.Type
	if instType == "" {
		instType = InstanceTypeContainer
	}
	status := arg.Status
	if status == "" {
		status = InstanceStatusStopped
	}
	statusCode := arg.StatusCode
	if statusCode == 0 {
		statusCode = int32(StatusCodeStopped)
	}
	profiles := arg.Profiles
	if profiles == nil {
		profiles = []string{}
	}
	cfg := arg.ConfigJson
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	return r.q.CreateComputeInstance(ctx, gen.CreateComputeInstanceParams{
		TenantID:         tenantID,
		ProjectName:      arg.ProjectName,
		Name:             arg.Name,
		Type:             instType,
		Status:           status,
		StatusCode:       statusCode,
		ImageAlias:       arg.ImageAlias,
		ImageFingerprint: arg.ImageFingerprint,
		Profiles:         profiles,
		ConfigJson:       cfg,
		Description:      arg.Description,
	})
}

// Get returns the compute_instances row with id within the tenant in ctx.
func (r *ComputeInstancesRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeInstance, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeInstance{}, err
	}
	return r.q.GetComputeInstanceByID(ctx, gen.GetComputeInstanceByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the compute_instances row matching name within the
// tenant in ctx.
func (r *ComputeInstancesRepository) GetByName(ctx context.Context, name string) (gen.ComputeInstance, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeInstance{}, err
	}
	return r.q.GetComputeInstanceByName(ctx, gen.GetComputeInstanceByNameParams{
		TenantID: tenantID, Name: name,
	})
}

// List returns a page of compute_instances within the tenant in ctx.
func (r *ComputeInstancesRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeInstance, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeInstances(ctx, gen.ListComputeInstancesParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of non-deleted compute_instances within the
// tenant in ctx.
func (r *ComputeInstancesRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeInstances(ctx, tenantID)
}

// CountByStatus returns the number of non-deleted compute_instances within
// the tenant in ctx with the given status. The quota checker uses it to
// count running instances.
func (r *ComputeInstancesRepository) CountByStatus(
	ctx context.Context,
	status string,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeInstancesByStatus(ctx, gen.CountComputeInstancesByStatusParams{
		TenantID: tenantID, Status: status,
	})
}

// InstanceConfigForQuota is the (id, config_json) pair the quota checker
// walks to aggregate CPU/RAM/disk usage. Parsing happens in the compute
// service layer (limits.cpu can be pinned-cpu lists, limits.memory / root
// size accept unit suffixes — Go is the cleaner place to do the math).
type InstanceConfigForQuota struct {
	ID         uuid.UUID
	ConfigJson json.RawMessage
}

// ListConfigsForQuota returns the (id, config_json) pair for every
// non-deleted instance within the tenant in ctx.
func (r *ComputeInstancesRepository) ListConfigsForQuota(
	ctx context.Context,
) ([]InstanceConfigForQuota, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.q.ListComputeInstanceConfigsForQuota(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]InstanceConfigForQuota, 0, len(rows))
	for _, row := range rows {
		out = append(out, InstanceConfigForQuota{ID: row.ID, ConfigJson: row.ConfigJson})
	}
	return out, nil
}

// SetStatus caches the last-known Incus status for the instance.
func (r *ComputeInstancesRepository) SetStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
	statusCode int32,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetComputeInstanceStatus(ctx, gen.SetComputeInstanceStatusParams{
		TenantID: tenantID, ID: id, Status: status, StatusCode: statusCode,
	})
}

// SetImageFingerprint records the resolved fingerprint for the instance.
func (r *ComputeInstancesRepository) SetImageFingerprint(
	ctx context.Context,
	id uuid.UUID,
	fingerprint string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetComputeInstanceImageFingerprint(ctx, gen.SetComputeInstanceImageFingerprintParams{
		TenantID: tenantID, ID: id, ImageFingerprint: fingerprint,
	})
}

// UpdateConfig replaces the cached config snapshot for the instance.
func (r *ComputeInstancesRepository) UpdateConfig(
	ctx context.Context,
	id uuid.UUID,
	config json.RawMessage,
	profiles []string,
	description string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if profiles == nil {
		profiles = []string{}
	}
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	return r.q.UpdateComputeInstanceConfig(ctx, gen.UpdateComputeInstanceConfigParams{
		TenantID: tenantID, ID: id, ConfigJson: config,
		Profiles: profiles, Description: description,
	})
}

// SoftDelete marks the instance as deleted (deleted_at = now). The row is
// retained for historical audit + billing joins. The Incus instance itself
// should be deleted via the provider driver before this runs.
func (r *ComputeInstancesRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeInstance(ctx, gen.SoftDeleteComputeInstanceParams{
		TenantID: tenantID, ID: id,
	})
}
