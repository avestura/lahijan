// Package database: audit_log_repo.go wraps the sqlc-generated audit_log and
// audit_log_outcomes queries. Both tables are append-only (enforced by
// triggers in migrations 0005 and 0010), so this repo intentionally exposes
// only INSERT and SELECT on either. tenant_id is nullable on audit_log because
// some events are system-level; the Create method accepts an optional tenant
// id and the tenant-scoped List/Count methods derive the tenant from the
// request context like other tenant-scoped repos. Audit events are emitted
// from the service layer (WS-08), never from repositories further down.
package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database/gen"
)

// AuditLogRepository is the persistence boundary for the audit_log and
// audit_log_outcomes tables.
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

// MarkOutcome appends a row to audit_log_outcomes for the given audit id,
// recording the post-side-effect status. The audit_log_outcomes table is
// append-only (migration 0010), so this is the only mutating operation
// permitted on the outcomes trail.
func (r *AuditLogRepository) MarkOutcome(
	ctx context.Context,
	auditID uuid.UUID,
	status string,
	details json.RawMessage,
) error {
	if len(details) == 0 {
		details = json.RawMessage("{}")
	}
	_, err := r.q.CreateAuditLogOutcome(ctx, gen.CreateAuditLogOutcomeParams{
		AuditID: auditID,
		Status:  status,
		Details: details,
	})
	return err
}

// ListOutcomes returns every outcome row for an audit event, newest first. The
// caller can pick the first element as the "current" status.
func (r *AuditLogRepository) ListOutcomes(ctx context.Context, auditID uuid.UUID) ([]gen.AuditLogOutcome, error) {
	return r.q.ListAuditLogOutcomes(ctx, auditID)
}

// Get returns a single audit row by id (any tenant — admin read path).
func (r *AuditLogRepository) Get(ctx context.Context, id uuid.UUID) (gen.AuditLog, error) {
	return r.q.GetAuditLog(ctx, id)
}

// GetForTenant returns a single audit row by id, scoped to the tenant in ctx.
// A row whose tenant_id is NULL (system-level event) is also returned, so an
// admin viewing their tenant's audit can still see system events that touch
// their users (e.g. logins).
func (r *AuditLogRepository) GetForTenant(ctx context.Context, id uuid.UUID) (gen.AuditLog, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return gen.AuditLog{}, err
	}
	return r.q.GetAuditLogForTenant(ctx, gen.GetAuditLogForTenantParams{
		TenantID: &tenantID,
		ID:       id,
	})
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

// AuditLogFilter carries the optional filters for the GET /audit endpoint.
// Each field is a pointer; a nil pointer means "do not filter on this column".
// TenantID here is only honored by the global (admin) variant; the
// tenant-scoped variant derives the tenant from ctx and ignores this field.
type AuditLogFilter struct {
	ActorUserID  *uuid.UUID
	Action       *string
	ResourceType *string
	ResourceID   *uuid.UUID // honored only by the tenant-scoped variant
	Status       *string
	ActorType    *string
	TenantID     *uuid.UUID // honored only by the global (admin) variant
	FromTS       *time.Time
	ToTS         *time.Time
}

// ListForTenantFiltered returns filtered + paginated audit rows for the tenant
// in ctx. Filters are NULL-able; pagination is via limit/offset.
func (r *AuditLogRepository) ListForTenantFiltered(
	ctx context.Context,
	f AuditLogFilter,
	limit, offset int32,
) ([]gen.AuditLog, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return r.q.ListAuditLogForTenantFiltered(ctx, gen.ListAuditLogForTenantFilteredParams{
		TenantID:     &tenantID,
		ActorUserID:  f.ActorUserID,
		Action:       f.Action,
		ResourceType: f.ResourceType,
		ResourceID:   f.ResourceID,
		Status:       f.Status,
		ActorType:    f.ActorType,
		FromTs:       f.FromTS,
		ToTs:         f.ToTS,
		Limit:        limit,
		Offset:       offset,
	})
}

// CountForTenantFiltered returns the count matching the same filters used by
// ListForTenantFiltered; needed for pagination metadata in the API response.
func (r *AuditLogRepository) CountForTenantFiltered(ctx context.Context, f AuditLogFilter) (int64, error) {
	tenantID, err := TenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	return r.q.CountAuditLogForTenantFiltered(ctx, gen.CountAuditLogForTenantFilteredParams{
		TenantID:     &tenantID,
		ActorUserID:  f.ActorUserID,
		Action:       f.Action,
		ResourceType: f.ResourceType,
		ResourceID:   f.ResourceID,
		Status:       f.Status,
		ActorType:    f.ActorType,
		FromTs:       f.FromTS,
		ToTs:         f.ToTS,
	})
}

// ListGlobalFiltered returns filtered + paginated audit rows across all
// tenants. Admin-only; gate the caller with RequirePerm("audit.read_global").
func (r *AuditLogRepository) ListGlobalFiltered(
	ctx context.Context,
	f AuditLogFilter,
	limit, offset int32,
) ([]gen.AuditLog, error) {
	return r.q.ListAuditLogGlobalFiltered(ctx, gen.ListAuditLogGlobalFilteredParams{
		TenantID:     f.TenantID,
		ActorUserID:  f.ActorUserID,
		Action:       f.Action,
		ResourceType: f.ResourceType,
		Status:       f.Status,
		ActorType:    f.ActorType,
		FromTs:       f.FromTS,
		ToTs:         f.ToTS,
		Limit:        limit,
		Offset:       offset,
	})
}

// CountGlobalFiltered returns the count matching the same filters used by
// ListGlobalFiltered.
func (r *AuditLogRepository) CountGlobalFiltered(ctx context.Context, f AuditLogFilter) (int64, error) {
	return r.q.CountAuditLogGlobalFiltered(ctx, gen.CountAuditLogGlobalFilteredParams{
		TenantID:     f.TenantID,
		ActorUserID:  f.ActorUserID,
		Action:       f.Action,
		ResourceType: f.ResourceType,
		Status:       f.Status,
		ActorType:    f.ActorType,
		FromTs:       f.FromTS,
		ToTs:         f.ToTS,
	})
}
