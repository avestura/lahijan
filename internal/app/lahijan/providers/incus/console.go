// Package incus: console.go wraps the Incus VM graphical console endpoint
// (WS-24). The flow is the same operation-secret bootstrap as exec (see
// exec.go) but the per-fd WebSocket carries a single binary RFB (VNC)
// stream instead of three fd-specific byte streams:
//
//  1. POST /1.0/instances/<name>/console?type=vga. The daemon responds
//     with an async operation whose metadata carries the single per-fd
//     websocket secret (fds.0).
//  2. The caller dials /1.0/operations/<op-uuid>/websocket?secret=<secret>
//     and pumps RFB bytes both directions until either side closes.
//
// WS-14 ships xterm.js over exec (interactive command stream); WS-24 ships
// noVNC over console (graphical framebuffer + keyboard/pointer). The two
// never share a WebSocket: the browser side speaks two different protocols.
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

// ConsoleType is the kind of console Incus should attach to.
type ConsoleType string

const (
	// ConsoleTypeVGA is the graphical VGA console. VM-only; the daemon
	// refuses it for containers. This is the noVNC target.
	ConsoleTypeVGA ConsoleType = "vga"

	// ConsoleTypeConsole is the serial text console. Both containers and
	// VMs support it; Lahijan uses xterm.js+exec for text consoles (WS-14),
	// so this constant exists for completeness rather than active use.
	ConsoleTypeConsole ConsoleType = "console"
)

// InstanceConsolePost is the body of POST /1.0/instances/<name>/console.
// All fields are optional: the daemon treats an empty body as "vga, default
// size" which is what the noVNC client expects (it negotiates its own
// framebuffer dimensions over RFB).
type InstanceConsolePost struct {
	// Type selects the console backend. Empty defaults to "console" on the
	// daemon side; Lahijan callers MUST pass ConsoleTypeVGA explicitly for
	// noVNC.
	Type ConsoleType `json:"type,omitempty"`

	// Width / Height are the initial framebuffer dimensions. Zero lets the
	// daemon pick; noVNC re-negotiates via SetDesktopSize on connect.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// ConsoleSession is the result of OpenVNCConsole — the caller dials the
// Incus operation's WebSocket using OperationID + Secret.
type ConsoleSession struct {
	// OperationID is the Incus async-operation UUID the daemon created for
	// this console session. The caller forwards it to the operation
	// websocket URL.
	OperationID string

	// Secret is the single per-fd websocket secret. The caller dials
	// /1.0/operations/<op>/websocket?secret=<secret> and pumps RFB bytes.
	Secret string
}

// OpenVNCConsole opens a graphical (VGA) console against a running virtual
// machine and returns the operation id + per-fd secret the caller needs to
// dial the operation's WebSocket. The caller (the compute service in
// WS-24) is responsible for emitting the audit row before invoking this
// method, and for pumping the RFB bytes once it dials.
//
// The instance MUST be a virtual machine and MUST be running; the daemon
// returns an Incus API error otherwise (mapped through the standard error
// envelope by the caller). containers do not get a VGA console.
//
// Reuses the same operation-secret bootstrap as exec: the response is an
// async Operation whose metadata carries {"fds": {"0": "<secret>"}}.
// Unlike exec there is only one fd (no stdin/stdout/stderr split).
func (p *Provider) OpenVNCConsole(
	ctx context.Context,
	project, instance string,
) (ConsoleSession, error) {
	ctx, span := startSpan(ctx, "instance.console.vnc",
		projectAttr(project))
	defer span.End()

	body := InstanceConsolePost{Type: ConsoleTypeVGA}
	path := "instances/" + url.QueryEscape(instance) +
		"/console?project=" + url.QueryEscape(project)
	raw, err := p.do(ctx, "POST", path, body)
	if err != nil {
		setStatus(span, err)
		return ConsoleSession{}, err
	}

	// console returns an async operation whose metadata carries the per-fd
	// secret. Same shape as exec ({"fds": {"0": "<secret>"}}) but with a
	// single fd instead of three.
	var op Operation
	if err := json.Unmarshal(raw, &op); err != nil {
		setStatus(span, err)
		return ConsoleSession{}, fmt.Errorf("incus: decode console operation: %w", err)
	}
	if op.ID == "" {
		err := fmt.Errorf("incus: console operation missing id: %s", truncate(string(raw), 256))
		setStatus(span, err)
		return ConsoleSession{}, err
	}
	var meta ExecMetadata
	if err := json.Unmarshal(op.Metadata, &meta); err != nil {
		setStatus(span, err)
		return ConsoleSession{}, fmt.Errorf("incus: decode console metadata: %w", err)
	}
	secret, ok := meta.FDs["0"]
	if !ok || secret == "" {
		err := errors.New("incus: console operation missing fd 0 secret")
		setStatus(span, err)
		return ConsoleSession{}, err
	}
	setStatus(span, nil)
	return ConsoleSession{OperationID: op.ID, Secret: secret}, nil
}

// DialVNCConsole dials the per-fd WebSocket for an Incus console operation
// opened by OpenVNCConsole. The returned *websocket.Conn speaks raw RFB
// (VNC) bytes both directions; the caller (the WebSocket proxy handler in
// WS-24) bridges it to the browser's noVNC client.
//
// The caller owns the *websocket.Conn lifetime: it MUST close the conn
// when the browser disconnects or the context expires. The context is
// honoured only for the dial, not for the conn's lifetime.
//
// Reuses the same ws→wss scheme flip as exec (execWebSocketURL).
func (p *Provider) DialVNCConsole(ctx context.Context, opID, secret string) (*websocket.Conn, error) {
	if opID == "" || secret == "" {
		return nil, errors.New("incus: console dial requires non-empty op id + secret")
	}
	wsURL := p.execWebSocketURL(opID, secret)
	conn, _, err := p.wsDialer.DialContext(ctx, wsURL, http.Header{
		"User-Agent": {userAgent},
	})
	if err != nil {
		return nil, fmt.Errorf("incus: console websocket: %w", err)
	}
	return conn, nil
}
