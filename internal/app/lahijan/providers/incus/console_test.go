// Package incus_test: console_test.go covers the WS-24 VM graphical console
// flow. The test opens a VNC console session against the fake daemon, dials
// the per-fd WebSocket, and round-trips RFB-style bytes both directions.
package incus_test

import (
	"context"
	"testing"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenVNCConsole_ReturnsSessionForVM(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	// Create a VM target instance for the console to attach to.
	_, err := p.CreateInstance(ctx, incus.CreateInstanceParams{
		Project: project,
		Name:    "vm-target",
		Type:    "virtual-machine",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenVNCConsole(ctx, project, "vm-target")
	require.NoError(t, err)
	require.NotEmpty(t, session.OperationID, "operation id must be populated")
	require.NotEmpty(t, session.Secret, "per-fd secret must be populated")
}

func TestOpenVNCConsole_MissingInstance_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tenantID := uuid.New()
	require.NoError(t, p.EnsureProject(ctx, tenantID))
	project := p.ProjectName(tenantID)

	_, err := p.OpenVNCConsole(ctx, project, "does-not-exist")
	require.Error(t, err, "console against a missing instance must error")
}

func TestDialVNCConsole_RoundTripsBytes(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	// Custom handler: announce a fake RFB version string on connect, then
	// echo every received byte back. This stands in for a real VNC server
	// negotiating the protocol handshake.
	rfbAnnounce := []byte("RFB 003.008\n")
	srv.SetConsoleHandler(func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		if err := conn.WriteMessage(websocket.BinaryMessage, rfbAnnounce); err != nil {
			return
		}
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(msgType, data); err != nil {
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
		Name:    "vm-rfb",
		Type:    "virtual-machine",
		Source:  incus.InstanceSource{Type: "image", Alias: "ubuntu/24.04"},
	})
	require.NoError(t, err)

	session, err := p.OpenVNCConsole(ctx, project, "vm-rfb")
	require.NoError(t, err)

	conn, err := p.DialVNCConsole(ctx, session.OperationID, session.Secret)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Set a read deadline so the test fails fast instead of hanging.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	// First message: the fake's RFB announce.
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, rfbAnnounce, data, "must receive the fake's RFB announce")

	// Echo round-trip: client writes, fake echoes back.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("hello-vnc")))
	_, echo, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, []byte("hello-vnc"), echo, "fake must echo the bytes we sent")
}

func TestDialVNCConsole_EmptyArgs_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := newFakeWithDefaults(t)
	p := connectProvider(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := p.DialVNCConsole(ctx, "", "whatever")
	require.Error(t, err, "empty op id must error before dial")

	_, err = p.DialVNCConsole(ctx, "op", "")
	require.Error(t, err, "empty secret must error before dial")
}
