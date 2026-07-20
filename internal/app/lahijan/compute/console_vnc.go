// Package compute: console_vnc.go implements the graphical (noVNC) console
// session-open for VM instances (WS-24). The orchestration is:
//
//  1. RBAC check (api/middleware.RequirePerm at the HTTP boundary; the
//     audit gate maps /api/v1/compute/instances/{id}/vnc ->
//     compute.instance.console.vnc).
//  2. Lookup the cached instance row; reject if missing, if it is not a
//     virtual machine, or if it is not Running.
//  3. Audit emit (action=compute.instance.console.vnc.connect,
//     status=success). The audit row is emitted BEFORE the Incus call so
//     it persists even if the daemon is unreachable; a daemon failure
//     surfaces to the caller as ErrVNCUnavailable but the audit row stays.
//  4. Incus OpenVNCConsole -> returns (opID, secret).
//
// The WebSocket bridge (the bytes-pump between the browser's noVNC client
// and the Incus per-fd WebSocket) lives in the API layer
// (api/compute_vnc_handlers.go) because it owns the Fiber WebSocket upgrade
// + the *websocket.Conn lifetime — both concerns that don't belong in the
// service.
//
// Per WS-24 scope: VM-only, RFB protocol, distinct permission
// (compute.instance.console.vnc), distinct audit action
// (compute.instance.console.vnc.connect). Containers continue to use the
// xterm.js+exec path from WS-14.
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

// VNCConsoleSession is the API-facing shape returned to the WebSocket
// bridge handler. The handler dials the Incus operation's per-fd
// WebSocket using OperationID + Secret and pumps RFB bytes both ways.
type VNCConsoleSession struct {
	// Project is the Incus project name (cached on the instance row);
	// included so the handler can log it without re-reading the row.
	Project string

	// Instance is the Incus instance name (cached on the row); included
	// for the same reason.
	Instance string

	// OperationID is the Incus async-operation UUID returned by
	// OpenVNCConsole. The handler dials
	// /1.0/operations/<op>/websocket?secret=<secret> with it.
	OperationID string

	// Secret is the single per-fd websocket secret. The handler dials
	// the per-fd WebSocket with it and pumps RFB bytes.
	Secret string
}

// OpenVNCConsole opens a graphical (VGA) console session against a running
// VM instance and returns the (opID, secret) the WebSocket bridge needs to
// dial Incus' per-fd WebSocket. Emits an audit row (action=
// compute.instance.console.vnc.connect, status=success) BEFORE the Incus
// call so the privileged action is observable even when the daemon is down.
//
// Returns:
//   - ErrProviderDisabled when the Incus provider is not wired.
//   - ErrInstanceNotFound when the instance row does not exist in the
//     caller's tenant (the repo's tenant scoping enforces this).
//   - ErrInstanceNotVM when the instance is a container (only VMs get a
//     VGA console).
//   - ErrInstanceNotRunning when the instance is not in the Running state.
//   - ErrVNCUnavailable when Incus refuses or fails the console-open.
func (s *Service) OpenVNCConsole(
	ctx context.Context,
	tenantID, userID, instanceID uuid.UUID,
) (VNCConsoleSession, error) {
	if s.provider == nil {
		return VNCConsoleSession{}, ErrProviderDisabled
	}

	row, err := s.repos.ComputeInstances.Get(ctx, instanceID)
	if err != nil {
		if database.IsNoRows(err) {
			return VNCConsoleSession{}, ErrInstanceNotFound
		}
		return VNCConsoleSession{}, fmt.Errorf("compute: get instance: %w", err)
	}
	if row.Type != database.InstanceTypeVirtualMachine {
		return VNCConsoleSession{}, ErrInstanceNotVM
	}
	if row.Status != database.InstanceStatusRunning {
		return VNCConsoleSession{}, ErrInstanceNotRunning
	}

	// Audit emit (status=success). The session-open is the privileged
	// action; the bytes-flow afterwards is operational (the audit row
	// already records who opened what when). A daemon failure does NOT
	// mark this row as failure: the user *did* initiate the connect; the
	// daemon being unreachable is an operational error surfaced to the
	// caller via ErrVNCUnavailable, not a denied privileged action.
	_, _ = s.audit.Emit(ctx, audit.Event{
		TenantID:     &tenantID,
		ActorUserID:  &userID,
		ActorType:    audit.ActorUser,
		Action:       AuditInstanceConsoleVNCConnect,
		ResourceType: ResourceInstance,
		ResourceID:   &row.ID,
		Status:       audit.StatusSuccess,
		Metadata: map[string]any{
			"instance": row.Name,
			"project":  row.ProjectName,
		},
	})

	session, err := s.provider.OpenVNCConsole(ctx, row.ProjectName, row.Name)
	if err != nil {
		if errors.Is(err, incus.ErrNotFound) {
			// The daemon says the instance is gone even though the
			// cached row says otherwise. Surface as not-found so the
			// caller reconciles.
			return VNCConsoleSession{}, ErrInstanceNotFound
		}
		return VNCConsoleSession{}, fmt.Errorf("%w: %v", ErrVNCUnavailable, err)
	}
	return VNCConsoleSession{
		Project:     row.ProjectName,
		Instance:    row.Name,
		OperationID: session.OperationID,
		Secret:      session.Secret,
	}, nil
}

// DialVNCConsole is a thin pass-through to the Incus provider's per-fd
// WebSocket dial. Exposed on the service so the WebSocket bridge handler
// has a single dependency (the service) instead of two (service + provider).
// The caller owns the returned *websocket.Conn's lifetime.
func (s *Service) DialVNCConsole(
	ctx context.Context,
	session VNCConsoleSession,
) (*websocket.Conn, error) {
	if s.provider == nil {
		return nil, ErrProviderDisabled
	}
	conn, err := s.provider.DialVNCConsole(ctx, session.OperationID, session.Secret)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVNCUnavailable, err)
	}
	return conn, nil
}
