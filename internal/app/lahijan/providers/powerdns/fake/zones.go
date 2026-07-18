// Package fake: zones.go implements the per-zone handler (GET/PUT/PATCH/DELETE)
// plus the cryptokeys + metadata sub-resources.
package fake

import (
	"net/http"
	"strings"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
)

// handleZoneSub dispatches the per-zone endpoints.
//
//	path: servers/localhost/zones/<id>[/<sub>[/<id>]]
func (s *Server) handleZoneSub(w http.ResponseWriter, r *http.Request, rest string) {
	// rest = "<id>" or "<id>/<sub>" or "<id>/<sub>/<subid>"
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		writePDNSError(w, http.StatusBadRequest, "missing zone id")
		return
	}
	zoneID := parts[0]
	if len(parts) == 1 {
		s.handleZone(w, r, zoneID)
		return
	}
	sub := parts[1]
	switch sub {
	case "cryptokeys":
		s.handleCryptoKeys(w, r, zoneID, parts[2:])
	case "metadata":
		s.handleMetadata(w, r, zoneID, parts[2:])
	default:
		writePDNSError(w, http.StatusNotFound, "unknown sub-resource %q", sub)
	}
}

// ---- zone CRUD ----

func (s *Server) handleZone(w http.ResponseWriter, r *http.Request, zoneID string) {
	switch r.Method {
	case http.MethodGet:
		s.handleZoneGet(w, r, zoneID)
	case http.MethodPatch:
		s.handleZonePatch(w, r, zoneID)
	case http.MethodDelete:
		s.handleZoneDelete(w, r, zoneID)
	default:
		writePDNSError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleZonesList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]powerdns.Zone, 0, len(s.zones))
	for _, fz := range s.zones {
		out = append(out, fz.Zone)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleZoneCreate(w http.ResponseWriter, r *http.Request) {
	var body powerdns.ZoneCreate
	if err := decodeBody(r, &body); err != nil {
		writePDNSError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	if body.Name == "" {
		writePDNSError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !strings.HasSuffix(body.Name, ".") {
		writePDNSError(w, http.StatusBadRequest, "name must end with a dot")
		return
	}
	kind := body.Kind
	if kind == "" {
		kind = "Native"
	}
	soa := body.SOAEditAPI
	if soa == "" {
		soa = "DEFAULT"
	}
	id := strings.ToLower(body.Name)
	var z powerdns.Zone
	var created bool
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, exists := s.zones[id]; exists {
			writePDNSError(w, http.StatusConflict, "zone %q already exists", id)
			return
		}
		rrsets := make(map[string]*powerdns.RRset, len(body.RRsets))
		for i := range body.RRsets {
			rr := body.RRsets[i]
			rrsets[rrsetKey(rr.Name, rr.Type)] = &rr
		}
		// Seed the SOA + NS records PDNS' real daemon would bootstrap.
		if _, ok := rrsets[rrsetKey(body.Name, "SOA")]; !ok && len(body.Nameservers) > 0 {
			ns := body.Nameservers[0]
			if !strings.HasSuffix(ns, ".") {
				ns += "."
			}
			soaRR := powerdns.RRset{
				Name: body.Name,
				Type: "SOA",
				TTL:  3600,
				Records: []powerdns.Record{{
					Content: ns + " hostmaster." + body.Name + " 1 10800 3600 604800 3600",
				}},
			}
			rrsets[rrsetKey(body.Name, "SOA")] = &soaRR
		}
		if _, ok := rrsets[rrsetKey(body.Name, "NS")]; !ok && len(body.Nameservers) > 0 {
			recs := make([]powerdns.Record, 0, len(body.Nameservers))
			for _, ns := range body.Nameservers {
				if !strings.HasSuffix(ns, ".") {
					ns += "."
				}
				recs = append(recs, powerdns.Record{Content: ns})
			}
			nsRR := powerdns.RRset{
				Name:    body.Name,
				Type:    "NS",
				TTL:     3600,
				Records: recs,
			}
			rrsets[rrsetKey(body.Name, "NS")] = &nsRR
		}
		z = powerdns.Zone{
			ID:         id,
			Name:       body.Name,
			Type:       kind,
			Kind:       kind,
			URL:        "/api/v1/servers/localhost/zones/" + id,
			Serial:     1,
			SOAEditAPI: soa,
			SOAEdit:    body.SOAEdit,
			Account:    body.Account,
		}
		s.zones[id] = &fakeZone{
			Zone:       z,
			RRsets:     rrsets,
			CryptoKeys: make(map[int64]*powerdns.CryptoKey),
			Metadata:   make(map[string]*powerdns.Metadata),
		}
		created = true
	}()
	if !created {
		return // The inner func already wrote the error response.
	}
	writeJSON(w, http.StatusCreated, z)
	s.emit("dns.zone.created", id)
}

func (s *Server) handleZoneGet(w http.ResponseWriter, r *http.Request, zoneID string) {
	withRRsets := r.URL.Query().Get("rrsets") == "true"
	s.mu.Lock()
	defer s.mu.Unlock()
	fz, ok := s.zones[zoneID]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	z := fz.Zone
	if withRRsets {
		z.RRsets = make([]powerdns.RRset, 0, len(fz.RRsets))
		for _, rr := range fz.RRsets {
			cp := *rr
			z.RRsets = append(z.RRsets, cp)
		}
	}
	writeJSON(w, http.StatusOK, z)
}

func (s *Server) handleZonePatch(w http.ResponseWriter, r *http.Request, zoneID string) {
	var body powerdns.ZoneUpdate
	if err := decodeBody(r, &body); err != nil {
		writePDNSError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	badType := ""
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		fz, ok := s.zones[zoneID]
		if !ok {
			writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
			return
		}
		if body.Account != "" {
			fz.Zone.Account = body.Account
		}
		if body.Kind != "" {
			fz.Zone.Kind = body.Kind
			fz.Zone.Type = body.Kind
		}
		if body.SOAEditAPI != "" {
			fz.Zone.SOAEditAPI = body.SOAEditAPI
		}
		if body.SOAEdit != "" {
			fz.Zone.SOAEdit = body.SOAEdit
		}
		// Apply RRset change set atomically (the fake has no real
		// transaction, but mutations are guarded by s.mu).
		for _, rr := range body.RRsets {
			key := rrsetKey(rr.Name, rr.Type)
			switch rr.Changetype {
			case "DELETE":
				delete(fz.RRsets, key)
			case "REPLACE", "":
				cp := rr
				fz.RRsets[key] = &cp
			default:
				badType = rr.Changetype
				return
			}
		}
		// Bump the SOA serial to mimic PDNS.
		fz.Zone.Serial++
	}()
	if badType != "" {
		writePDNSError(w, http.StatusBadRequest, "unknown changetype %q", badType)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
	s.emit("dns.zone.updated", zoneID)
}

func (s *Server) handleZoneDelete(w http.ResponseWriter, _ *http.Request, zoneID string) {
	found := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.zones[zoneID]; !ok {
			return false
		}
		delete(s.zones, zoneID)
		return true
	}()
	if !found {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
	s.emit("dns.zone.deleted", zoneID)
}

// rrsetKey builds the per-zone RRset storage key. PDNS allows the same name
// to carry multiple types; the (name,type) tuple is the unique key.
func rrsetKey(name, rtype string) string {
	return name + " " + rtype
}
