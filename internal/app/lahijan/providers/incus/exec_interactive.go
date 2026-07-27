// Package incus: exec_interactive.go wraps the Incus exec endpoint in
// interactive mode (WS-32). Interactive mode is what `incus exec <name> --
// /bin/sh` uses on the CLI: a bidirectional PTY-backed shell session with
// resize support, as opposed to the one-shot "run + capture" path in
// exec.go.
//
// The flow reuses the operation-secret bootstrap from exec.go but flips
// two flags on InstanceExecPost:
//
//   - Interactive: true  -> the daemon allocates a PTY and combines
//     stdout + stderr into fd 1 (stderr fd 2 is omitted).
//   - Width / Height      -> initial PTY dimensions; subsequent resizes
//     flow over the control fd as JSON messages.
//
// When Interactive=true + WaitForWS=true the daemon returns four
// per-fd websocket secrets in metadata.fds:
//
//	"0"       -> stdin  (browser writes keystrokes; the daemon forwards
//	            them to the PTY master).
//	"1"       -> stdout (PTY slave output; stderr is combined in when
//	            Interactive=true so fd 2 is omitted).
//	"2"       -> stderr (only when Interactive=false; absent here).
//	"control" -> control channel for out-of-band JSON messages:
//	             {"type":"resize","width":N,"height":N} resizes the
//	             PTY; {"type":"signal",...} forwards signals.
//
// The resize mechanism is the modern Incus control fd (not a separate
// REST POST). All Incus releases that support Interactive+WaitForWS
// (Incus 5.x + 6.x, the only versions Lahijan supports per ADR-0040)
// return the control secret. See ADR-0044 for the design rationale and
// the alternatives that were considered.
package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
)

// InteractiveExecParams controls an interactive exec session. The
// caller (compute.Service.OpenExecConsole) maps tenant + instance row
// to Project + Instance before invoking this; provider calls never
// carry tenant_id.
type InteractiveExecParams struct {
	// Project is the Incus project name (tenant -> project mapping
	// happens in the service layer).
	Project string

	// Instance is the instance name to exec inside.
	Instance string

	// Command is the argv to spawn as the shell. Defaults to
	// ["/bin/sh"] when nil/empty — matches `incus exec <name>` with
	// no command. Power users + SDKs can pass ["/bin/bash"] or
	// ["bash", "-c", "..."] via the WS endpoint's ?cmd= query.
	Command []string

	// Environment is the env vars to set (PATH, HOME, TERM, ...).
	// The handler always injects TERM=xterm so ANSI sequences render.
	Environment map[string]string

	// User / Group are the uid/gid to run as. Zero inherits the
	// instance's default user.
	User  int
	Group int

	// Cwd is the working directory inside the instance.
	Cwd string

	// Width / Height are the initial PTY dimensions (cols + rows).
	// Zero lets the daemon pick (usually 80x25); the first resize
	// control message from the browser overrides whatever the daemon
	// chose so this is mostly cosmetic.
	Width  int
	Height int
}

// InteractiveExecSession is the result of OpenInteractiveExec — the
// operation id + per-fd secrets the WebSocket bridge needs to dial
// each fd. The bridge (api/compute_console_handlers.go) dials stdin
// + stdout + control and pumps bytes both directions.
type InteractiveExecSession struct {
	// OperationID is the Incus async-operation UUID the daemon
	// created for this exec session.
	OperationID string

	// StdinSecret dials fd "0" — the browser's keystrokes flow here.
	StdinSecret string

	// StdoutSecret dials fd "1" — the PTY's combined stdout+stderr
	// (Interactive mode merges them; fd 2 is absent).
	StdoutSecret string

	// StderrSecret dials fd "2". Empty in Interactive mode (the
	// daemon combines stderr into stdout). Present when a future
	// non-interactive variant reuses this type.
	StderrSecret string

	// ControlSecret dials fd "control" — the JSON control channel
	// for resize + signal messages. Empty when the daemon does not
	// expose one (very old builds); the bridge degrades gracefully
	// by skipping resize forwarding.
	ControlSecret string
}

// OpenInteractiveExec opens an interactive (PTY-backed) exec session
// against a running instance and returns the operation id + per-fd
// secrets the caller needs to dial the operation's WebSocket(s).
//
// The instance MUST be running; the daemon refuses exec against a
// stopped instance (the compute service pre-validates this so the
// daemon's 400 never reaches the user, but the call is still safe to
// retry if the instance transitions between the pre-check and the
// call). Both containers and VMs are supported.
//
// The caller (the compute service in WS-32) is responsible for:
//   - emitting the audit row (action=
//     compute.instance.console.exec.connect) BEFORE this call so the
//     privileged action is observable even when the daemon is down,
//   - pumping bytes once it dials each per-fd WebSocket,
//   - tearing down the operation when the browser disconnects
//     (closing the stdin WS triggers Incus to terminate the exec).
//
// Reuses execOpen's plumbing (POST + Operation/Metadata decode) but
// flips Interactive=true + sets Width/Height. The metadata shape is
// the same ExecMetadata struct ({"fds": {...}}) but with 3-4 entries
// instead of 3.
func (p *Provider) OpenInteractiveExec(
	ctx context.Context,
	params InteractiveExecParams,
) (InteractiveExecSession, error) {
	ctx, span := startSpan(ctx, "instance.exec.interactive",
		projectAttr(params.Project))
	defer span.End()

	command := params.Command
	if len(command) == 0 {
		// Default shell matches `incus exec <name>` with no command.
		// Overridable via the WS endpoint's ?cmd= query for SDKs that
		// want bash, zsh, or a one-shot argv.
		command = []string{"/bin/sh"}
	}

	body := InstanceExecPost{
		Command:     command,
		Environment: params.Environment,
		WaitForWS:   true,
		Interactive: true,
		User:        params.User,
		Group:       params.Group,
		Cwd:         params.Cwd,
		Width:       params.Width,
		Height:      params.Height,
	}
	path := "instances/" + url.QueryEscape(params.Instance) +
		"/exec?project=" + url.QueryEscape(params.Project)
	raw, err := p.do(ctx, "POST", path, body)
	if err != nil {
		setStatus(span, err)
		return InteractiveExecSession{}, err
	}

	// The response is an async operation whose metadata carries the
	// per-fd secrets. Reuse the same Operation + ExecMetadata decode
	// as the one-shot path so the wire shape stays identical.
	var op Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		setStatus(span, err)
		return InteractiveExecSession{}, fmt.Errorf("incus: decode interactive exec operation: %w", err)
	}
	if op.ID == "" {
		err := fmt.Errorf("incus: interactive exec operation missing id: %s",
			truncate(string(raw), 256))
		setStatus(span, err)
		return InteractiveExecSession{}, err
	}
	var meta ExecMetadata
	if err := json.Unmarshal(op.Metadata, &meta); err != nil {
		setStatus(span, err)
		return InteractiveExecSession{}, fmt.Errorf("incus: decode interactive exec metadata: %w", err)
	}

	session := InteractiveExecSession{OperationID: op.ID}
	// Pull each well-known fd by name. Stdin (0) + stdout (1) are
	// required; control + stderr are optional.
	session.StdinSecret = meta.FDs["0"]
	session.StdoutSecret = meta.FDs["1"]
	session.StderrSecret = meta.FDs["2"]
	session.ControlSecret = meta.FDs["control"]

	if session.StdinSecret == "" {
		err := errors.New("incus: interactive exec metadata missing stdin (fd 0) secret")
		setStatus(span, err)
		return InteractiveExecSession{}, err
	}
	if session.StdoutSecret == "" {
		err := errors.New("incus: interactive exec metadata missing stdout (fd 1) secret")
		setStatus(span, err)
		return InteractiveExecSession{}, err
	}
	setStatus(span, nil)
	return session, nil
}

// DialExecFD dials a per-fd WebSocket for an Incus exec operation opened
// by OpenInteractiveExec. The returned *websocket.Conn speaks raw bytes
// for fd "0"/"1"/"2" or JSON control messages for fd "control".
//
// The caller owns the *websocket.Conn lifetime: it MUST close the conn
// when the browser disconnects or the context expires. The context is
// honoured only for the dial, not for the conn's lifetime.
//
// Reuses the same ws→wss scheme flip as the one-shot exec dial
// (execWebSocketURL) so a TLS-fronted daemon just works.
func (p *Provider) DialExecFD(ctx context.Context, opID, secret string) (*websocket.Conn, error) {
	if opID == "" || secret == "" {
		return nil, errors.New("incus: exec fd dial requires non-empty op id + secret")
	}
	wsURL := p.execWebSocketURL(opID, secret)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, http.Header{
		"User-Agent": {userAgent},
	})
	if err != nil {
		return nil, fmt.Errorf("incus: exec fd websocket: %w", err)
	}
	return conn, nil
}

// ExecControlResize is the JSON payload the Incus control fd accepts
// for window-resize. The bridge sends one of these every time the
// browser's ResizeObserver fires (debounced) so vim / htop / top
// render at the right dimensions.
//
// Wire format (documented in the OpenAPI description + ADR-0044):
//
//	{"type":"resize","width":<cols>,"height":<rows>}
//
// Width is columns; Height is rows. The names match the Incus daemon's
// own struct (lxd/instance_exec_control.go WindowsResize args) so the
// daemon does not need a translation layer.
type ExecControlResize struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ExecControlSignal is the JSON payload for signal forwarding. Not
// used by the dashboard today (the browser sends no signals); documented
// here so the protocol is exhaustive + future SDKs can send SIGINT/
// SIGTERM without breaking the bridge.
type ExecControlSignal struct {
	Type   string `json:"type"`
	Signal string `json:"signal"` // "SIGTERM", "SIGINT", "SIGHUP", ...
}

// WriteExecResize writes a resize control message to the supplied
// control-fd websocket. The caller owns the conn (typically opened by
// DialExecFD against the control secret and kept open for the duration
// of the session).
//
// Returns an error if the write fails; the bridge logs + continues
// (one missed resize is not worth tearing the session down for).
func WriteExecResize(conn *websocket.Conn, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		// The browser may send 0 during the initial layout pass; the
		// daemon rejects non-positive dimensions, so skip the write
		// rather than waste a round-trip on a guaranteed error.
		return nil
	}
	payload, err := json.Marshal(ExecControlResize{
		Type:   "resize",
		Width:  cols,
		Height: rows,
	})
	if err != nil {
		return fmt.Errorf("incus: marshal resize control: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("incus: write resize control: %w", err)
	}
	return nil
}
