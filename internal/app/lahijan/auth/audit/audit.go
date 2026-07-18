// Package audit defines the audit-event seam every auth (and later every
// privileged) action emits through (pillar 7, WS-06 DoD). WS-08 wires the real
// RequirePerm-based enforcement and the MarkOutcome pattern that records the
// outcome of a privileged action after the side effect completes.
//
// The audit_log table is fully append-only (migration 0005); WS-08 adds a
// sibling audit_log_outcomes table (migration 0010) so the MarkOutcome trail
// is itself append-only. A typical privileged-action flow is:
//
//	auditID, _ := emitter.Emit(ctx, Event{Action: "compute.instance.create", Status: StatusPending})
//	defer func() { _ = emitter.MarkOutcome(ctx, auditID, status, details) }()
//	// ... perform the privileged action ...
//
// For fire-and-forget events (the WS-06 pattern), Emit alone with a final
// status is still sufficient; MarkOutcome is only needed when the caller wants
// to record what happened AFTER Emit ran.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Action constants. The auth subsystem actions live here (defined in WS-06);
// module-specific actions (compute, dns, s3, billing, plugins) live in the
// rbac permission registry (internal/app/lahijan/auth/rbac) and are mirrored
// here as i18n-keyed strings so the audit query API can render them.
//
// Format: "scope.action" (e.g. "auth.user.login", "compute.instance.create")
// per docs/glossary.md.
const (
	ActionRegister           = "auth.user.register"
	ActionLogin              = "auth.user.login"
	ActionLogout             = "auth.user.logout"
	ActionRefresh            = "auth.session.refresh"
	ActionRefreshReuse       = "auth.session.refresh_reuse"
	ActionVerifyEmail        = "auth.email.verify"
	ActionResendVerification = "auth.email.resend_verification"
	ActionPasswordResetReq   = "auth.password.reset_request"
	ActionPasswordResetConf  = "auth.password.reset_confirm"
	ActionEmailChangeReq     = "auth.email.change_request"
	ActionEmailChangeConf    = "auth.email.change_confirm"
	ActionPasswordChange     = "auth.password.change"
	ActionProfileUpdate      = "auth.user.profile_update"
	ActionPATCreate          = "auth.pat.create"
	ActionPATRevoke          = "auth.pat.revoke"
	ActionPATUse             = "auth.pat.use"

	// External IdP actions (WS-07a). Linked = a new (provider, subject)
	// row was bound to a user; Login = an existing identity was used to
	// log in; Unlink = a row was removed.
	ActionIdpLink   = "auth.idp.link"
	ActionIdpLogin  = "auth.idp.login"
	ActionIdpUnlink = "auth.idp.unlink"

	// Audit subsystem actions (the audit query API itself emits these).
	ActionAuditExport = "audit.export"
	ActionRBACRoleOps = "rbac.role.update"
)

// Standard statuses recorded on audit_log.status and audit_log_outcomes.status.
// Pending is used when Emit opens a row before a side effect and MarkOutcome
// finalizes it; success/failure are the terminal states.
const (
	StatusPending = "pending"
	StatusSuccess = "success"
	StatusFailure = "failure"
)

// Actor types recorded on audit_log.actor_type.
const (
	ActorUser   = "user"
	ActorSystem = "system"
	ActorPlugin = "plugin"
)

// ResourceType constants. The auth subsystem resources live here; module
// resources (instance, zone, bucket, ...) live in their own packages and are
// passed in as strings when those modules ship.
const (
	ResourceUser    = "user"
	ResourceSession = "session"
	ResourcePAT     = "personal_access_token"
	ResourceEmail   = "email"
	ResourceAudit   = "audit_log"
	ResourceRole    = "role"
	ResourceTenant  = "tenant"
)

// Event is the data an emitter records. TenantID is nil for system-level auth
// events (login is global); Metadata is a free-form JSON blob for details that
// don't deserve their own column.
type Event struct {
	TenantID     *uuid.UUID
	ActorUserID  *uuid.UUID
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	Status       string
	RequestID    *string
	Metadata     map[string]any
}

// Outcome carries the data for a MarkOutcome call. Details is a free-form JSON
// blob (typically error context for failures).
type Outcome struct {
	Status  string
	Details map[string]any
}

// Emitter records audit events and their outcomes. Implementations must be
// safe for concurrent use. Emit and MarkOutcome must never block the caller
// indefinitely: a failing write is logged but must not roll back the audited
// action. Both methods append rows; nothing ever updates or deletes them.
type Emitter interface {
	// Emit writes a row to audit_log and returns its id so the caller can
	// pass it to MarkOutcome. Callers that don't need the outcome trail can
	// discard the id.
	Emit(ctx context.Context, event Event) (uuid.UUID, error)

	// MarkOutcome appends a row to audit_log_outcomes for the given audit
	// id, recording the post-side-effect status. Safe to call multiple times
	// for the same audit id (the latest row wins as the "current" status).
	// Returns an error if the audit id does not exist or the write fails.
	MarkOutcome(ctx context.Context, auditID uuid.UUID, outcome Outcome) error
}

// NoopEmitter discards every event. Used in tests that don't assert on audit
// rows and in dev runs where the DB is unavailable.
type NoopEmitter struct{}

// Emit implements Emitter by doing nothing and returning the nil UUID.
func (NoopEmitter) Emit(_ context.Context, _ Event) (uuid.UUID, error) {
	return uuid.Nil, nil
}

// MarkOutcome implements Emitter by doing nothing.
func (NoopEmitter) MarkOutcome(_ context.Context, _ uuid.UUID, _ Outcome) error {
	return nil
}

// DBEmitter persists events to the audit_log table via AuditLogRepository and
// outcomes to audit_log_outcomes. Both tables are append-only by trigger, so
// this emitter is the only sanctioned mutating path either table can hit.
type DBEmitter struct {
	repo *database.AuditLogRepository
}

// NewDBEmitter wraps an AuditLogRepository.
func NewDBEmitter(repo *database.AuditLogRepository) *DBEmitter {
	return &DBEmitter{repo: repo}
}

// Emit writes the event to the audit_log table and returns the new row id.
func (e *DBEmitter) Emit(ctx context.Context, ev Event) (uuid.UUID, error) {
	status := ev.Status
	if status == "" {
		status = StatusSuccess
	}
	actorType := ev.ActorType
	if actorType == "" {
		actorType = ActorUser
	}
	var meta json.RawMessage
	if ev.Metadata != nil {
		raw, err := json.Marshal(ev.Metadata)
		if err != nil {
			return uuid.Nil, fmt.Errorf("audit: marshal metadata: %w", err)
		}
		meta = raw
	}
	row, err := e.repo.Create(ctx, database.CreateAuditLogParams{
		TenantID:     ev.TenantID,
		ActorUserID:  ev.ActorUserID,
		ActorType:    actorType,
		Action:       ev.Action,
		ResourceType: ev.ResourceType,
		ResourceID:   ev.ResourceID,
		Status:       &status,
		RequestID:    ev.RequestID,
		Metadata:     meta,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: emit %s: %w", ev.Action, err)
	}
	return row.ID, nil
}

// MarkOutcome appends a row to audit_log_outcomes for the given audit id.
func (e *DBEmitter) MarkOutcome(ctx context.Context, auditID uuid.UUID, outcome Outcome) error {
	status := outcome.Status
	if status == "" {
		status = StatusSuccess
	}
	var details json.RawMessage
	if outcome.Details != nil {
		raw, err := json.Marshal(outcome.Details)
		if err != nil {
			return fmt.Errorf("audit: marshal outcome details: %w", err)
		}
		details = raw
	}
	if err := e.repo.MarkOutcome(ctx, auditID, status, details); err != nil {
		return fmt.Errorf("audit: mark outcome %s: %w", auditID, err)
	}
	return nil
}
