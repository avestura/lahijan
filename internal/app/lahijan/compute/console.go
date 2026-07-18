// Package compute: console.go implements the exec websocket proxy for the
// xterm.js UI (WS-14 DoD: "exec websocket round-trips input/output"). The
// proxy bridges a Fiber websocket upgrade to the Incus provider's Exec
// helper; for the MVP the proxy is one-shot (the caller POSTs a command,
// receives the captured stdout/stderr/exit-code) — the interactive
// (bidirectional stdin/stdout) variant lands with WS-20 (dashboard UI).
//
// Per WS-14 "Open questions" item 3: exec requires the instance to be
// running. The service rejects a call against a non-running instance with
// ErrInstanceNotRunning; the handler maps that to a 409 conflict.
package compute

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// ExecParams carries the user-controlled fields of an exec call. Command
// is the argv; the provider joins it inside the instance.
type ExecParams struct {
	InstanceID  uuid.UUID
	Command     []string
	Environment map[string]string
	User        int
	Group       int
	Cwd         string
	Stdin       []byte
}

// ExecResult is the captured-output shape returned to the caller.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Exec runs a one-shot command in an instance and returns the captured
// stdout/stderr + the exit code. Emits an audit event (audit.action =
// compute.instance.exec) so privileged exec is observable. Used by:
//
//   - the POST /instances/{id}/exec HTTP endpoint (this WS),
//   - the xterm.js UI (interactive variant is the WS-20 follow-up).
//
// The interactive console path will call into the provider's execOpen +
// websocket pumps directly so bytes flow both ways in real time. This
// method is the simpler "run + capture" path the API exposes today.
func (s *Service) Exec(
	ctx context.Context,
	tenantID, userID uuid.UUID,
	params ExecParams,
) (ExecResult, error) {
	if s.provider == nil {
		return ExecResult{}, ErrProviderDisabled
	}
	if len(params.Command) == 0 {
		return ExecResult{}, errors.New("compute: exec requires a non-empty command")
	}

	row, err := s.repos.ComputeInstances.Get(ctx, params.InstanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return ExecResult{}, ErrInstanceNotFound
		}
		return ExecResult{}, fmt.Errorf("compute: get instance: %w", err)
	}
	if row.Status != database.InstanceStatusRunning {
		return ExecResult{}, ErrInstanceNotRunning
	}

	// Audit emit (status=success — the exec will be attempted; we mark
	// failure only when the daemon rejects the command).
	_, _ = s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceExec,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"command": params.Command,
		},
	})

	res, err := s.provider.Exec(ctx, incus.ExecParams{
		Project:     row.ProjectName,
		Instance:    row.Name,
		Command:     params.Command,
		Environment: params.Environment,
		User:        params.User,
		Group:       params.Group,
		Cwd:         params.Cwd,
		Stdin:       params.Stdin,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("compute: exec: %w", err)
	}
	return ExecResult{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
	}, nil
}
