// Package api: compute_vnc_handlers.go implements the WS-24 noVNC WebSocket
// proxy. The browser-side noVNC client opens a WebSocket to
// /api/v1/compute/instances/{instanceId}/vnc; this handler upgrades the
// request, asks the compute service to open an Incus VNC operation
// (validation + audit + Incus call), dials Incus' per-fd operation
// WebSocket, and pumps RFB bytes both directions until either side closes.
//
// Per ADR-0031:
//   - Validation + audit happen BEFORE the WS upgrade so an invalid request
//     (container instance, stopped VM, missing instance) gets a clean HTTP
//     error envelope instead of a "WS opened then closed" UX.
//   - The audit row (action=compute.instance.console.vnc.connect,
//     status=success) is emitted by the service before the Incus call.
//   - The route is registered via apigen (operationId=openVncComputeInstance);
//     the audit gate in router.go dispatches it to
//     RequirePerm(compute.instance.console.vnc).
//   - The browser-side WS uses github.com/gofiber/websocket/v2 (official
//     Fiber companion, MIT). The Incus-side WS uses gorilla/websocket (BSD-2,
//     already a dep). The two libraries share RFC 6455 opcode values so the
//     bridge is a pair of message pumps.
package api

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	fiberws "github.com/gofiber/websocket/v2"
	gorillaws "github.com/gorilla/websocket"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// vncBridgePumpBufferSize is the cap on a single WS message we forward
// either direction. RFB frames are typically tiny (a few KB max); 1 MiB
// is a generous upper bound that still protects the proxy from a
// misbehaving peer trying to OOM it.
const vncBridgePumpBufferSize = 1 << 20 // 1 MiB

// OpenVncComputeInstance handles GET /api/v1/compute/instances/{instanceId}/vnc
// (WebSocket upgrade). The flow is:
//  1. Pre-upgrade: validate request shape + open the Incus console session
//     (the service validates the instance exists, is a VM, and is running;
//     emits the audit row; calls Incus).
//  2. Upgrade: hand off to fiberws for the WS handshake with the browser.
//  3. Post-upgrade: dial the Incus operation's per-fd WS using the secret
//     from step 1.
//  4. Bridge: pump messages both directions until either side closes.
//
// Pre-upgrade errors return the standard error envelope (no WS handshake).
// Post-upgrade errors close the WS with a close message; the noVNC client
// surfaces the reason in its disconnect UI.
func (s *Server) OpenVncComputeInstance(c *fiber.Ctx, instanceID openapi_types.UUID) error {
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

	// Pre-upgrade: validate + open the Incus session. A failure here
	// returns a clean HTTP envelope — no WS handshake reaches the client
	// for invalid requests (container instance, stopped VM, missing
	// instance, daemon down). The service emits the audit row as part of
	// this call.
	session, err := s.computeSvc.OpenVNCConsole(c.UserContext(), tid, uid, instanceID)
	if err != nil {
		return mapComputeError(c, err)
	}

	// fibre fiberws handler: the upgrade happens when bridge(c) is called
	// below. If the upgrade fails (e.g. the client didn't send the WS
	// headers), fibre fiberws sets 426 Upgrade Required and returns.
	bridge := fiberws.New(func(conn *fiberws.Conn) {
		s.serveVNCBridge(conn, session)
	})
	return bridge(c)
}

// serveVNCBridge dials the Incus operation's per-fd WS using the secret
// embedded in session, then pumps messages both directions until either
// side closes. The caller (fibre fiberws) closes the browser-side conn
// when this returns; we close the Incus-side conn via defer.
//
// The bridge uses context.Background() for the Incus dial because the
// browser's request context is already done by the time the WS handler
// runs (the HTTP request has been hijacked). Lifecycle is governed by the
// two conns themselves: when one closes, the pump reading from it returns,
// which triggers the other pump to close its write side via closeConn.
func (s *Server) serveVNCBridge(conn *fiberws.Conn, session compute.VNCConsoleSession) {
	// Dial Incus' per-fd WS. Use background context: the operation's
	// lifetime is independent of the original HTTP request (which has
	// been hijacked) and is bounded by the conns' lifetimes.
	incusConn, err := s.computeSvc.DialVNCConsole(context.Background(), session)
	if err != nil {
		// Best-effort: tell the browser why we are closing.
		_ = conn.WriteMessage(gorillaws.CloseMessage,
			gorillaws.FormatCloseMessage(
				gorillaws.CloseTryAgainLater,
				i18n.T(context.Background(), "compute.err_vnc_unavailable", nil),
			))
		_ = conn.Close()
		return
	}
	defer func() { _ = incusConn.Close() }()

	// Coordinate shutdown: when one pump returns we close the OTHER
	// side's write end so its blocked ReadMessage returns. The first
	// close wins; subsequent calls are no-ops on a *websocket.Conn.
	done := make(chan struct{}, 2)
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = conn.Close()
			_ = incusConn.Close()
			close(done)
		})
	}
	defer closeBoth()

	// Browser -> Incus.
	go func() {
		pumpWebSocketBridge(conn, incusConn, vncBridgePumpBufferSize)
		closeBoth()
	}()

	// Incus -> Browser (runs on the handler goroutine; the caller closes
	// the conn when we return).
	pumpWebSocketBridge(incusConn, conn, vncBridgePumpBufferSize)
	closeBoth()
}

// pumpWebSocketBridge reads messages from src and writes them verbatim to
// dst until either side closes or errors. Both src and dst must support
// the gorilla/fasthttp ReadMessage/WriteMessage signature; the standard
// RFC 6455 opcodes (1=Text, 2=Binary) pass through unchanged between the
// two libraries.
//
// Errors are logged at Debug (they are expected on every clean close).
func pumpWebSocketBridge(src, dst wsMessageConn, maxBytes int) {
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			// io.EOF, websocket close, or read deadline: the other
			// side initiated the close. Stop pumping.
			if !isExpectedClose(err) {
				slog.Default().Debug("vnc bridge: src read ended",
					"error", err.Error())
			}
			return
		}
		if len(data) > maxBytes {
			slog.Default().Warn("vnc bridge: dropping oversized message",
				"size", len(data), "limit", maxBytes)
			continue
		}
		if err := dst.WriteMessage(msgType, data); err != nil {
			if !isExpectedClose(err) {
				slog.Default().Debug("vnc bridge: dst write ended",
					"error", err.Error())
			}
			return
		}
	}
}

// wsMessageConn is the narrow shape both gorilla/websocket and
// fasthttp/websocket satisfy: ReadMessage returns (type, bytes, err) and
// WriteMessage takes (type, bytes) and returns err. Defining it here (not
// in a shared package) keeps the bridge local to the VNC handler.
type wsMessageConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	Close() error
}

// isExpectedClose reports whether err is one of the benign close causes
// (clean close, going-away, normal closure) that should NOT be logged at
// Warn. Both gorilla and fasthttp websocket expose IsCloseError + a set
// of close-code constants; we use gorilla's here because the package
// already imports it. fasthttp/websocket uses the same numeric codes.
func isExpectedClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, gorillaws.ErrCloseSent) {
		return true
	}
	if gorillaws.IsCloseError(err,
		gorillaws.CloseNormalClosure,
		gorillaws.CloseGoingAway,
		gorillaws.CloseNoStatusReceived,
	) {
		return true
	}
	// net.OpError on read of a closed TCP conn (gorilla + fasthttp both
	// surface this shape on the reader side after Close()).
	var ce *gorillaws.CloseError
	return errors.As(err, &ce)
}

// Compile-time assertion that *fiberws.Conn and *gorillaws.Conn both
// satisfy wsMessageConn. (The fasthttp websocket.Conn's ReadMessage/
// WriteMessage/Close methods are value receivers, so *Conn satisfies.)
var (
	_ wsMessageConn = (*fiberws.Conn)(nil)
	_ wsMessageConn = (*gorillaws.Conn)(nil)
)
