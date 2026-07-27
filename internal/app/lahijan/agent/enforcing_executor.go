// Package agent: enforcing_executor.go wraps a ToolExecutor so every tool
// call enforces RBAC (rbac.Require against the calling user's permissions)
// and emits an audit row before + after the side effect.
//
// The LLMHarness holds this executor (not the raw bridge) so the read-only
// tool loop is gated exactly like a manual UI action: the agent is "just
// another caller" of the module surface (pillar 2 + 7). Tenant scoping is
// enforced at the repository seam; RBAC + audit are enforced here.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
)

// EnforcingExecutor decorates a ToolExecutor with per-tool RBAC + audit. It
// delegates Destructive/Describe/All to the inner executor unchanged.
type EnforcingExecutor struct {
	inner  ToolExecutor
	policy rbac.PolicyEvaluator
	audit  audit.Emitter
}

var _ ToolExecutor = (*EnforcingExecutor)(nil)

// NewEnforcingExecutor wraps inner so every Execute is permission-gated and
// audit-logged. policy / auditEmitter must be non-nil; a nil policy fails
// every call closed (rbac.Require returns ErrPermissionDenied when policy
// is nil).
func NewEnforcingExecutor(
	inner ToolExecutor,
	policy rbac.PolicyEvaluator,
	auditEmitter audit.Emitter,
) *EnforcingExecutor {
	return &EnforcingExecutor{inner: inner, policy: policy, audit: auditEmitter}
}

// toolPermission maps each advertised tool to the RBAC permission slug it
// requires. A tool not present here is left to the inner executor (which
// returns ErrToolNotFound for unknown names) — no RBAC decision is made for
// tools the bridge does not own.
var toolPermission = map[string]string{
	toolDNSListZones:       rbac.PermDNSZoneRead,
	toolComputeListInsts:   rbac.PermComputeInstanceRead,
	toolStorageListBuckets: rbac.PermS3BucketRead,
	toolBillingListUsage:   rbac.PermBillingLedgerRead,
	toolAuditListEvents:    rbac.PermAuditRead,
}

// Destructive delegates to the inner executor.
func (e *EnforcingExecutor) Destructive(tool string) bool { return e.inner.Destructive(tool) }

// Describe delegates to the inner executor.
func (e *EnforcingExecutor) Describe(tool string) (ToolDescriptor, bool) {
	return e.inner.Describe(tool)
}

// All delegates to the inner executor.
func (e *EnforcingExecutor) All() []ToolDescriptor { return e.inner.All() }

// Execute enforces the tool's permission, emits a pending audit row, runs the
// inner executor, and finalises the audit row with the outcome.
func (e *EnforcingExecutor) Execute(
	ctx context.Context,
	tool string,
	args json.RawMessage,
) (json.RawMessage, error) {
	userID := actorUserIDFromContext(ctx)
	tenantID, _ := database.TenantFromContext(ctx) // uuid.Nil when absent => Require denies.
	slug, known := toolPermission[tool]
	if !known {
		// Not a module tool (e.g. the stub "ping"); let the inner executor
		// resolve it. No RBAC decision is recorded for unknown tools.
		return e.inner.Execute(ctx, tool, args)
	}
	if err := rbac.Require(ctx, e.policy, userID, tenantID, slug); err != nil {
		// Permission denied: record the attempted call + reason, then surface
		// a wrapped error the harness turns into an EventError.
		e.auditTool(ctx, tool, userID, audit.StatusFailure,
			map[string]any{"permission": slug, "denied": err.Error()})
		return nil, fmt.Errorf("agent.tool.execute %s: %w", tool, err)
	}
	auditID := e.auditTool(ctx, tool, userID, audit.StatusPending,
		map[string]any{"permission": slug})
	result, execErr := e.inner.Execute(ctx, tool, args)
	outcome := audit.StatusSuccess
	if execErr != nil {
		outcome = audit.StatusFailure
	}
	if auditID != uuid.Nil {
		_ = e.audit.MarkOutcome(ctx, auditID, audit.Outcome{Status: outcome})
	}
	return result, execErr
}

// auditTool emits an audit row for a tool execution and returns its id
// (uuid.Nil when the emit failed — audit never blocks the action).
func (e *EnforcingExecutor) auditTool(
	ctx context.Context,
	tool string,
	userID uuid.UUID,
	status string,
	extra map[string]any,
) uuid.UUID {
	meta := make(map[string]any, len(extra)+2)
	meta["via_agent"] = true
	meta["tool"] = tool
	for k, v := range extra {
		meta[k] = v
	}
	ev := audit.Event{
		Action:       audit.ActionAgentToolExecute,
		ResourceType: ResourceToolCall,
		Status:       status,
		ActorType:    audit.ActorUser,
		Metadata:     meta,
	}
	if userID != uuid.Nil {
		ev.ActorUserID = &userID
	}
	id, err := e.audit.Emit(ctx, ev)
	if err != nil {
		return uuid.Nil
	}
	return id
}
