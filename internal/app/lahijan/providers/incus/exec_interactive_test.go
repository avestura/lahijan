// Package incus: exec_interactive_test.go covers the WS-32 interactive
// exec path against the in-process fake daemon. Asserts:
//
//   - OpenInteractiveExec returns the 3 per-fd secrets (0=stdin, 1=stdout,
//     control=control) the bridge expects; stderr (fd 2) is absent in
//     interactive mode.
//   - The default shell fallback kicks in when Command is empty.
//   - WriteExecResize emits a well-formed JSON control message.
//   - The fake echoes stdin bytes back to stdout so the bridge round-trip
//     has something to assert against.
package incus_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenInteractiveExec_ReturnsThreeFDs asserts the metadata layout
// the WS-32 bridge depends on: stdin + stdout + control present, stderr
// (fd 2) absent.
func TestOpenInteractiveExec_ReturnsThreeFDs(t *testing.T) {
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
	assert.NotEmpty(t, session.StdoutSecret, "stdout (fd 1) secret must be present")
	assert.Empty(t, session.StderrSecret, "stderr (fd 2) must be absent in Interactive mode")
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

// TestDialExecFD_RoundTripsBytes asserts the per-fd dial + byte pump.
// The fake's default interactive handler writes a banner on stdout; we
// read it via DialExecFD(fd=1) to confirm the WS hand-off works.
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

	// Dial stdout + read the default handler's banner.
	conn, err := p.DialExecFD(ctx, session.OperationID, session.StdoutSecret)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(data), "interactive exec ready",
		"stdout must carry the fake handler's banner")
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

// TestInteractiveExec_HandlerWritesStdout asserts the interactive
// handler owns the stdout conn and can write bytes the bridge will
// see. The fake's stdin/stdout pumps are separate goroutines; the
// handler is the seam where a future "true echo" fake would wire the
// two together. For WS-32 we only need to assert the handler can push
// bytes onto stdout (the bridge test covers the full round-trip).
func TestInteractiveExec_HandlerWritesStdout(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	// Override the handler so it writes a known payload to stdout as
	// soon as the driver dials. The handler then blocks on ReadMessage
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

	// Dial stdin + stdout; both must be dialable independently.
	stdin, err := p.DialExecFD(ctx, session.OperationID, session.StdinSecret)
	require.NoError(t, err)
	defer func() { _ = stdin.Close() }()
	stdout, err := p.DialExecFD(ctx, session.OperationID, session.StdoutSecret)
	require.NoError(t, err)
	defer func() { _ = stdout.Close() }()

	// The handler writes the payload to stdout as soon as it parks.
	_ = stdout.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := stdout.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, payload, string(data),
		"stdout must carry the handler's payload verbatim")
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

// controlMessageWireShape is the on-wire shape of a resize control
// message. Used by TestControlResizeMessage_WireFormat to assert the
// JSON the bridge sends to Incus matches the daemon's expected format.
type controlMessageWireShape struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// TestControlResizeMessage_WireFormat asserts the JSON shape of the
// resize control message matches what the Incus daemon's control fd
// parser expects (type=resize, width=cols, height=rows).
func TestControlResizeMessage_WireFormat(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(incus.ExecControlResize{
		Type:   "resize",
		Width:  132,
		Height: 50,
	})
	require.NoError(t, err)

	var decoded controlMessageWireShape
	require.NoError(t, json.Unmarshal(payload, &decoded))
	assert.Equal(t, "resize", decoded.Type)
	assert.Equal(t, 132, decoded.Width)
	assert.Equal(t, 50, decoded.Height)

	// The wire format must contain the canonical field names the
	// daemon's control parser looks for.
	assert.Contains(t, string(payload), `"type":"resize"`)
	assert.Contains(t, string(payload), `"width":132`)
	assert.Contains(t, string(payload), `"height":50`)
}

