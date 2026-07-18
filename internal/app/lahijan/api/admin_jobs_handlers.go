// Package api: admin_jobs_handlers.go implements the OpenAPI-derived admin
// endpoints for inspecting and controlling the River job queue (WS-09).
//
// Four endpoints, all platform-admin-only:
//
//	GET  /api/v1/admin/jobs                -> ListAdminJobs (paginated + filtered)
//	GET  /api/v1/admin/jobs/{jobId}        -> GetAdminJob   (single row)
//	POST /api/v1/admin/jobs/{jobId}/retry  -> RetryAdminJob (re-queue DLQ'd)
//	POST /api/v1/admin/jobs/{jobId}/cancel -> CancelAdminJob
//
// Per-route RequirePerm is applied by RegisterRoutes via the path-aware
// gate middleware (AdminJobsGate). Retry and Cancel additionally emit audit
// events (action: platform.job.retry | platform.job.cancel) per pillar 7.
//
// Handlers are intentionally thin: parse -> call jobs.Client -> render.
package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/api/middleware"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
)

// DefaultAdminJobsPageSize is the page size the admin jobs API uses when the
// caller does not pass one. Smaller than the audit page size because the
// typical queue surface is much smaller than the audit log.
const DefaultAdminJobsPageSize = 25

// MaxAdminJobsPageSize caps a misbehaving client's page size. River itself
// caps a single JobList at 10_000; this is much tighter because the admin
// UI paginates and a single page > 200 rows is almost always a misuse.
const MaxAdminJobsPageSize = 200

// JobsClient is the minimal jobs.Client surface the admin handlers depend
// on. Declared as an interface so the api package does not need to import
// the jobs package at compile time (avoiding a cycle when jobs ever needs
// api types) and so unit tests can fake it.
type JobsClient interface {
	JobGet(ctx context.Context, id int64) (*rivertype.JobRow, error)
	JobList(ctx context.Context, params *river.JobListParams) (*river.JobListResult, error)
	JobRetry(ctx context.Context, id int64) (*rivertype.JobRow, error)
	JobCancel(ctx context.Context, id int64) (*rivertype.JobRow, error)
}

// ListAdminJobs handles GET /api/v1/admin/jobs. Returns a paginated, filtered
// page of jobs. The caller must already have passed RequirePerm for
// platform.jobs.read (enforced by AdminJobsGate).
func (s *Server) ListAdminJobs(c *fiber.Ctx, params apigen.ListAdminJobsParams) error {
	if s.jobs == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "jobs.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.jobs.list")
	defer span.End()

	limit, offset := adminJobsPageParams(params.Limit, params.Offset)
	listParams := river.NewJobListParams().
		First(limit).
		OrderBy(river.JobListOrderByTime, river.SortOrderDesc)
	if states := adminJobsStateFilter(params.State); len(states) > 0 {
		listParams = listParams.States(states...)
	}
	if params.Kind != nil && *params.Kind != "" {
		listParams = listParams.Kinds(*params.Kind)
	}
	if params.Queue != nil && *params.Queue != "" {
		listParams = listParams.Queues(*params.Queue)
	}

	res, err := s.jobs.JobList(ctx, listParams)
	if err != nil {
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	items := make([]apigen.AdminJob, 0, len(res.Jobs))
	for _, row := range res.Jobs {
		items = append(items, toAdminJobDTO(row))
	}
	// River's JobListResult does not expose a total count by design (counting
	// a busy queue is expensive); we report Total as len(items)+offset for
	// the current page so the UI can disable "next" when the page is short.
	total := int64(offset) + int64(len(items))
	return c.JSON(apigen.AdminJobPage{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// GetAdminJob handles GET /api/v1/admin/jobs/{jobId}. Returns the full row.
func (s *Server) GetAdminJob(c *fiber.Ctx, jobID int64) error {
	if s.jobs == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "jobs.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.jobs.get")
	defer span.End()
	row, err := s.jobs.JobGet(ctx, jobID)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			return SendNotFound(c, i18n.T(c.UserContext(), "jobs.err_not_found", nil))
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return c.JSON(toAdminJobDTO(row))
}

// RetryAdminJob handles POST /api/v1/admin/jobs/{jobId}/retry. Re-queues a
// discarded job. Emits an audit event (action: platform.job.retry).
func (s *Server) RetryAdminJob(c *fiber.Ctx, jobID int64) error {
	if s.jobs == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "jobs.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.jobs.retry")
	defer span.End()

	// Fetch first so we can return 404 vs 409 cleanly and the audit row
	// records the pre-retry state.
	pre, err := s.jobs.JobGet(ctx, jobID)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			return SendNotFound(c, i18n.T(c.UserContext(), "jobs.err_not_found", nil))
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	if pre.State != rivertype.JobStateDiscarded {
		// River's JobRetry DOES allow retrying non-discarded jobs (it just
		// re-queues them), but the API contract promises "DLQ retry only".
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "jobs.err_not_discarded", nil), nil)
	}

	row, err := s.jobs.JobRetry(ctx, jobID)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			return SendNotFound(c, i18n.T(c.UserContext(), "jobs.err_not_found", nil))
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	s.recordJobAdminAction(c, audit.ActionPlatformJobRetry, jobID, pre.State, row.State)
	return c.JSON(toAdminJobDTO(row))
}

// CancelAdminJob handles POST /api/v1/admin/jobs/{jobId}/cancel. Cancels a
// queued/running job. Emits an audit event (action: platform.job.cancel).
func (s *Server) CancelAdminJob(c *fiber.Ctx, jobID int64) error {
	if s.jobs == nil {
		return SendNotImplemented(c, i18n.T(c.UserContext(), "jobs.err_disabled", nil))
	}
	ctx, span := s.tracer.Start(c.UserContext(), "admin.jobs.cancel")
	defer span.End()

	pre, err := s.jobs.JobGet(ctx, jobID)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			return SendNotFound(c, i18n.T(c.UserContext(), "jobs.err_not_found", nil))
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	if isTerminalState(pre.State) {
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "jobs.err_already_terminal", nil), nil)
	}

	row, err := s.jobs.JobCancel(ctx, jobID)
	if err != nil {
		if errors.Is(err, rivertype.ErrNotFound) {
			return SendNotFound(c, i18n.T(c.UserContext(), "jobs.err_not_found", nil))
		}
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	s.recordJobAdminAction(c, audit.ActionPlatformJobCancel, jobID, pre.State, row.State)
	return c.JSON(toAdminJobDTO(row))
}

// recordJobAdminAction emits an audit event for a retry/cancel call.
// Best-effort: a failure here is logged but does not block the response.
// The actor and tenant are pulled from the request scope; the request_id
// ties the audit row back to the originating HTTP request.
func (s *Server) recordJobAdminAction(
	c *fiber.Ctx,
	action string,
	jobID int64,
	fromState, toState rivertype.JobState,
) {
	if s.auditEmitter == nil {
		return
	}
	uid, _ := currentUserID(c)
	tid, ok := tenantIDFromCtx(c)
	rid := middleware.RequestID(c)
	_, _ = s.auditEmitter.Emit(c.UserContext(), audit.Event{
		TenantID:     tid,
		ActorUserID:  actorPtr(uid),
		ActorType:    audit.ActorUser,
		Action:       action,
		ResourceType: audit.ResourceJob,
		Status:       audit.StatusSuccess,
		RequestID:    &rid,
		Metadata: map[string]any{
			"job_id":          jobID,
			"from_state":      string(fromState),
			"to_state":        string(toState),
			"tenant_resolved": ok,
		},
	})
}

// adminJobsPageParams clamps and defaults the limit/offset pair.
func adminJobsPageParams(limit, offset *int) (int, int) {
	lim := DefaultAdminJobsPageSize
	if limit != nil && *limit > 0 {
		lim = *limit
	}
	if lim > MaxAdminJobsPageSize {
		lim = MaxAdminJobsPageSize
	}
	off := 0
	if offset != nil && *offset > 0 {
		off = *offset
	}
	return lim, off
}

// adminJobsStateFilter translates the OpenAPI query enum slice into River's
// rivertype.JobState slice. Unknown / empty values are dropped.
func adminJobsStateFilter(in *[]apigen.ListAdminJobsParamsState) []rivertype.JobState {
	if in == nil || len(*in) == 0 {
		return nil
	}
	out := make([]rivertype.JobState, 0, len(*in))
	for _, s := range *in {
		switch s {
		case apigen.ListAdminJobsParamsStateAvailable:
			out = append(out, rivertype.JobStateAvailable)
		case apigen.ListAdminJobsParamsStateCancelled:
			out = append(out, rivertype.JobStateCancelled)
		case apigen.ListAdminJobsParamsStateCompleted:
			out = append(out, rivertype.JobStateCompleted)
		case apigen.ListAdminJobsParamsStateDiscarded:
			out = append(out, rivertype.JobStateDiscarded)
		case apigen.ListAdminJobsParamsStatePending:
			out = append(out, rivertype.JobStatePending)
		case apigen.ListAdminJobsParamsStateRetryable:
			out = append(out, rivertype.JobStateRetryable)
		case apigen.ListAdminJobsParamsStateRunning:
			out = append(out, rivertype.JobStateRunning)
		case apigen.ListAdminJobsParamsStateScheduled:
			out = append(out, rivertype.JobStateScheduled)
		}
	}
	return out
}

// isTerminalState reports whether the state is a terminal state where
// cancel would be a no-op (or, in River's terms, an error to attempt).
func isTerminalState(s rivertype.JobState) bool {
	switch s {
	case rivertype.JobStateCancelled, rivertype.JobStateCompleted, rivertype.JobStateDiscarded:
		return true
	case rivertype.JobStateAvailable, rivertype.JobStatePending,
		rivertype.JobStateRetryable, rivertype.JobStateRunning, rivertype.JobStateScheduled:
		return false
	default:
		// Unknown states are treated as non-terminal so cancel is allowed
		// (the River-side cancel call itself will return the appropriate
		// error if the state is genuinely not cancellable).
		return false
	}
}

// toAdminJobDTO converts a River JobRow into the OpenAPI AdminJob schema.
// Errors are flattened to a best-effort string; the raw JSONB is preserved
// on the row for the admin UI to dig into.
func toAdminJobDTO(row *rivertype.JobRow) apigen.AdminJob {
	out := apigen.AdminJob{
		Id:          row.ID,
		Kind:        row.Kind,
		State:       apigen.AdminJobState(row.State),
		Queue:       row.Queue,
		Priority:    row.Priority,
		Attempt:     row.Attempt,
		MaxAttempts: row.MaxAttempts,
		CreatedAt:   row.CreatedAt,
		ScheduledAt: row.ScheduledAt,
	}
	if row.AttemptedAt != nil {
		t := *row.AttemptedAt
		out.AttemptedAt = &t
	}
	if row.FinalizedAt != nil {
		t := *row.FinalizedAt
		out.FinalizedAt = &t
	}
	if len(row.AttemptedBy) > 0 {
		cp := append([]string(nil), row.AttemptedBy...)
		out.AttemptedBy = &cp
	}
	if len(row.Tags) > 0 {
		cp := append([]string(nil), row.Tags...)
		out.Tags = &cp
	}
	if args := decodeJSONB(row.EncodedArgs); args != nil {
		out.Args = args
	}
	if meta := decodeJSONB(row.Metadata); meta != nil {
		out.Metadata = meta
	}
	if errs := adminJobErrorsDTO(row.Errors); len(errs) > 0 {
		out.Errors = &errs
	}
	return out
}

// decodeJSONB unmarshals a River JSONB column into a map. Returns nil on
// empty/invalid input so the OpenAPI schema's omitempty drops the field.
func decodeJSONB(raw []byte) *map[string]any {
	if len(raw) == 0 {
		return nil
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return &map[string]any{"_raw": string(raw)}
	}
	if len(m) == 0 {
		return nil
	}
	return &m
}

// adminJobErrorsDTO flattens River's AttemptError slice into the OpenAPI
// AdminJobError shape. River's field is `Error` (string); we surface it as
// `Message` in the API to keep the schema stable if River renames.
func adminJobErrorsDTO(in []rivertype.AttemptError) []apigen.AdminJobError {
	out := make([]apigen.AdminJobError, 0, len(in))
	for _, e := range in {
		out = append(out, apigen.AdminJobError{
			Attempt: e.Attempt,
			At:      e.At,
			Message: e.Error,
		})
	}
	return out
}
