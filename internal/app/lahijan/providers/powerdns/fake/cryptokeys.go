// Package fake: cryptokeys.go implements the per-zone DNSSEC key handlers.
package fake

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
)

// handleCryptoKeys dispatches /zones/<id>/cryptokeys[/<keyid>].
func (s *Server) handleCryptoKeys(w http.ResponseWriter, r *http.Request, zoneID string, rest []string) {
	// rest is the trailing path after "cryptokeys/"; empty for the
	// collection. Elements are: "" (collection) or "<keyid>".
	if len(rest) == 0 || rest[0] == "" {
		switch r.Method {
		case http.MethodGet:
			s.handleCryptoKeysList(w, r, zoneID)
		case http.MethodPost:
			s.handleCryptoKeyCreate(w, r, zoneID)
		default:
			writePDNSError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		}
		return
	}
	keyID, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		writePDNSError(w, http.StatusBadRequest, "invalid key id %q", rest[0])
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleCryptoKeyGet(w, r, zoneID, keyID)
	case http.MethodPut:
		s.handleCryptoKeyPut(w, r, zoneID, keyID)
	case http.MethodDelete:
		s.handleCryptoKeyDelete(w, r, zoneID, keyID)
	default:
		writePDNSError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleCryptoKeysList(w http.ResponseWriter, _ *http.Request, zoneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fz, ok := s.zones[zoneID]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	out := make([]powerdns.CryptoKey, 0, len(fz.CryptoKeys))
	for _, k := range fz.CryptoKeys {
		out = append(out, *k)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCryptoKeyCreate(w http.ResponseWriter, r *http.Request, zoneID string) {
	var body powerdns.CryptoKeyCreate
	if err := decodeBody(r, &body); err != nil {
		writePDNSError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	if body.KeyType == "" {
		body.KeyType = "ksk"
	}
	var key powerdns.CryptoKey
	var ok bool
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		fz, exists := s.zones[zoneID]
		if !exists {
			return
		}
		fz.nextKeyID++
		id := fz.nextKeyID
		key = powerdns.CryptoKey{
			ID:        id,
			KeyType:   body.KeyType,
			Active:    body.Active,
			Bits:      body.Bits,
			Published: body.Published,
			Algorithm: body.Algorithm,
			DNSsec:    true,
		}
		if key.Bits == 0 {
			key.Bits = 256
		}
		if key.Algorithm == "" {
			key.Algorithm = "ecdsaP256SHA256"
		}
		fz.CryptoKeys[id] = &key
		// Flip the zone-level dnssec flag whenever an active key exists.
		fz.Zone.DNSsec = zoneHasActiveKey(fz)
		ok = true
	}()
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	writeJSON(w, http.StatusCreated, key)
	s.emit("dns.zone.dnssec.key_created", zoneID)
}

func (s *Server) handleCryptoKeyGet(w http.ResponseWriter, _ *http.Request, zoneID string, keyID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fz, ok := s.zones[zoneID]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "zone %q not found", zoneID)
		return
	}
	key, ok := fz.CryptoKeys[keyID]
	if !ok {
		writePDNSError(w, http.StatusNotFound, "cryptokey %d not found", keyID)
		return
	}
	writeJSON(w, http.StatusOK, *key)
}

func (s *Server) handleCryptoKeyPut(w http.ResponseWriter, r *http.Request, zoneID string, keyID int64) {
	var body powerdns.CryptoKey
	if err := decodeBody(r, &body); err != nil {
		writePDNSError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	state := putCryptoResult{status: http.StatusNoContent}
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		fz, ok := s.zones[zoneID]
		if !ok {
			state.fail(http.StatusNotFound, "zone %s not found", strconv.Quote(zoneID))
			return
		}
		key, ok := fz.CryptoKeys[keyID]
		if !ok {
			state.fail(http.StatusNotFound, "cryptokey %d not found", keyID)
			return
		}
		key.Active = body.Active
		fz.Zone.DNSsec = zoneHasActiveKey(fz)
	}()
	if state.status != http.StatusNoContent {
		writePDNSError(w, state.status, "%s", state.msg)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
	s.emit("dns.zone.dnssec.key_toggled", zoneID)
}

// putCryptoResult is the return shape from the locked section of
// handleCryptoKeyPut / handleCryptoKeyDelete. Kept as a struct so the
// closure can mutate without a pile of named returns.
type putCryptoResult struct {
	status int
	msg    string
}

// fail records a failure status + formatted message. The caller checks
// status != http.StatusNoContent after the locked closure returns.
func (r *putCryptoResult) fail(status int, format string, args ...any) {
	r.status = status
	r.msg = fmt.Sprintf(format, args...)
}

func (s *Server) handleCryptoKeyDelete(w http.ResponseWriter, _ *http.Request, zoneID string, keyID int64) {
	state := putCryptoResult{status: http.StatusNoContent}
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		fz, ok := s.zones[zoneID]
		if !ok {
			state.fail(http.StatusNotFound, "zone %s not found", strconv.Quote(zoneID))
			return
		}
		if _, ok := fz.CryptoKeys[keyID]; !ok {
			state.fail(http.StatusNotFound, "cryptokey %d not found", keyID)
			return
		}
		delete(fz.CryptoKeys, keyID)
		fz.Zone.DNSsec = zoneHasActiveKey(fz)
	}()
	if state.status != http.StatusNoContent {
		writePDNSError(w, state.status, "%s", state.msg)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
	s.emit("dns.zone.dnssec.key_deleted", zoneID)
}

// zoneHasActiveKey reports whether the fake zone has at least one active
// cryptokey. Used to keep the zone-level dnssec flag in sync.
func zoneHasActiveKey(fz *fakeZone) bool {
	for _, k := range fz.CryptoKeys {
		if k.Active {
			return true
		}
	}
	return false
}
