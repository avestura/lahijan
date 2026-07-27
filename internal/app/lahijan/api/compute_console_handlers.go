// Package api: compute_console_handlers.go implements the WS-32
// interactive exec WebSocket proxy. The browser-side xterm.js client
// opens a WebSocket to /api/v1/compute/instances/{instanceId}/console;
// this handler upgrades the request, asks the compute service to open
// an Incus interactive exec operation (validation + audit + Incus
// call), dials Incus' per-fd operation WebSockets (stdin, stdout,
// control), and pumps bytes both directions until either side closes.
//
// Per ADR-0044 + mirror of ADR-0031:
//   - Validation + audit happen BEFORE the WS upgrade so an invalid
//     request (missing instance, stopped instance, missing perm, daemon
//     down) gets a clean HTTP error envelope instead of "WS opened then
//     closed" UX.
//   - The audit row (action=compute.instance.console.exec.connect,
//     status=success) is emitted by the service before the Incus call.
//   - The route is registered via apigen (operationId=
//     openConsoleComputeInstance); the audit gate in router.go
//     dispatches it to RequirePerm(compute.instance.console.exec).
//   - The browser-side WS uses github.com/gofiber/websocket/v2; the
//     Incus-side WS uses gorilla/websocket (already a dep). The two
//     libraries share RFC 6455 opcodes so message types pass through
//     unchanged on the stdout path.
//
// Control channel: the browser may send JSON text frames of shape
//
//	{"type":"resize","cols":N,"rows":N}
//
// on the same WS as stdin. The handler sniffs each browser→Incus text
// frame, parses JSON-matching frames, translates the cols/rows naming
// (browser: cols/rows; Incus control fd: width/height), and forwards
// the resize to the Incus control fd. Non-JSON text frames are forwarded
// to Incus' stdin verbatim. The control fd is opened once at session
// start and held open for the duration of the browser WS.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gofiber/fiber/v2"
	fiberws "github.com/gofiber/websocket/v2"
	gorillaws "github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// consoleBridgePumpBufferSize is the cap on a single WS message we
// forward either direction. Interactive shell output is typically tiny
// (a few KB per redraw); 1 MiB is a generous upper bound that still
// protects the proxy from a misbehaving peer trying to OOM it. Mirrors
// the VNC bridge's limit.
const consoleBridgePumpBufferSize = 1 << 20 // 1 MiB

// browserResizeMessage is the JSON control envelope the browser sends
// on the same WS as stdin. The browser-side naming is cols/rows (the
// xterm.js convention); the Incus control fd wants width/height. The
// handler translates before forwarding.
//
// Wire format documented in api/openapi.yaml + ADR-0044:
//
//	{"type":"resize","cols":<columns>,"rows":<rows>}
type browserResizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// OpenConsoleComputeInstance handles
// GET /api/v1/compute/instances/{instanceId}/console (WebSocket
// upgrade). The flow mirrors OpenVncComputeInstance:
//  1. Pre-upgrade: validate request shape + open the Incus exec
//     session (the service validates the instance exists + is running;
//     emits the audit row; calls Incus).
//  2. Upgrade: hand off to fiberws for the WS handshake with the browser.
//  3. Post-upgrade: dial the Incus operation's per-fd WS for stdin +
//     stdout + control using the secrets from step 1.
//  4. Bridge: pump messages both directions until either side closes.
//     Browser→Incus text frames are sniffed for JSON resize messages;
//     matching frames are routed to the control fd, others go to stdin
//     verbatim.
//
// Pre-upgrade errors return the standard error envelope (no WS
// handshake). Post-upgrade errors close the WS with a close message;
// the xterm.js client surfaces the reason in its disconnect UI.
//
// The optional ?cmd=<argv> query parameter is URL-encoded argv
// (e.g. "?cmd=/bin/bash" or "?cmd=bash,-c,echo%20hi"). When empty the
// backend defaults to ["/bin/sh"] (matches `incus exec <name>`).
func (s *Server) OpenConsoleComputeInstance(
	c *fiber.Ctx,
	instanceID openapi_types.UUID,
	params apigen.OpenConsoleComputeInstanceParams,
) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	uid, ok := currentUserID(c)
	if !ok {
		return SendUnauthorized(c, i18n.T(c.UserContext(), "auth.err_unauthorized", nil))
	}
	tid, err := database.TenantFromContext(c.UserContext())
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "rbac.err_tenant_scope_required", nil), nil)
	}

	// Optional ?cmd=<argv> query. URL-decoded + comma-split so SDKs can
	// pick the shell. Empty/absent -> nil -> provider defaults to
	// ["/bin/sh"]. The dashboard always uses the default.
	command := parseConsoleCmdParams(params)

	// Pre-upgrade: validate + open the Incus session. A failure here
	// returns a clean HTTP envelope — no WS handshake reaches the
	// client for invalid requests (stopped instance, missing instance,
	// daemon down). The service emits the audit row as part of this
	// call (BEFORE the Incus call, mirror WS-24).
	session, err := s.computeSvc.OpenExecConsole(c.UserContext(), tid, uid, instanceID, command, 0, 0)
	if err != nil {
		return mapComputeError(c, err)
	}

	bridge := fiberws.New(func(conn *fiberws.Conn) {
		s.serveExecConsoleBridge(conn, session)
	})
	return bridge(c)
}

// serveExecConsoleBridge dials each Incus per-fd WS (stdin, stdout,
// control) using the secrets embedded in session, then pumps messages
// both directions until either side closes. The caller (fiberws)
// closes the browser-side conn when this returns; we close every
// Incus-side conn via defer.
//
// Lifecycle is governed by the conn lifetimes themselves: when one
// pump returns we close every other conn via closeAll so blocked
// ReadMessage calls unblock. Closing the stdin conn triggers Incus to
// terminate the exec (the documented "EOF on stdin ends the session"
// behaviour) which then closes stdout + control from the daemon side.
func (s *Server) serveExecConsoleBridge(conn *fiberws.Conn, session compute.ExecConsoleSession) {
	bg := context.Background()

	// Dial each per-fd WS. Use background context: the operation's
	// lifetime is independent of the original HTTP request (which has
	// been hijacked) and is bounded by the conn lifetimes.
	stdinConn, err := s.computeSvc.DialExecConsoleFD(bg, session, session.StdinSecret)
	if err != nil {
		s.logConsoleDialFailure(session, "stdin", err)
		_ = conn.WriteMessage(gorillaws.CloseMessage,
			gorillaws.FormatCloseMessage(
				gorillaws.CloseTryAgainLater,
				i18n.T(bg, "compute.err_exec_unavailable", nil),
			))
		_ = conn.Close()
		return
	}
	stdoutConn, err := s.computeSvc.DialExecConsoleFD(bg, session, session.StdoutSecret)
	if err != nil {
		s.logConsoleDialFailure(session, "stdout", err)
		_ = stdinConn.Close()
		_ = conn.WriteMessage(gorillaws.CloseMessage,
			gorillaws.FormatCloseMessage(
				gorillaws.CloseTryAgainLater,
				i18n.T(bg, "compute.err_exec_unavailable", nil),
			))
		_ = conn.Close()
		return
	}
	// The control fd is optional: very old Incus builds do not return
	// a "control" secret. When absent, the bridge degrades gracefully
	// by skipping resize forwarding (the shell still works; vim/htop
	// just render at the daemon's default PTY size).
	var controlConn *gorillaws.Conn
	if session.ControlSecret != "" {
		controlConn, err = s.computeSvc.DialExecConsoleFD(bg, session, session.ControlSecret)
		if err != nil {
			// Non-fatal: log + continue without resize support.
			s.logConsoleDialFailure(session, "control", err)
			controlConn = nil
		}
	}
	defer func() {
		_ = stdinConn.Close()
		_ = stdoutConn.Close()
		if controlConn != nil {
			_ = controlConn.Close()
		}
	}()

	// Coordinate shutdown: when one pump returns we close every other
	// side so blocked ReadMessage calls unblock.
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			_ = conn.Close()
			_ = stdinConn.Close()
			_ = stdoutConn.Close()
			if controlConn != nil {
				_ = controlConn.Close()
			}
		})
	}
	defer closeAll()

	// Browser -> Incus stdin (with resize sniffing on the control fd).
	go func() {
		pumpBrowserToIncusStdin(conn, stdinConn, controlConn)
		closeAll()
	}()

	// Incus stdout -> Browser (runs on the handler goroutine).
	pumpWebSocketBridge(stdoutConn, conn, consoleBridgePumpBufferSize)
	closeAll()
}

// logConsoleDialFailure emits the structured log line required by the
// backend AGENTS.md "Hard rules" section: every error catch-all MUST
// log the underlying error (with type name, op id, fd name) BEFORE
// returning the generic envelope. The dial failure path is the single
// most expensive debug path in the console bridge if it's silent.
func (s *Server) logConsoleDialFailure(session compute.ExecConsoleSession, fd string, err error) {
	slog.Default().Error(
		"compute: console bridge dial failed",
		"error", err.Error(),
		"error_type", fmt.Sprintf("%T", err),
		"operation_id", session.OperationID,
		"instance", session.Instance,
		"project", session.Project,
		"fd", fd,
	)
}

// pumpBrowserToIncusStdin reads messages from the browser's WS and
// forwards them to the Incus stdin WS. Text frames are sniffed for
// the resize control envelope; matching frames are routed to the
// control WS (when wired) instead of the stdin WS. Non-matching text
// frames + binary frames go to stdin verbatim.
//
// Errors are logged at Debug (they are expected on every clean close).
func pumpBrowserToIncusStdin(browser *fiberws.Conn, stdin *gorillaws.Conn, control *gorillaws.Conn) {
	for {
		msgType, data, err := browser.ReadMessage()
		if err != nil {
			if !isExpectedClose(err) {
				slog.Default().Debug("console bridge: browser read ended",
					"error", err.Error())
			}
			return
		}
		if len(data) > consoleBridgePumpBufferSize {
			slog.Default().Warn("console bridge: dropping oversized browser message",
				"size", len(data), "limit", consoleBridgePumpBufferSize)
			continue
		}
		// Sniff text frames for the resize control envelope. Binary
		// frames + non-resize text frames go to stdin verbatim — the
		// shell sees them exactly as the browser sent them.
		if msgType == gorillaws.TextMessage && control != nil {
			if handled := maybeHandleResizeControl(data, control); handled {
				continue
			}
		}
		if err := stdin.WriteMessage(msgType, data); err != nil {
			if !isExpectedClose(err) {
				slog.Default().Debug("console bridge: stdin write ended",
					"error", err.Error())
			}
			return
		}
	}
}

// maybeHandleResizeControl reports whether data is a JSON resize
// control envelope. When it is, the resize is forwarded to the Incus
// control fd (with the cols/rows → width/height translation) + the
// function returns true so the caller skips the stdin write. When it
// is not (parse error, missing fields, unknown type), the function
// returns false so the caller forwards the bytes to stdin verbatim —
// matching the principle "least surprise for non-control input".
func maybeHandleResizeControl(data []byte, control *gorillaws.Conn) bool {
	// Cheap pre-check: the resize envelope always starts with `{`.
	// Skip the JSON parse for the common case of raw shell input
	// (keystrokes, paste, etc.) so the hot path stays fast.
	if len(data) == 0 || data[0] != '{' {
		return false
	}
	var msg browserResizeMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return false
	}
	if msg.Type != "resize" {
		// Some other JSON control message we don't recognise yet
		// (signal forwarding, env-var updates, ...). Forward to stdin
		// verbatim — the shell will probably error on it, which is
		// the least-surprise behaviour for an unknown control frame.
		return false
	}
	// Translate browser (cols/rows) -> Incus control fd (width/height)
	// and forward. WriteExecResize skips non-positive dims internally
	// so a zero from a transient layout pass is a no-op.
	if err := incus.WriteExecResize(control, msg.Cols, msg.Rows); err != nil {
		slog.Default().Debug("console bridge: resize control write failed",
			"error", err.Error(),
			"cols", msg.Cols, "rows", msg.Rows)
	}
	return true
}

// parseConsoleCmdParams extracts the optional ?cmd=<argv> query
// parameter from the generated params struct. The value is a
// comma-separated argv (URL-encoded by the time it reaches the
// handler); empty or absent returns nil so the provider defaults to
// ["/bin/sh"].
//
// Example: "?cmd=/bin/bash" -> ["/bin/bash"]
//
//	"?cmd=bash,-c,echo%20hi" -> ["bash", "-c", "echo hi"]
func parseConsoleCmdParams(params apigen.OpenConsoleComputeInstanceParams) []string {
	if params.Cmd == nil || *params.Cmd == "" {
		return nil
	}
	parts := splitConsoleCmd(*params.Cmd)
	if len(parts) == 0 {
		return nil
	}
	return parts
}

// splitConsoleCmd splits the ?cmd= query value into an argv. Empty
// tokens (e.g. from "a,,b") are dropped — they have no meaning in an
// argv. A leading "/" is preserved so absolute paths work.
func splitConsoleCmd(s string) []string {
	out := make([]string, 0, 4)
	current := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ',' {
			if len(current) > 0 {
				out = append(out, string(current))
				current = current[:0]
			}
			continue
		}
		current = append(current, ch)
	}
	if len(current) > 0 {
		out = append(out, string(current))
	}
	return out
}
