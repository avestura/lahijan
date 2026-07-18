// Package fake: events.go implements the events websocket.
package fake

import (
	"net/http"
)

// handleEventsWS upgrades the connection to a websocket and registers it for
// broadcasts from EmitLifecycleEvent. The connection stays open until the
// client disconnects (which the read-loop here detects) or the server closes.
func (s *Server) handleEventsWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.WSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the error response.
	}
	s.eventsMu.Lock()
	s.eventsConns = append(s.eventsConns, conn)
	s.eventsMu.Unlock()

	defer func() {
		s.eventsMu.Lock()
		out := s.eventsConns[:0]
		for _, c := range s.eventsConns {
			if c != conn {
				out = append(out, c)
			}
		}
		s.eventsConns = out
		s.eventsMu.Unlock()
		_ = conn.Close()
	}()

	// Read loop. We never reply; this just keeps the connection alive and
	// detects client disconnects.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
