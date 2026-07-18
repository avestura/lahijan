// Package fake: metadata.go implements the per-zone metadata handlers.
package fake

import (
	"net/http"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
)

// handleMetadata dispatches /zones/<id>/metadata[/<kind>].
func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request, zoneID string, rest []string) {
	if len(rest) == 0 || rest[0] == "" {
		switch r.Method {
		case http.MethodGet:
			s.handleMetadataList(w, r, zoneID)
		default:
			writePDNSError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		}
		return
	}
	kind := rest[0]
	switch r.Method {
	case http.MethodGet:
		s.handleMetadataGet(w, r, zoneID, kind)
	case http.MethodPut:
		s.handleMetadataPut(w, r, zoneID, kind)
	case http.MethodDelete:
		s.handleMetadataDelete(w, r, zoneID, kind)
	default:
		writePDNSError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleMetadataList(w http.ResponseWriter, _ *http.Request, zoneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fz, ok := s.zones[zoneID]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	out := make([]powerdns.Metadata, 0, len(fz.Metadata))
	for _, m := range fz.Metadata {
		out = append(out, *m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMetadataGet(w http.ResponseWriter, _ *http.Request, zoneID, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fz, ok := s.zones[zoneID]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	m, ok := fz.Metadata[kind]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "metadata %q not found", kind)
		return
	}
	writeJSON(w, http.StatusOK, *m)
}

func (s *Server) handleMetadataPut(w http.ResponseWriter, r *http.Request, zoneID, kind string) {
	var body powerdns.Metadata
	if err := decodeBody(r, &body); err != nil {
		writePDNSError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	if body.Kind == "" {
		body.Kind = kind
	}
	var ok bool
	var out powerdns.Metadata
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		fz, exists := s.zones[zoneID]
		if !exists {
			return
		}
		if len(body.Metadata) == 0 {
			// PDNS convention: empty value = delete the entry.
			delete(fz.Metadata, kind)
			out = body
			ok = true
			return
		}
		cp := body
		fz.Metadata[kind] = &cp
		out = cp
		ok = true
	}()
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	writeJSON(w, http.StatusOK, out)
	s.emit("dns.zone.metadata.updated", zoneID)
}

func (s *Server) handleMetadataDelete(w http.ResponseWriter, _ *http.Request, zoneID, kind string) {
	// deleted is one of: "ok", "no-zone", "no-meta".
	deleted := "no-zone"
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		fz, ok := s.zones[zoneID]
		if !ok {
			return
		}
		if _, ok := fz.Metadata[kind]; !ok {
			deleted = "no-meta"
			return
		}
		delete(fz.Metadata, kind)
		deleted = "ok"
	}()
	if deleted == "no-zone" {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	if deleted == "no-meta" {
		writePDNSError(w, http.StatusNotFound, "metadata %q not found", kind)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
	s.emit("dns.zone.metadata.deleted", zoneID)
}
