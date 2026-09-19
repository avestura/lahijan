// Package fake: exec.go implements the exec websocket flow.
//
// The flow:
//  1. POST /1.0/instances/<name>/exec -> handler returns async operation
//     whose metadata carries per-fd websocket secrets.
//  2. Client (the driver) dials each per-fd websocket at
//     /1.0/operations/<op>/websocket?secret=<secret>. The fake upgrades
//     each dial and hands the connection to the goroutine waiting on that
//     (op, secret) tuple via the execAccept map.
//  3. The stdout goroutine runs the handler, writes its output to the
//     stdout websocket, then closes. The stderr goroutine just accepts +
//     closes (the default handler writes nothing on stderr). The stdin
//     goroutine reads until the client closes.
//  4. When all three pumps finish, the operation is marked complete with
//     exit code in its metadata so WaitOperation + execExitCode can read it.
package fake

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request, instance string) {
	project := queryProject(r)
	if r.Method != http.MethodPost {
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		return
	}
	var body incus.InstanceExecPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}

	s.mu.Lock()
	fp, ok := s.projects[project]
	if !ok {
		s.mu.Unlock()
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	if _, exists := fp.Instances[instance]; !exists {
		s.mu.Unlock()
		writeIncusError(w, http.StatusNotFound, "Instance %q not found", instance)
		return
	}
	handler := s.execHandler
	interactiveHandler := s.interactiveExecHandler
	s.mu.Unlock()

	// Interactive exec (WS-32): the daemon mints a different fd layout
	// (stdin + combined stdout + control) and treats the session as a
	// long-lived bidirectional PTY instead of a one-shot run + capture.
	// Dispatch to a dedicated handler so the WS-32 bridge can be tested
	// without disturbing the one-shot assertions.
	if body.Interactive {
		s.handleInteractiveExec(w, body, project, instance, interactiveHandler)
		return
	}

	stdinSecret := uuid.NewString()
	stdoutSecret := uuid.NewString()
	stderrSecret := uuid.NewString()
	opID := uuid.NewString()

	// Per the Incus REST API: POST /exec returns an async operation whose
	// metadata field is itself an Operation object carrying the per-fd
	// secrets nested one level deeper under "metadata.fds".
	opEnvelope := map[string]any{
		"id":          opID,
		"class":       "websocket",
		"status":      "Running",
		"status_code": http.StatusOK,
		"may_cancel":  true,
		"created_at":  time.Now().UTC(),
		"updated_at":  time.Now().UTC(),
		"metadata": map[string]any{
			"fds": map[string]string{
				"0": stdinSecret,
				"1": stdoutSecret,
				"2": stderrSecret,
			},
		},
	}
	envelopeJSON, _ := json.Marshal(opEnvelope)

	secretsJSON, _ := json.Marshal(map[string]any{
		"fds": map[string]string{
			"0": stdinSecret,
			"1": stdoutSecret,
			"2": stderrSecret,
		},
	})
	s.mu.Lock()
	s.operations[opID] = &fakeOperation{
		op: incus.Operation{
			ID:         opID,
			Class:      "websocket",
			Status:     "Running",
			StatusCode: http.StatusOK,
			MayCancel:  true,
			Metadata:   secretsJSON,
		},
		done: make(chan struct{}),
	}
	s.mu.Unlock()

	// Stdout pump: waits for driver to dial, runs handler, writes stdout,
	// closes. Signals doneWG when finished.
	doneWG := sync.WaitGroup{}
	doneWG.Add(2)
	go func() {
		defer doneWG.Done()
		conn := s.acceptExecWS(opID, stdoutSecret)
		if conn == nil {
			return
		}
		defer func() { _ = conn.Close() }()
		stdout, _, _ := handler(project, instance, body)
		if len(stdout) > 0 {
			_ = conn.WriteMessage(websocket.TextMessage, stdout)
		}
	}()
	// Stderr pump: just accept + close so the driver's stderr read loop
	// unblocks. The default handler emits nothing on stderr.
	go func() {
		defer doneWG.Done()
		conn := s.acceptExecWS(opID, stderrSecret)
		if conn == nil {
			return
		}
		defer func() { _ = conn.Close() }()
	}()
	// Stdin pump: read until the client closes (the driver writes its input
	// then sends a close frame). We do not consume the input here; the
	// handler ignores it.
	go func() {
		conn := s.acceptExecWS(opID, stdinSecret)
		if conn == nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// When the stdout + stderr pumps finish, mark the operation complete
	// with the exit code in the metadata.
	go func() {
		doneWG.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		fo, ok := s.operations[opID]
		if !ok {
			return
		}
		_, _, exit := handler(project, instance, body)
		exitMeta, _ := json.Marshal(map[string]any{
			"output": map[string]any{"return": exit},
			"fds": map[string]string{
				"0": stdinSecret, "1": stdoutSecret, "2": stderrSecret,
			},
		})
		fo.op.Metadata = exitMeta
		fo.op.Status = "Success"
		fo.op.StatusCode = 200
		select {
		case <-fo.done:
		default:
			close(fo.done)
		}
	}()

	writeIncusAsyncWithMeta(w, opID, envelopeJSON)
}

// acceptExecWS blocks until a client connects to the per-fd websocket route
// for the given (opID, secret). Returns nil on timeout (5 seconds) or if the
// server is shutting down.
func (s *Server) acceptExecWS(opID, secret string) *websocket.Conn {
	ch := make(chan *websocket.Conn, 1)
	key := acceptKey(opID, secret)
	s.execAcceptMu.Lock()
	s.execAccept[key] = ch
	s.execAcceptMu.Unlock()

	defer func() {
		s.execAcceptMu.Lock()
		delete(s.execAccept, key)
		s.execAcceptMu.Unlock()
	}()

	select {
	case conn := <-ch:
		return conn
	case <-time.After(5 * time.Second):
		return nil
	}
}

// handleExecWS is the websocket route the driver dials for an exec fd.
// The main handler routes /1.0/operations/<op>/websocket here via the
// operations dispatcher in operations.go.
func (s *Server) handleExecWS(w http.ResponseWriter, r *http.Request, opID string) {
	secret := r.URL.Query().Get("secret")
	conn, err := s.WSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.execAcceptMu.Lock()
	ch, ok := s.execAccept[acceptKey(opID, secret)]
	s.execAcceptMu.Unlock()
	if !ok {
		_ = conn.Close()
		return
	}
	select {
	case ch <- conn:
	default:
		_ = conn.Close()
	}
}

// acceptKey is the (opID, secret) tuple the websocket route matches against.
func acceptKey(opID, secret string) string { return opID + "|" + secret }

// handleInteractiveExec services an Interactive=true + WaitForWS=true exec
// POST (WS-32). The fake mints four per-fd secrets (stdin, stdout, stderr
// for completeness, + control), parks goroutines that:
//
//   - echo stdin bytes back onto stdout (a minimal PTY stand-in), and
//   - read + record the JSON control messages the bridge sends for resize
//     (asserted by the bridge unit test via ResizeMessages()).
//
// The handler owns the conn lifetime for each fd it parks; closing the
// stdin conn (the bridge does this on browser-disconnect) terminates the
// echo loop and lets the operation complete.
func (s *Server) handleInteractiveExec(
	w http.ResponseWriter,
	body incus.InstanceExecPost,
	_, instance string,
	handler func(conn *websocket.Conn, params incus.InstanceExecPost, resizes *[]incus.ExecControlResize),
) {
	dataSecret := uuid.NewString()
	controlSecret := uuid.NewString()
	opID := uuid.NewString()

	// Real Incus interactive exec (Interactive=true + WaitForWS=true)
	// exposes a SINGLE bidirectional data fd ("0" — the PTY master: the
	// client writes keystrokes to it AND reads output from it on the same
	// websocket) plus the "control" channel for resize/signal JSON. There
	// is no separate stdout ("1") or stderr ("2") fd in interactive mode;
	// the PTY merges them. The previous fake wrongly minted a separate
	// "1", which made every test pass while the real daemon returned only
	// "0" + "control" — so the driver's "fd 1 must exist" check 503'd
	// against real Incus. This layout now mirrors the daemon exactly.
	opEnvelope := map[string]any{
		"id":          opID,
		"class":       "websocket",
		"status":      "Running",
		"status_code": http.StatusOK,
		"may_cancel":  true,
		"created_at":  time.Now().UTC(),
		"updated_at":  time.Now().UTC(),
		"metadata": map[string]any{
			"fds": map[string]string{
				"0":       dataSecret,
				"control": controlSecret,
			},
		},
	}
	envelopeJSON, _ := json.Marshal(opEnvelope)

	secretsJSON, _ := json.Marshal(map[string]any{
		"fds": map[string]string{
			"0":       dataSecret,
			"control": controlSecret,
		},
	})
	s.mu.Lock()
	s.operations[opID] = &fakeOperation{
		op: incus.Operation{
			ID:         opID,
			Class:      "websocket",
			Status:     "Running",
			StatusCode: http.StatusOK,
			MayCancel:  true,
			Metadata:   secretsJSON,
		},
		done: make(chan struct{}),
	}
	if s.interactiveResizes == nil {
		s.interactiveResizes = make(map[string]*[]incus.ExecControlResize)
	}
	resizes := make([]incus.ExecControlResize, 0)
	s.interactiveResizes[opID] = &resizes
	s.mu.Unlock()

	// Data fd pump: accept the single bidirectional websocket ("0") and
	// hand it to the interactive handler. The handler owns the conn and
	// MUST close it. The default handler writes a banner + echoes every
	// input frame back (a minimal PTY round-trip stand-in) on this SAME
	// conn — reading keystrokes and writing output go over one websocket,
	// exactly like the real daemon's PTY master.
	go func() {
		conn := s.acceptExecWS(opID, dataSecret)
		if conn == nil {
			return
		}
		handler(conn, body, s.interactiveResizes[opID])
	}()

	// Control pump: accept + read JSON control messages. The fake
	// records resizes for later assertion + replies with a 200 ack
	// (matching the real daemon's "control fd is fire-and-forget"
	// behaviour — the daemon does not ack on success, but writing
	// nothing keeps the conn open for the next message).
	go func() {
		conn := s.acceptExecWS(opID, controlSecret)
		if conn == nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg incus.ExecControlResize
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg.Type == "resize" {
				s.mu.Lock()
				*s.interactiveResizes[opID] = append(*s.interactiveResizes[opID], msg)
				s.mu.Unlock()
			}
		}
	}()

	writeIncusAsyncWithMeta(w, opID, envelopeJSON)
}

// InteractiveResizes returns the recorded resize control messages for
// the given operation id. The bridge test uses this to assert the
// browser's resize control message reached the Incus control fd.
func (s *Server) InteractiveResizes(opID string) []incus.ExecControlResize {
	s.mu.Lock()
	defer s.mu.Unlock()
	ptr, ok := s.interactiveResizes[opID]
	if !ok || ptr == nil {
		return nil
	}
	out := make([]incus.ExecControlResize, len(*ptr))
	copy(out, *ptr)
	return out
}

// SetInteractiveExecHandler overrides the per-interactive-exec handler.
// The default echoes stdin bytes back to stdout (a minimal PTY round-trip
// stand-in). The handler owns the stdout conn's lifetime: it MUST close
// it before returning.
func (s *Server) SetInteractiveExecHandler(h func(conn *websocket.Conn, params incus.InstanceExecPost, resizes *[]incus.ExecControlResize)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interactiveExecHandler = h
}

// defaultInteractiveExecHandler is a minimal PTY stand-in for the single
// bidirectional data fd ("0") the real daemon exposes in interactive mode.
// It writes a banner, then echoes every input frame back onto the SAME conn
// so a bridge round-trip test sees its own keystrokes returned as output
// (mirroring how a real shell echoes typed chars + command output over the
// PTY master). The handler owns the conn and closes it when the read side
// ends (the bridge closes stdin on disconnect, which terminates this loop).
func defaultInteractiveExecHandler(conn *websocket.Conn, _ incus.InstanceExecPost, _ *[]incus.ExecControlResize) {
	defer func() { _ = conn.Close() }()
	_ = conn.WriteMessage(websocket.TextMessage, []byte("# interactive exec ready\r\n"))
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		// Echo the frame straight back (same type, same bytes) — a real
		// PTY echoes typed input and emits command output over this fd.
		if err := conn.WriteMessage(msgType, data); err != nil {
			return
		}
	}
}
