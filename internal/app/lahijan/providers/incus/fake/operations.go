// Package fake: operations.go implements the async operations surface.
package fake

import (
	"net/http"
	"strings"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// registerOp stores a fake operation under opID. If onRun is non-nil it is
// invoked immediately to model the async side-effect completing before the
// client polls Wait. Caller holds s.mu.
func (s *Server) registerOp(opID string, onRun func()) {
	op := incus.Operation{
		ID:         opID,
		Class:      "task",
		Status:     "Running",
		StatusCode: http.StatusOK,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		MayCancel:  true,
	}
	fo := &fakeOperation{op: op, done: make(chan struct{})}
	s.operations[opID] = fo
	if onRun != nil {
		onRun()
	}
	// Auto-complete the operation immediately so WaitOperation returns
	// instantly. Tests that want to assert on "still running" can wrap
	// the register path later (none do today).
	close(fo.done)
	fo.op.Status = "Success"
	fo.op.StatusCode = 200
}

func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request, rest string) {
	// Dispatch the exec websocket: /1.0/operations/<op>/websocket?secret=<s>.
	if strings.HasSuffix(rest, "/websocket") {
		opID := strings.TrimSuffix(rest, "/websocket")
		s.handleExecWS(w, r, opID)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	opID := parts[0]
	if opID == "" {
		writeIncusError(w, http.StatusNotFound, "missing op id")
		return
	}
	s.mu.Lock()
	fo, ok := s.operations[opID]
	s.mu.Unlock()
	if !ok {
		writeIncusError(w, http.StatusNotFound, "operation %q not found", opID)
		return
	}
	if len(parts) == 2 && parts[1] == "wait" {
		// WaitOperation: block until the op completes (already closed in
		// registerOp), then return the final state.
		select {
		case <-fo.done:
		case <-r.Context().Done():
			writeIncusError(w, http.StatusRequestTimeout, "wait cancelled")
			return
		}
		writeIncusResult(w, http.StatusOK, fo.op)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeIncusResult(w, http.StatusOK, fo.op)
	case http.MethodDelete:
		fo.op.Status = "Cancelled"
		fo.op.StatusCode = 200
		writeIncusResult(w, http.StatusOK, fo.op)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}
