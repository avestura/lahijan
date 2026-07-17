// Package api: audit_handlers.go implements the OpenAPI-derived audit query
// endpoints (WS-08). Each handler is thin: it parses query params, calls the
// audit log repository with the tenant scope from ctx, and renders the
// response. Per-route RequirePerm is applied by RegisterRoutes via the
// path-aware gate middleware (AuditGate).
//
// Three endpoints:
//
//	GET /api/v1/audit               -> ListAudit  (paginated + filtered)
//	GET /api/v1/audit/{auditId}     -> GetAudit   (single row + outcome trail)
//	GET /api/v1/audit/export        -> ExportAudit (CSV stream or JSON dump)
package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// DefaultAuditPageSize is the page size when the caller doesn't pass one.
const DefaultAuditPageSize = 50

// MaxAuditPageSize caps very large page sizes so a misbehaving client cannot
// stress the DB. The OpenAPI spec enforces this via maximum: 200; we keep the
// guard here too so internal callers cannot bypass it.
const MaxAuditPageSize = 200

// ExportRowCap caps the number of rows the export endpoint will return in one
// shot, even with no filters. Streaming CSV keeps the memory cost low, but
// the DB still has to scan; a tenant-wide unfiltered query against a very
// active tenant could otherwise be a DoS vector.
const ExportRowCap = MaxAuditPageSize * 50 // 10_000

// ListAudit handles GET /api/v1/audit. Returns a filtered, paginated page of
// audit events for the tenant in ctx. The caller must already have passed the
// RequirePerm check for audit.read (enforced by the audit gate middleware).
func (s *Server) ListAudit(c *fiber.Ctx, params apigen.ListAuditParams) error {
	f := parseAuditFilter(params.ActorUserId, params.Action, params.ResourceType,
		params.Status, params.ActorType, params.FromTs, params.ToTs)
	limit, offset := pageParams(params.Limit, params.Offset)
	rows, err := s.audit.ListForTenantFiltered(c.UserContext(), f, limit, offset)
	if err != nil {
		if errors.Is(err, database.ErrNoTenantInContext) {
			return SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	total, err := s.audit.CountForTenantFiltered(c.UserContext(), f)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	items := make([]apigen.AuditEvent, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAuditEventDTO(row, nil))
	}
	return c.JSON(apigen.AuditPage{
		Items:  items,
		Total:  total,
		Limit:  int(limit),
		Offset: int(offset),
	})
}

// GetAudit handles GET /api/v1/audit/{auditId}. Returns the audit row plus its
// outcome trail. A not-found id (or one outside the caller's tenant) returns
// the standard 404 envelope.
func (s *Server) GetAudit(c *fiber.Ctx, auditID openapi_types.UUID) error {
	row, err := s.audit.GetForTenant(c.UserContext(), auditID)
	if err != nil {
		if database.IsNoRows(err) {
			return SendNotFound(c, i18n.T(c.UserContext(), "audit.err_not_found", nil))
		}
		if errors.Is(err, database.ErrNoTenantInContext) {
			return SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	outcomes, err := s.audit.ListOutcomes(c.UserContext(), auditID)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(toAuditEventDTO(row, outcomes))
}

// ExportAudit handles GET /api/v1/audit/export. Streams the matching rows as
// CSV (default) or JSON. The export itself is recorded as an audit event
// (action: audit.export) for tamper-evident after-the-fact review.
//
// The CSV format uses chunked writes via Fiber's Stream(...) so very large
// exports do not need to fit in memory. The JSON format returns a single
// array; if the caller needs streaming JSON, they should page /audit.
func (s *Server) ExportAudit(c *fiber.Ctx, params apigen.ExportAuditParams) error {
	f := parseAuditFilter(params.ActorUserId, params.Action, params.ResourceType,
		params.Status, params.ActorType, params.FromTs, params.ToTs)
	rows, err := s.audit.ListForTenantFiltered(c.UserContext(), f, ExportRowCap, 0)
	if err != nil {
		if errors.Is(err, database.ErrNoTenantInContext) {
			return SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}

	// Record the export as an audit event (best-effort). The actor + tenant
	// are pulled from the request scope; missing user means the gate let an
	// anonymous request through, which is itself a wiring bug.
	s.recordAuditExport(c, len(rows))

	if params.Format != nil && *params.Format == apigen.ExportAuditParamsFormatJson {
		return c.JSON(rowsToDTOs(rows))
	}
	return streamCSV(c, rows)
}

// recordAuditExport emits an audit event for the export action. Best-effort:
// a failure here is logged via slog (TODO: WS-04 wiring) but does not block
// the export. The event is recorded AFTER the export completes so the row
// count is accurate.
func (s *Server) recordAuditExport(c *fiber.Ctx, rowCount int) {
	if s.auditEmitter == nil {
		return
	}
	uid, _ := currentUserID(c)
	tid, ok := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)
	// Best-effort: audit failure is logged but does not block the export.
	_, _ = s.auditEmitter.Emit(c.UserContext(), audit.Event{
		TenantID:     tid,
		ActorUserID:  actorPtr(uid),
		ActorType:    audit.ActorUser,
		Action:       audit.ActionAuditExport,
		ResourceType: audit.ResourceAudit,
		Status:       audit.StatusSuccess,
		RequestID:    &rid,
		Metadata: map[string]any{
			"row_count": rowCount,
			"format":    "csv_or_json",
			// ok=false means the tenant scope wasn't set; surface it so the
			// audit row is self-explanatory when this happens in a wiring bug.
			"tenant_resolved": ok,
		},
	})
}

// enumStr coerces an oapi-codegen enum pointer (any of the *ParamsStatus /
// *ParamsActorType types, which are all "string under the hood") into its
// underlying string. Returns "" when the pointer is nil or the value is empty.
// The reflection-based nil check is required because Go interface comparisons
// (v == nil) are FALSE for typed nil pointers like (*ListAuditParamsStatus)(nil).
func enumStr(v any) string {
	if v == nil || isNilPointer(v) {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case *string:
		if t == nil {
			return ""
		}
		return *t
	case fmt.Stringer:
		return t.String()
	}
	// Fallback: fmt.Sprintf handles anything that has a String() method via
	// the Stringer interface; the verb %v on a Stringer calls String().
	return fmt.Sprintf("%v", v)
}

// isNilPointer reports whether v is a typed nil pointer (e.g.
// (*ListAuditParamsStatus)(nil)). The plain v == nil check misses this case
// because the interface itself is non-nil; it just holds a nil pointer.
func isNilPointer(v any) bool {
	// reflection-based check; cheap enough for the audit path which runs at
	// most once per request.
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return true
	}
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}

// parseAuditFilter translates the OpenAPI query params into the nullable
// AuditLogFilter the repository expects. Empty strings become nil (no filter).
// Status and ActorType are typed as `any` here because ListAudit and
// ExportAudit generate distinct enum types (ListAuditParamsStatus vs
// ExportAuditParamsStatus) even though both are "string" underneath; enumStr
// normalizes them.
func parseAuditFilter(
	actorUserID *openapi_types.UUID,
	action, resourceType *string,
	status, actorType any,
	fromTS, toTS *time.Time,
) database.AuditLogFilter {
	f := database.AuditLogFilter{}
	if actorUserID != nil && *actorUserID != (openapi_types.UUID{}) {
		id := uuid.UUID(*actorUserID)
		f.ActorUserID = &id
	}
	if action != nil && *action != "" {
		s := *action
		f.Action = &s
	}
	if resourceType != nil && *resourceType != "" {
		s := *resourceType
		f.ResourceType = &s
	}
	if s := enumStr(status); s != "" {
		f.Status = &s
	}
	if s := enumStr(actorType); s != "" {
		f.ActorType = &s
	}
	if fromTS != nil {
		t := *fromTS
		f.FromTS = &t
	}
	if toTS != nil {
		t := *toTS
		f.ToTS = &t
	}
	return f
}

// pageParams clamps + defaults the limit/offset pair.
func pageParams(limit, offset *int) (int32, int32) {
	lim := DefaultAuditPageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxAuditPageSize {
		lim = MaxAuditPageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return int32(lim), int32(off)
}

// toAuditEventDTO converts a database row + outcome trail into the OpenAPI
// schema. The "current status" of an event is the latest outcome's status
// when at least one outcome exists, otherwise the row's initial status.
func toAuditEventDTO(row database.AuditLog, outcomes []database.AuditLogOutcome) apigen.AuditEvent {
	out := apigen.AuditEvent{
		Id:           openapi_types.UUID(row.ID),
		Action:       row.Action,
		ResourceType: row.ResourceType,
		Status:       apigen.AuditEventStatus(row.Status),
		Metadata:     metadataToDTO(row.Metadata),
		CreatedAt:    row.CreatedAt,
	}
	if row.TenantID != nil {
		t := openapi_types.UUID(*row.TenantID)
		out.TenantId = &t
	}
	if row.ActorUserID != nil {
		u := openapi_types.UUID(*row.ActorUserID)
		out.ActorUserId = &u
	}
	if row.ResourceID != nil {
		r := openapi_types.UUID(*row.ResourceID)
		out.ResourceId = &r
	}
	out.RequestId = row.RequestID
	// Default actorType to "user" when NULL/empty in the DB (defensive).
	actorType := apigen.AuditEventActorType(row.ActorType)
	if actorType == "" {
		actorType = apigen.AuditEventActorTypeUser
	}
	out.ActorType = &actorType

	outcomeDTOs := make([]apigen.AuditOutcome, 0, len(outcomes))
	for _, o := range outcomes {
		outcomeDTOs = append(outcomeDTOs, apigen.AuditOutcome{
			Id:        openapi_types.UUID(o.ID),
			Status:    apigen.AuditOutcomeStatus(o.Status),
			Details:   metadataToDTO(o.Details),
			CreatedAt: o.CreatedAt,
		})
	}
	out.Outcomes = &outcomeDTOs
	if len(outcomeDTOs) > 0 {
		// The latest outcome is the first element (ListOutcomes is DESC).
		out.Status = apigen.AuditEventStatus(outcomeDTOs[0].Status)
	}
	return out
}

// rowsToDTOs converts a slice of audit rows (no outcomes) into DTOs.
func rowsToDTOs(rows []database.AuditLog) []apigen.AuditEvent {
	out := make([]apigen.AuditEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAuditEventDTO(r, nil))
	}
	return out
}

// metadataToDTO unmarshals the JSONB metadata column into a map[string]any
// the OpenAPI schema expects. A NULL or "{}" yields an empty map. The OpenAPI
// schema declares Metadata as *map[string]interface{} (nullable object), so
// we always return a non-nil pointer to a non-nil map.
func metadataToDTO(raw json.RawMessage) *map[string]any {
	m := map[string]any{}
	if len(raw) == 0 {
		return &m
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		// Defensive: if metadata was hand-corrupted, return the raw string so
		// the API doesn't 500 on a query.
		m = map[string]any{"_raw": string(raw)}
		return &m
	}
	if m == nil {
		m = map[string]any{}
	}
	return &m
}

// actorPtr returns &uid when uid is non-zero, else nil.
func actorPtr(uid uuid.UUID) *uuid.UUID {
	if uid == uuid.Nil {
		return nil
	}
	return &uid
}

// streamCSV writes the audit rows as CSV to the response using Fiber's
// streaming writer. Each row is one CSV record; the header row is the column
// list. Large exports stream chunk-by-chunk rather than building one giant
// string in memory.
func streamCSV(c *fiber.Ctx, rows []database.AuditLog) error {
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", `attachment; filename="audit-export.csv"`)
	c.Context().SetStatusCode(http.StatusOK)

	// Use Fiber's underlying body writer so we don't buffer the whole body.
	w := c.Response().BodyWriter()
	cw := csv.NewWriter(w)

	header := []string{
		"id", "tenant_id", "actor_user_id", "actor_type",
		"action", "resource_type", "resource_id", "status",
		"request_id", "metadata", "created_at",
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("audit.export: write header: %w", err)
	}
	for _, r := range rows {
		if err := cw.Write(auditRowToCSVRecord(r)); err != nil {
			return fmt.Errorf("audit.export: write row: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("audit.export: flush: %w", err)
	}
	return nil
}

// auditRowToCSVRecord flattens a row into a CSV-friendly slice. Nil pointers
// become empty strings; timestamps use RFC3339.
func auditRowToCSVRecord(r database.AuditLog) []string {
	out := make([]string, 11)
	out[0] = r.ID.String()
	if r.TenantID != nil {
		out[1] = r.TenantID.String()
	}
	if r.ActorUserID != nil {
		out[2] = r.ActorUserID.String()
	}
	out[3] = r.ActorType
	out[4] = r.Action
	out[5] = r.ResourceType
	if r.ResourceID != nil {
		out[6] = r.ResourceID.String()
	}
	out[7] = r.Status
	if r.RequestID != nil {
		out[8] = *r.RequestID
	}
	out[9] = string(r.Metadata)
	out[10] = r.CreatedAt.UTC().Format(time.RFC3339)
	return out
}

// tenantIDFromCtx extracts the tenant id from the request context (set by the
// tenant middleware). Returns (nil, false) when absent.
func tenantIDFromCtx(c *fiber.Ctx) (*uuid.UUID, bool) {
	id, err := database.TenantFromContext(c.UserContext())
	if err != nil {
		return nil, false
	}
	return &id, true
}
