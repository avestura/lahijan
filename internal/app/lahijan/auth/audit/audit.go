// Package audit defines the audit-event seam every auth (and later every
// privileged) action emits through (pillar 7, WS-06 DoD). WS-08 wires the real
// RequirePerm-based enforcement; for WS-06 the Emitter interface and a
// database-backed implementation are sufficient, since the audit_log table and
// its append-only trigger already exist (migration 0005).
//
// Auth actions call Emit BEFORE the side effect with status="success" or
// "failure"; when an action spans a longer flow (e.g. login that can fail
// mid-way), emit once with the final status rather than two rows.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// Action constants for the auth subsystem. Formatted scope.action per the
// glossary; reused across login, logout, refresh, PAT, and email flows.
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
)

// Standard statuses. Anything other than StatusSuccess is a failure.
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
)

// Actor types recorded on audit_log.actor_type.
const (
	ActorUser   = "user"
	ActorSystem = "system"
)

// ResourceType constants for the auth subsystem.
const (
	ResourceUser    = "user"
	ResourceSession = "session"
	ResourcePAT     = "personal_access_token"
	ResourceEmail   = "email"
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

// Emitter records an audit event. Implementations must be safe for concurrent
// use. Emit must never block the caller indefinitely: a failing emit is logged
// but must not roll back the audited action.
type Emitter interface {
	Emit(ctx context.Context, event Event) error
}

// NoopEmitter discards every event. Used in tests that don't assert on audit
// rows.
type NoopEmitter struct{}

// Emit implements Emitter by doing nothing.
func (NoopEmitter) Emit(_ context.Context, _ Event) error { return nil }

// DBEmitter persists events to the audit_log table via AuditLogRepository. The
// append-only trigger (migration 0005) is the only mutating path it can hit.
type DBEmitter struct {
	repo *database.AuditLogRepository
}

// NewDBEmitter wraps an AuditLogRepository.
func NewDBEmitter(repo *database.AuditLogRepository) *DBEmitter {
	return &DBEmitter{repo: repo}
}

// Emit writes the event to the audit_log table.
func (e *DBEmitter) Emit(ctx context.Context, ev Event) error {
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
			return fmt.Errorf("audit: marshal metadata: %w", err)
		}
		meta = raw
	}
	_, err := e.repo.Create(ctx, database.CreateAuditLogParams{
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
		return fmt.Errorf("audit: emit %s: %w", ev.Action, err)
	}
	return nil
}
