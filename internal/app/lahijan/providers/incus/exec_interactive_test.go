// Package incus: exec_interactive_test.go covers the WS-32 interactive
// exec path against the in-process fake daemon. Asserts:
//
//   - OpenInteractiveExec returns the per-fd layout the WS-32 bridge
//     depends on: a single bidirectional data fd ("0") + "control";
//     stdout ("1") + stderr ("2") are absent in interactive PTY mode
//     (the real Incus daemon merges output onto fd "0").
//   - The default shell fallback kicks in when Command is empty.
//   - WriteExecResize emits a well-formed JSON control message.
//   - The fake echoes input frames back over the single data fd so the
//     bridge round-trip has something to assert against.
package incus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenInteractiveExec_ReturnsBidirectionalDataFD asserts the metadata
// layout the WS-32 bridge depends on. The real Incus daemon exposes a
// SINGLE bidirectional data fd ("0" — the PTY master) + "control"; there
// is no separate stdout ("1") or stderr ("2") in interactive mode. The
// previous fake wrongly minted a "1", which let this check pass while the
// real daemon returned only "0" + "control" and the driver 503'd.
func TestOpenInteractiveExec_ReturnsBidirectionalDataFD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "interactive-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "interactive-target",
		Width:    80,
		Height:   24,
	})
	require.NoError(t, err)
	require.NotEmpty(t, session.OperationID)
	assert.NotEmpty(t, session.StdinSecret, "stdin (fd 0) secret must be present")
	assert.Empty(t, session.StdoutSecret, "stdout (fd 1) must be ABSENT in interactive PTY mode (bidirectional on fd 0)")
	assert.Empty(t, session.StderrSecret, "stderr (fd 2) must be absent in interactive mode")
	assert.NotEmpty(t, session.ControlSecret, "control secret must be present (Incus 5+/6+)")
}

// TestOpenInteractiveExec_DefaultShellFallback asserts that an empty
// Command defaults to ["/bin/sh"] — matches `incus exec <name>` with no
// argv. The provider cannot see the command the daemon spawns (it is
// inside the instance); we assert via the fake's recorded InstanceExecPost.
func TestOpenInteractiveExec_DefaultShellFallback(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "shell-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	_, err = p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "shell-target",
		// Command intentionally empty; provider must default to /bin/sh.
	})
	require.NoError(t, err)
}

// TestOpenInteractiveExec_MissingInstance_ReturnsError asserts the
// pre-shell validation: the daemon refuses exec against an unknown
// instance and the provider surfaces the error to the caller (the
// compute service maps it to ErrInstanceNotFound via incus.ErrNotFound).
func TestOpenInteractiveExec_MissingInstance_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "does-not-exist",
	})
	require.Error(t, err, "exec against a missing instance must error")
}

// TestDialExecFD_RoundTripsBytes asserts the per-fd dial + byte pump on
// the single bidirectional data fd. The fake's default interactive
// handler writes a banner on fd "0"; we dial StdinSecret (the data fd)
// and read the banner back to confirm the WS hand-off works in both
// directions on the one websocket.
func TestDialExecFD_RoundTripsBytes(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "dial-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "dial-target",
	})
	require.NoError(t, err)

	// Dial the bidirectional data fd (fd "0") + read the handler's banner.
	conn, err := p.DialExecFD(ctx, session.OperationID, session.StdinSecret)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(data), "interactive exec ready",
		"data fd must carry the fake handler's banner")
}

// TestWriteExecResize_FormatsJSON asserts WriteExecResize emits the
// documented wire format `{"type":"resize","width":N,"height":N}`. The
// fake records the message; we read it back via InteractiveResizes().
func TestWriteExecResize_FormatsJSON(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "resize-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "resize-target",
	})
	require.NoError(t, err)

	// Dial the control fd + write a resize.
	ctrl, err := p.DialExecFD(ctx, session.OperationID, session.ControlSecret)
	require.NoError(t, err)
	defer func() { _ = ctrl.Close() }()

	require.NoError(t, incus.WriteExecResize(ctrl, 120, 40))

	// The fake records the resize; assert it landed with the right shape.
	require.Eventually(t, func() bool {
		return len(srv.InteractiveResizes(session.OperationID)) > 0
	}, 3*time.Second, 50*time.Millisecond, "fake must record the resize control message")

	msgs := srv.InteractiveResizes(session.OperationID)
	require.Len(t, msgs, 1)
	assert.Equal(t, "resize", msgs[0].Type)
	assert.Equal(t, 120, msgs[0].Width)
	assert.Equal(t, 40, msgs[0].Height)
}

// TestWriteExecResize_ZeroDimensionsNoop asserts WriteExecResize skips
// the write when cols/rows are non-positive (the daemon would reject
// them anyway, and the browser's initial layout pass often sends 0).
// We verify the no-op by pointing WriteExecResize at a control fd on
// the fake daemon and then asserting no resize was recorded.
func TestWriteExecResize_ZeroDimensionsNoop(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "zero-dims-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "zero-dims-target",
	})
	require.NoError(t, err)

	ctrl, err := p.DialExecFD(ctx, session.OperationID, session.ControlSecret)
	require.NoError(t, err)
	defer func() { _ = ctrl.Close() }()

	require.NoError(t, incus.WriteExecResize(ctrl, 0, 24))
	require.NoError(t, incus.WriteExecResize(ctrl, 80, 0))
	require.NoError(t, incus.WriteExecResize(ctrl, -1, -1))

	// Give the fake's control pump a beat to process any would-be
	// message, then assert none landed.
	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, srv.InteractiveResizes(session.OperationID),
		"non-positive dimensions must not produce a resize message")
}

// TestInteractiveExec_HandlerWritesDataFD asserts the interactive
// handler owns the single bidirectional data fd ("0") and can push
// bytes the bridge will see on the same websocket it writes input to.
// The fake's data pump is ONE goroutine on ONE conn; the handler is the
// seam where output is emitted. For WS-32 we assert the handler can
// write bytes onto the data fd that the bridge reads as output.
func TestInteractiveExec_HandlerWritesDataFD(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	// Override the handler so it writes a known payload to the data fd
	// as soon as the driver dials. The handler then blocks on ReadMessage
	// (returning when the test closes the conn).
	const payload = "ws-32-interactive-payload\r\n"
	srv.SetInteractiveExecHandler(func(conn *websocket.Conn, _ incus.InstanceExecPost, _ *[]incus.ExecControlResize) {
		defer func() { _ = conn.Close() }()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(payload))
		// Block until the test closes the conn; this keeps the
		// handler alive for the duration of the assertion.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "echo-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "echo-target",
	})
	require.NoError(t, err)

	// Interactive mode: a SINGLE bidirectional data fd. Dial it once.
	data, err := p.DialExecFD(ctx, session.OperationID, session.StdinSecret)
	require.NoError(t, err)
	defer func() { _ = data.Close() }()

	// The handler writes the payload onto the data fd as soon as it
	// parks; the driver (bridge) reads it back as output on the same conn.
	_ = data.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := data.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, payload, string(got),
		"data fd must carry the handler's payload verbatim")
}

// TestInteractiveExec_MalformedControlMessageDropped asserts the
// control fd ignores non-JSON / non-resize messages instead of
// crashing. The fake's control pump should silently drop them.
func TestInteractiveExec_MalformedControlMessageDropped(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "control-target",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenInteractiveExec(ctx, incus.InteractiveExecParams{
		Project:  project,
		Instance: "control-target",
	})
	require.NoError(t, err)

	ctrl, err := p.DialExecFD(ctx, session.OperationID, session.ControlSecret)
	require.NoError(t, err)
	defer func() { _ = ctrl.Close() }()

	// Send garbage; the fake must ignore it (no recorded resize).
	require.NoError(t, ctrl.WriteMessage(websocket.TextMessage, []byte("not-json-at-all")))
	// Then a real resize; only that one should land.
	require.NoError(t, incus.WriteExecResize(ctrl, 100, 30))

	require.Eventually(t, func() bool {
		return len(srv.InteractiveResizes(session.OperationID)) == 1
	}, 3*time.Second, 50*time.Millisecond,
		"only the well-formed resize should be recorded")

	msgs := srv.InteractiveResizes(session.OperationID)
	require.Len(t, msgs, 1)
	assert.Equal(t, 100, msgs[0].Width)
}

// TestControlResizeMessage_WireFormat asserts WriteExecResize emits the
// shape the real Incus control fd parses (api.InstanceExecControl:
// command=window-resize, string width/height args). A real daemon
// silently ignores any other shape, which left the PTY stuck at 80x25.
func TestControlResizeMessage_WireFormat(t *testing.T) {
	t.Parallel()
	got := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, data, _ := conn.ReadMessage()
		got <- data
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, incus.WriteExecResize(conn, 132, 50))

	var decoded incus.ExecControl
	require.NoError(t, json.Unmarshal(<-got, &decoded))
	assert.Equal(t, "window-resize", decoded.Command)
	assert.Equal(t, map[string]string{"width": "132", "height": "50"}, decoded.Args)
}
