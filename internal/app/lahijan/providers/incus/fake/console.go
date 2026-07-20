// Package fake: console.go implements the Incus VM graphical console
// endpoint (WS-24). The flow mirrors exec (single fd instead of three):
//
//  1. POST /1.0/instances/<name>/console?type=vga -> handler returns an
//     async operation whose metadata carries the per-fd websocket secret.
//  2. Client (the driver) dials /1.0/operations/<op>/websocket?secret=<s>.
//     The fake upgrades the dial and hands the connection to the goroutine
//     parked on the (opID, secret) tuple via the shared execAccept map.
//  3. The parked goroutine invokes s.consoleHandler (default: echo bytes
//     back). The handler owns the conn's lifetime and MUST close it.
//
// The fake does NOT model the operation completing; the WS stays open until
// the handler closes it. Tests that want to assert on the operation's
// terminal state can do so via the standard operations API.
package fake

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
)

// handleConsole services POST /1.0/instances/<name>/console?type=vga. It
// validates the project + instance exist, mints an operation id + per-fd
// secret, parks a goroutine that waits for the driver to dial the WS route
// (handing off via the shared acceptExecWS mechanism), and returns the
// async operation envelope carrying the secret.
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request, instance string) {
	project := queryProject(r)
	if r.Method != http.MethodPost {
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		return
	}
	var body incus.InstanceConsolePost
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
	inst, exists := fp.Instances[instance]
	if !exists {
		s.mu.Unlock()
		writeIncusError(w, http.StatusNotFound, "Instance %q not found", instance)
		return
	}
	handler := s.consoleHandler
	s.mu.Unlock()

	// Refuse the VGA console for non-VM instances; matches the real
	// daemon's behaviour and lets tests assert the compute service rejects
	// containers before opening a session.
	if inst.Type != "" && inst.Type != "virtual-machine" {
		writeIncusError(w, http.StatusBadRequest,
			"Instance %q is not a virtual-machine; VGA console unavailable", instance)
		return
	}

	secret := uuid.NewString()
	opID := uuid.NewString()

	// The Incus REST API: POST /console returns an async operation whose
	// metadata field is itself an Operation object carrying the per-fd
	// secret nested one level deeper under "metadata.fds".
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
				"0": secret,
			},
		},
	}
	envelopeJSON, _ := json.Marshal(opEnvelope)

	secretsJSON, _ := json.Marshal(map[string]any{
		"fds": map[string]string{
			"0": secret,
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

	// Wait for the driver to dial the WS route, then hand the conn off to
	// the configured handler. The handler owns the conn's lifetime.
	go func() {
		conn := s.acceptExecWS(opID, secret)
		if conn == nil {
			return
		}
		handler(conn)
	}()

	writeIncusAsyncWithMeta(w, opID, envelopeJSON)
}
