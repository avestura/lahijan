// Package database: storage_lifecycle_rules_repo.go wraps the
// sqlc-generated storage_lifecycle_rules queries (WS-29, ADR-0036).
// Every query is tenant-scoped via WithTenant at the repository seam —
// callers cannot pass a tenant id directly. A cross-tenant bucket_id
// surfaces as ErrNoRows, never as the row itself.
//
// The lifecycle evaluator River worker (storage.lifecycle.evaluate)
// uses ListEnabledForTenant to scan due rules every tick; the storage
// service's HTTP-facing methods use Create / Get / List / Update /
// Delete for the per-rule privileged operations.
package database

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// StorageLifecycleRulesRepository is the persistence boundary for the
// storage_lifecycle_rules table.
type StorageLifecycleRulesRepository struct {
	q *gen.Queries
}

// NewStorageLifecycleRulesRepository wraps the given sqlc queries.
func NewStorageLifecycleRulesRepository(q *gen.Queries) *StorageLifecycleRulesRepository {
	return &StorageLifecycleRulesRepository{q: q}
}

// CreateStorageLifecycleRuleParams carries the user-controlled fields of
// a new storage_lifecycle_rules row. TenantID is taken from the request
// context, NOT from the caller.
type CreateStorageLifecycleRuleParams struct {
	BucketID     uuid.UUID
	RuleID       string
	Status       string
	Action       string
	Days         *int32
	DateAt       *time.Time
	StorageClass *string
	Prefix       *string
}

// Create inserts a new storage_lifecycle_rules row scoped to the tenant
// in ctx. Returns the new row so the caller can return the id straight
// to the API.
func (r *StorageLifecycleRulesRepository) Create(
	ctx context.Context,
	arg CreateStorageLifecycleRuleParams,
) (gen.StorageLifecycleRule, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageLifecycleRule{}, err
	}
	return r.q.CreateStorageLifecycleRule(ctx, gen.CreateStorageLifecycleRuleParams{
		TenantID:     tenantID,
		BucketID:     arg.BucketID,
		RuleID:       arg.RuleID,
		Status:       arg.Status,
		Action:       arg.Action,
		Days:         arg.Days,
		DateAt:       arg.DateAt,
		StorageClass: arg.StorageClass,
		Prefix:       arg.Prefix,
	})
}

// Get returns the rule with the given (bucket_id, rule_id) within the
// tenant in ctx. Used by the service layer on every privileged call
// (UpdateRule / DeleteRule) so a cross-tenant bucket id surfaces as
// ErrNoRows at the repository seam.
func (r *StorageLifecycleRulesRepository) Get(
	ctx context.Context,
	bucketID uuid.UUID,
	ruleID string,
) (gen.StorageLifecycleRule, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageLifecycleRule{}, err
	}
	return r.q.GetStorageLifecycleRule(ctx, gen.GetStorageLifecycleRuleParams{
		TenantID: tenantID, BucketID: bucketID, RuleID: ruleID,
	})
}

// GetByID returns the rule with the given id within the tenant in ctx.
// Used by the service layer's UpdateRule / DeleteRule / EnableRule /
// DisableRule paths that take the rule's primary key from the URL.
func (r *StorageLifecycleRulesRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (gen.StorageLifecycleRule, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.StorageLifecycleRule{}, err
	}
	return r.q.GetStorageLifecycleRuleByID(ctx, gen.GetStorageLifecycleRuleByIDParams{
		TenantID: tenantID, ID: id,
	})
}

// List returns a page of rules attached to the bucket within the tenant
// in ctx. Ordered by rule_id for deterministic UI rendering.
func (r *StorageLifecycleRulesRepository) List(
	ctx context.Context,
	bucketID uuid.UUID,
	limit, offset int32,
) ([]gen.StorageLifecycleRule, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListStorageLifecycleRules(ctx, gen.ListStorageLifecycleRulesParams{
		TenantID: tenantID, BucketID: bucketID, Limit: limit, Offset: offset,
	})
}

// Count returns the number of rules attached to the bucket within the
// tenant in ctx.
func (r *StorageLifecycleRulesRepository) Count(
	ctx context.Context,
	bucketID uuid.UUID,
) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountStorageLifecycleRules(ctx, gen.CountStorageLifecycleRulesParams{
		TenantID: tenantID, BucketID: bucketID,
	})
}

// ListEnabledForTenant returns every ENABLED rule across the tenant in
// ctx. Used by the lifecycle evaluator River worker (see
// storage/lifecycle_worker.go) on every tick so the worker can act on
// due rules in a single query per tenant. Ordered by (bucket_id,
// rule_id) for deterministic processing.
func (r *StorageLifecycleRulesRepository) ListEnabledForTenant(
	ctx context.Context,
) ([]gen.StorageLifecycleRule, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListEnabledStorageLifecycleRules(ctx, tenantID)
}

// UpdateStorageLifecycleRuleParams carries the mutable fields the
// service layer can rewrite. RuleID + BucketID are immutable; a rule
// cannot hop buckets or change its natural key.
type UpdateStorageLifecycleRuleParams struct {
	ID           uuid.UUID
	Status       string
	Days         *int32
	DateAt       *time.Time
	StorageClass *string
	Prefix       *string
}

// Update replaces the mutable fields of a rule.
func (r *StorageLifecycleRulesRepository) Update(
	ctx context.Context,
	arg UpdateStorageLifecycleRuleParams,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.UpdateStorageLifecycleRule(ctx, gen.UpdateStorageLifecycleRuleParams{
		TenantID:     tenantID,
		ID:           arg.ID,
		Status:       arg.Status,
		Days:         arg.Days,
		DateAt:       arg.DateAt,
		StorageClass: arg.StorageClass,
		Prefix:       arg.Prefix,
	})
}

// SetStatus flips just the status field. Used by the service layer's
// enable / disable shortcuts so the audit row metadata records only
// the changed field.
func (r *StorageLifecycleRulesRepository) SetStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.SetStorageLifecycleRuleStatus(ctx, gen.SetStorageLifecycleRuleStatusParams{
		TenantID: tenantID, ID: id, Status: status,
	})
}

// Delete removes the rule. Idempotent — a missing rule surfaces as
// ErrNoRows so the caller can decide whether to treat that as success
// (DELETE is idempotent at the HTTP layer) or as a 404.
func (r *StorageLifecycleRulesRepository) Delete(
	ctx context.Context,
	id uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteStorageLifecycleRule(ctx, gen.DeleteStorageLifecycleRuleParams{
		TenantID: tenantID, ID: id,
	})
}

// DeleteAllForBucket bulk-removes every rule attached to the bucket
// within the tenant in ctx. Used by the storage service when a user
// replaces the whole policy via PUT /lifecycle (the new policy is then
// written rule by rule in the same transaction).
func (r *StorageLifecycleRulesRepository) DeleteAllForBucket(
	ctx context.Context,
	bucketID uuid.UUID,
) error {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return err
	}
	return r.q.DeleteAllStorageLifecycleRulesForBucket(ctx, gen.DeleteAllStorageLifecycleRulesForBucketParams{
		TenantID: tenantID, BucketID: bucketID,
	})
}
