// Package compute: console_exec.go implements the interactive (xterm.js)
// shell session-open for any running instance (WS-32). The orchestration
// mirrors console_vnc.go (WS-24) almost 1:1:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary; the
//     audit gate maps /api/v1/compute/instances/{id}/console ->
//     compute.instance.console.exec).
//  2. Lookup the cached instance row; reject if missing or not Running.
//     Unlike VNC, BOTH containers and VMs are allowed (Incus supports
//     exec on both); there is no ErrInstanceNotVM path here.
//  3. Audit emit (action=compute.instance.console.exec.connect,
//     status=success) BEFORE the Incus call (mirror WS-24's timing).
//  4. Incus OpenInteractiveExec -> returns (opID, stdin/stdout/control
//     secrets).
//
// The WebSocket bridge (the bytes-pump between the browser's xterm.js
// client and the Incus per-fd WebSockets) lives in the API layer
// (api/compute_console_handlers.go) because it owns the Fiber WebSocket
// upgrade + the *websocket.Conn lifetime — both concerns that don't
// belong in the service.
//
// Per WS-32 scope: works for BOTH containers and VMs, ANSI/byte
// protocol (not RFB), distinct permission (compute.instance.console.exec),
// distinct audit action (compute.instance.console.exec.connect). VMs
// that need the graphical desktop continue to use the existing
// /vnc endpoint from WS-24.
package compute

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// ExecConsoleSession is the API-facing shape returned to the WebSocket
// bridge handler. The handler dials each per-fd WebSocket using the
// embedded secrets and pumps bytes both ways.
type ExecConsoleSession struct {
	// Project is the Incus project name (cached on the instance row);
	// included so the handler can log it without re-reading the row.
	Project string

	// Instance is the Incus instance name (cached on the row); included
	// for the same reason.
	Instance string

	// OperationID is the Incus async-operation UUID returned by
	// OpenInteractiveExec. The handler dials
	// /1.0/operations/<op>/websocket?secret=<secret> with it.
	OperationID string

	// StdinSecret dials fd "0" — the browser's keystrokes flow here.
	StdinSecret string

	// StdoutSecret dials fd "1" — the PTY's combined stdout+stderr
	// (Interactive mode merges them; fd 2 is absent).
	StdoutSecret string

	// ControlSecret dials fd "control" — the JSON control channel for
	// resize + signal messages. Empty when the daemon does not expose
	// one; the bridge degrades gracefully by skipping resize forwarding.
	ControlSecret string
}

// OpenExecConsole opens an interactive shell session against a running
// instance and returns the per-fd secrets the WebSocket bridge needs to
// dial Incus' per-fd WebSockets. Emits an audit row (action=
// compute.instance.console.exec.connect, status=success) BEFORE the
// Incus call so the privileged action is observable even when the
// daemon is down (mirror WS-24's VNC timing).
//
// Returns:
//   - ErrProviderDisabled when the Incus provider is not wired.
//   - ErrInstanceNotFound when the instance row does not exist in the
//     caller's tenant (the repo's tenant scoping enforces this).
//   - ErrInstanceNotRunning when the instance is not in the Running
//     state (exec requires a running instance per WS-14 open question 3).
//   - ErrExecUnavailable when Incus refuses or fails the exec-open.
//
// The caller passes optional cols/rows so the initial PTY dimensions
// match the browser's terminal size; subsequent resizes flow over the
// control fd.
func (s *Service) OpenExecConsole(
	ctx context.Context,
	tenantID, userID, instanceID uuid.UUID,
	command []string,
	cols, rows int,
) (ExecConsoleSession, error) {
	if s.provider == nil {
		return ExecConsoleSession{}, ErrProviderDisabled
	}

	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return ExecConsoleSession{}, ErrInstanceNotFound
		}
		return ExecConsoleSession{}, fmt.Errorf("compute: get instance: %w", err)
	}
	if row.Status != database.InstanceStatusRunning {
		return ExecConsoleSession{}, ErrInstanceNotRunning
	}

	// Audit emit (status=success). The session-open is the privileged
	// action; the bytes-flow afterwards is operational (the audit row
	// already records who opened what when). A daemon failure does NOT
	// mark this row as failure: the user *did* initiate the connect;
	// the daemon being unreachable is an operational error surfaced to
	// the caller via ErrExecUnavailable, not a denied privileged action.
	// (Mirror WS-24's audit timing decision; ADR-0044 records the
	// rationale.)
	_, _ = s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceConsoleExecConnect,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"instance": row.Name,
			"project":  row.ProjectName,
			"command":  nonNilCommand(command),
		},
	})

	// Always inject TERM=xterm so ANSI sequences render in the
	// browser's xterm.js regardless of the instance's default.
	env := map[string]string{"TERM": "xterm"}

	session, err := s.provider.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:     row.ProjectName,
		Instance:    row.Name,
		Command:     nonNilCommand(command),
		Environment: env,
		Width:       cols,
		Height:      rows,
	})
	if err != nil {
		if errors.Is(err, incus.ErrNotFound) {
			// The daemon says the instance is gone even though the
			// cached row says otherwise. Surface as not-found so the
			// caller reconciles.
			return ExecConsoleSession{}, ErrInstanceNotFound
		}
		return ExecConsoleSession{}, fmt.Errorf("%w: %w", ErrExecUnavailable, err)
	}
	return ExecConsoleSession{
		Project:       row.ProjectName,
		Instance:      row.Name,
		OperationID:   session.OperationID,
		StdinSecret:   session.StdinSecret,
		StdoutSecret:  session.StdoutSecret,
		ControlSecret: session.ControlSecret,
	}, nil
}

// DialExecConsoleFD is a thin pass-through to the Incus provider's
// per-fd WebSocket dial. Exposed on the service so the WebSocket bridge
// handler has a single dependency (the service) instead of two (service
// + provider). The caller owns the returned *websocket.Conn's lifetime.
func (s *Service) DialExecConsoleFD(
	ctx context.Context,
	session ExecConsoleSession,
	secret string,
) (*websocket.Conn, error) {
	if s.provider == nil {
		return nil, ErrProviderDisabled
	}
	conn, err := s.provider.DialExecFD(ctx, session.OperationID, secret)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrExecUnavailable, err)
	}
	return conn, nil
}

// nonNilCommand returns the supplied command, defaulting to ["/bin/sh"]
// when nil or empty. The default matches `incus exec <name>` with no
// argv — the most common interactive shell.
func nonNilCommand(cmd []string) []string {
	if len(cmd) == 0 {
		return []string{"/bin/sh"}
	}
	return cmd
}
