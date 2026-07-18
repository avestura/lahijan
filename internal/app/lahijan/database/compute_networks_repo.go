// Package database: compute_networks_repo.go wraps the sqlc-generated
// compute_networks queries (WS-14). Every query is tenant-scoped via
// WithTenant at the repository seam.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// NetworkType values mirror the Incus network "type" field.
const (
	NetworkTypeBridge   = "bridge"
	NetworkTypeMacvlan  = "macvlan"
	NetworkTypePhysical = "physical"
)

// ComputeNetworksRepository is the persistence boundary for compute_networks.
type ComputeNetworksRepository struct {
	q *gen.Queries
}

// NewComputeNetworksRepository wraps the given sqlc queries.
func NewComputeNetworksRepository(q *gen.Queries) *ComputeNetworksRepository {
	return &ComputeNetworksRepository{q: q}
}

// CreateComputeNetworkParams carries the user-controlled fields of a new row.
type CreateComputeNetworkParams struct {
	Name         string
	Description  string
	Type         string
	Config       json.RawMessage
	ACLNames     []string
	ForwardNames []string
}

// Create inserts a new compute_networks row scoped to the tenant in ctx.
func (r *ComputeNetworksRepository) Create(
	ctx context.Context,
	arg CreateComputeNetworkParams,
) (gen.ComputeNetwork, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeNetwork{}, err
	}
	netType := arg.Type
	if netType == "" {
		netType = NetworkTypeBridge
	}
	cfg := arg.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	acls := arg.ACLNames
	if acls == nil {
		acls = []string{}
	}
	fwds := arg.ForwardNames
	if fwds == nil {
		fwds = []string{}
	}
	return r.q.CreateComputeNetwork(ctx, gen.CreateComputeNetworkParams{
		TenantID: tenantID, Name: arg.Name, Description: arg.Description,
		Type: netType, ConfigJson: cfg, AclNames: acls, ForwardNames: fwds,
	})
}

// Get returns the compute_networks row with id within the tenant in ctx.
func (r *ComputeNetworksRepository) Get(ctx context.Context, id uuid.UUID) (gen.ComputeNetwork, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeNetwork{}, err
	}
	return r.q.GetComputeNetworkByID(ctx, gen.GetComputeNetworkByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// GetByName returns the compute_networks row matching name within the tenant.
func (r *ComputeNetworksRepository) GetByName(
	ctx context.Context,
	name string,
) (gen.ComputeNetwork, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.ComputeNetwork{}, err
	}
	return r.q.GetComputeNetworkByName(ctx, gen.GetComputeNetworkByNameParams{
		TenantID: tenantID, Name: name,
	})
}

// List returns a page of compute_networks within the tenant in ctx.
func (r *ComputeNetworksRepository) List(
	ctx context.Context,
	limit, offset int32,
) ([]gen.ComputeNetwork, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListComputeNetworks(ctx, gen.ListComputeNetworksParams{
		TenantID: tenantID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of compute_networks within the tenant in ctx.
func (r *ComputeNetworksRepository) Count(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountComputeNetworks(ctx, tenantID)
}

// Update replaces the description + config + ACL/forward name lists.
func (r *ComputeNetworksRepository) Update(
	ctx context.Context,
	id uuid.UUID,
	description string,
	config json.RawMessage,
	aclNames, forwardNames []string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if aclNames == nil {
		aclNames = []string{}
	}
	if forwardNames == nil {
		forwardNames = []string{}
	}
	return r.q.UpdateComputeNetwork(ctx, gen.UpdateComputeNetworkParams{
		TenantID: tenantID, ID: id, Description: description,
		ConfigJson: config, AclNames: aclNames, ForwardNames: forwardNames,
	})
}

// SoftDelete marks the network as deleted.
func (r *ComputeNetworksRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SoftDeleteComputeNetwork(ctx, gen.SoftDeleteComputeNetworkParams{
		TenantID: tenantID, ID: id,
	})
}
