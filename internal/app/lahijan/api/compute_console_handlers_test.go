// Package api: compute_console_handlers_test.go locks in the two
// non-obvious WS-32 bridge invariants that are easy to silently revert
// and would re-break the interactive console against a real Incus
// daemon:
//
//  1. Browser input is ALWAYS forwarded to the Incus stdin fd as a
//     BINARY WebSocket frame, regardless of the browser's frame type.
//     Incus's interactive exec fd "0" silently drops TEXT frames (a
//     text-frame "echo X\n" is ignored; the same bytes as binary are
//     echoed + executed). xterm.js emits keystrokes as string data
//     (text frames), so the conversion is mandatory.
//
//  2. Resize control envelopes ({"type":"resize",...}) sent as text
//     frames are NOT forwarded to stdin — they are consumed by the
//     control-fd sniffer.
//
// The pump is exercised with a pair of gorilla/websocket test conns
// (no Fiber stack needed); the browser side is typed as wsMessageConn
// so a gorilla conn stands in for the production *fiberws.Conn.
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startWSPair stands up an httptest websocket server and returns the
// dialed client conn plus a channel that yields the upgraded server
// conn. The server handler hands its conn to the channel and then
// returns; ownership of the server conn transfers to the caller (the
// test / pump).
func startWSPair(t *testing.T) (*httptest.Server, *gorillaws.Conn, chan *gorillaws.Conn) {
	t.Helper()
	upgrader := gorillaws.Upgrader{}
	srvCh := make(chan *gorillaws.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srvCh <- c
	}))
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	client, _, err := gorillaws.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	return srv, client, srvCh
}

// TestPumpBrowserToIncusStdin_AlwaysBinaryOpcode is the regression test
// for the binary-frame invariant. A text-frame keystroke from the
// browser must arrive at the Incus stdin fd as a BinaryMessage (opcode
// 2), not a TextMessage (opcode 1).
func TestPumpBrowserToIncusStdin_AlwaysBinaryOpcode(t *testing.T) {
	t.Parallel()

	// "Incus stdin fd" server: records every received message's opcode
	// + payload so the test can assert the pump converted text→binary.
	_, stdinClient, stdinSrvCh := startWSPair(t)
	stdinSrv := <-stdinSrvCh
	defer func() { _ = stdinClient.Close() }()
	defer func() { _ = stdinSrv.Close() }()
	type recv struct {
		opcode int
		data   []byte
	}
	got := make(chan recv, 4)
	go func() {
		for {
			op, data, err := stdinSrv.ReadMessage()
			if err != nil {
				return
			}
			got <- recv{opcode: op, data: data}
		}
	}()

	// "Browser" pair: the server side feeds pumpBrowserToIncusStdin's
	// reader; the test writes on the client side.
	_, browserClient, browserSrvCh := startWSPair(t)
	defer func() { _ = browserClient.Close() }()
	browserSrv := <-browserSrvCh
	defer func() { _ = browserSrv.Close() }()

	// Run the pump: browser server conn -> stdin client conn (which
	// forwards to the Incus stdin server above). No control fd.
	done := make(chan struct{})
	go func() {
		pumpBrowserToIncusStdin(browserSrv, stdinClient, nil)
		close(done)
	}()

	// Send a TEXT frame from the browser — this is exactly what xterm.js
	// produces (onData -> sock.send(string)).
	require.NoError(t, browserClient.WriteMessage(gorillaws.TextMessage, []byte("echo hi\n")))

	select {
	case r := <-got:
		assert.Equal(t, gorillaws.BinaryMessage, r.opcode,
			"browser text input MUST reach Incus as a binary frame; a text frame is silently dropped by the daemon's interactive exec fd")
		assert.Equal(t, "echo hi\n", string(r.data),
			"payload bytes must be forwarded verbatim (only the opcode changes)")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the stdin fd to receive the browser frame")
	}

	// A binary browser frame must also arrive as binary (no downgrade).
	require.NoError(t, browserClient.WriteMessage(gorillaws.BinaryMessage, []byte("ls\r")))
	select {
	case r := <-got:
		assert.Equal(t, gorillaws.BinaryMessage, r.opcode)
		assert.Equal(t, "ls\r", string(r.data))
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the second stdin frame")
	}

	// Closing the browser client ends the pump's read loop cleanly.
	require.NoError(t, browserClient.Close())
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pump did not exit after the browser conn closed")
	}
}

// TestPumpBrowserToIncusStdin_ResizeGoesToControlNotStdin is the
// regression test for the second invariant: a {"type":"resize"} text
// envelope is consumed by the sniffer and forwarded to the CONTROL fd
// (translated cols/rows -> width/height), and must NOT also land on
// stdin. Leaking it to stdin would type the raw JSON into the user's
// shell on every terminal resize.
func TestPumpBrowserToIncusStdin_ResizeGoesToControlNotStdin(t *testing.T) {
	t.Parallel()

	_, stdinClient, stdinSrvCh := startWSPair(t)
	stdinSrv := <-stdinSrvCh
	defer func() { _ = stdinClient.Close() }()
	defer func() { _ = stdinSrv.Close() }()
	stdinGot := make(chan []byte, 4)
	go func() {
		for {
			_, data, err := stdinSrv.ReadMessage()
			if err != nil {
				return
			}
			stdinGot <- data
		}
	}()

	_, controlClient, controlSrvCh := startWSPair(t)
	controlSrv := <-controlSrvCh
	defer func() { _ = controlClient.Close() }()
	defer func() { _ = controlSrv.Close() }()
	controlGot := make(chan []byte, 4)
	go func() {
		for {
			_, data, err := controlSrv.ReadMessage()
			if err != nil {
				return
			}
			controlGot <- data
		}
	}()

	_, browserClient, browserSrvCh := startWSPair(t)
	defer func() { _ = browserClient.Close() }()
	browserSrv := <-browserSrvCh
	defer func() { _ = browserSrv.Close() }()

	go pumpBrowserToIncusStdin(browserSrv, stdinClient, controlClient)

	// xterm.js sends the resize envelope as a TEXT frame on the same
	// socket as keystrokes.
	require.NoError(t, browserClient.WriteMessage(gorillaws.TextMessage,
		[]byte(`{"type":"resize","cols":132,"rows":50}`)))

	select {
	case data := <-controlGot:
		// The control fd speaks Incus's width/height naming, not the
		// browser's cols/rows.
		assert.Contains(t, string(data), `"width":132`)
		assert.Contains(t, string(data), `"height":50`)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the resize on the control fd")
	}

	// Follow with an ordinary keystroke: it must reach stdin. Because
	// the pump is strictly sequential, receiving this proves the resize
	// frame ahead of it was NOT also written to stdin.
	require.NoError(t, browserClient.WriteMessage(gorillaws.TextMessage, []byte("whoami\n")))
	select {
	case data := <-stdinGot:
		assert.Equal(t, "whoami\n", string(data),
			"the resize envelope must not be forwarded to stdin; the first stdin frame must be the keystroke that followed it")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the keystroke on the stdin fd")
	}
}

// TestPumpBrowserToIncusStdin_UnknownJSONGoesToStdin pins the
// documented fall-through: a JSON text frame that is NOT a resize
// envelope is forwarded to stdin verbatim rather than swallowed, so an
// unrecognised control message surfaces in the shell instead of
// vanishing silently.
func TestPumpBrowserToIncusStdin_UnknownJSONGoesToStdin(t *testing.T) {
	t.Parallel()

	_, stdinClient, stdinSrvCh := startWSPair(t)
	stdinSrv := <-stdinSrvCh
	defer func() { _ = stdinClient.Close() }()
	defer func() { _ = stdinSrv.Close() }()
	stdinGot := make(chan []byte, 4)
	go func() {
		for {
			_, data, err := stdinSrv.ReadMessage()
			if err != nil {
				return
			}
			stdinGot <- data
		}
	}()

	_, controlClient, controlSrvCh := startWSPair(t)
	controlSrv := <-controlSrvCh
	defer func() { _ = controlClient.Close() }()
	defer func() { _ = controlSrv.Close() }()

	_, browserClient, browserSrvCh := startWSPair(t)
	defer func() { _ = browserClient.Close() }()
	browserSrv := <-browserSrvCh
	defer func() { _ = browserSrv.Close() }()

	go pumpBrowserToIncusStdin(browserSrv, stdinClient, controlClient)

	const payload = `{"type":"signal","signal":9}`
	require.NoError(t, browserClient.WriteMessage(gorillaws.TextMessage, []byte(payload)))
	select {
	case data := <-stdinGot:
		assert.Equal(t, payload, string(data),
			"an unrecognised JSON control frame must fall through to stdin verbatim")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the unknown-JSON frame on the stdin fd")
	}
}
