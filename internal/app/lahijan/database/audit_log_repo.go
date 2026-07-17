// Package database: audit_log_repo.go wraps the sqlc-generated audit_log
// queries. The audit_log table is append-only (enforced by a trigger in
// migration 0005), so this repo intentionally exposes only INSERT and SELECT.
// tenant_id is nullable on audit_log because some events are system-level; the
// Create method accepts an optional tenant id and the tenant-scoped List/Count
// methods derive the tenant from the request context like other tenant-scoped
// repos. Audit events are emitted from the service layer (WS-08), never from
// repositories further down.
package database

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// AuditLogRepository is the persistence boundary for the audit_log table.
type AuditLogRepository struct {
	q *gen.Queries
}

// NewAuditLogRepository wraps the given sqlc queries.
func NewAuditLogRepository(q *gen.Queries) *AuditLogRepository {
	return &AuditLogRepository{q: q}
}

// CreateAuditLogParams carries the fields a service supplies when emitting an
// audit event. TenantID, ActorUserID, ResourceID, Status, RequestID, and
// Metadata are all optional; Action and ResourceType are required.
type CreateAuditLogParams struct {
	TenantID     *uuid.UUID
	ActorUserID  *uuid.UUID
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Status       *string
	RequestID    *string
	Metadata     json.RawMessage
}

// Create appends an audit row. The trigger on audit_log makes this the only
// mutating operation permitted.
func (r *AuditLogRepository) Create(ctx context.Context, arg CreateAuditLogParams) (gen.AuditLog, error) {
	meta := arg.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage("{}")
	}
	return r.q.CreateAuditLog(ctx, gen.CreateAuditLogParams{
		TenantID:     arg.TenantID,
		ActorUserID:  arg.ActorUserID,
		ActorType:    arg.ActorType,
		Action:       arg.Action,
		ResourceType: arg.ResourceType,
		ResourceID:   arg.ResourceID,
		Status:       strOr(arg.Status, "success"),
		RequestID:    arg.RequestID,
		Metadata:     meta,
	})
}

// Get returns a single audit row by id (any tenant — admin read path).
func (r *AuditLogRepository) Get(ctx context.Context, id uuid.UUID) (gen.AuditLog, error) {
	return r.q.GetAuditLog(ctx, id)
}

// ListForTenant returns audit rows for the tenant in ctx, newest first.
func (r *AuditLogRepository) ListForTenant(
	ctx context.Context,
	limit, offset int32,
) ([]gen.AuditLog, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAuditLogForTenant(ctx, gen.ListAuditLogForTenantParams{
		TenantID: &tenantID,
		Limit:    limit,
		Offset:   offset,
	})
}

// CountForTenant returns the number of audit rows for the tenant in ctx.
func (r *AuditLogRepository) CountForTenant(ctx context.Context) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountAuditLogForTenant(ctx, &tenantID)
}

// ListGlobal returns audit rows across all tenants. This is the admin/system
// read path and is intentionally NOT tenant-scoped; gate the caller with
// RequirePerm("audit.read_global") in WS-08.
func (r *AuditLogRepository) ListGlobal(ctx context.Context, limit, offset int32) ([]gen.AuditLog, error) {
	return r.q.ListAuditLogGlobal(ctx, gen.ListAuditLogGlobalParams{Limit: limit, Offset: offset})
}
