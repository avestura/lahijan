// Package fake provides an httptest-based fake of the PowerDNS Authoritative
// HTTP API. The fake is intentionally in-memory and per-test scoped: every
// test that needs a PDNS daemon spins one up via NewServer, points a real
// *powerdns.Provider at it via powerdns.NewClient, and tears it down at the
// end of the test.
//
// The fake implements the subset of the PDNS REST surface Lahijan's driver
// uses: GET /api/v1/servers/localhost (ping), zones CRUD, RRset PATCH,
// cryptokeys CRUD, and metadata CRUD. PDNS' real daemon delegates zone data
// to its gpgsql backend; the fake keeps it in maps.
//
// Concurrency: the fake serializes writes behind a sync.Mutex. Reads are
// goroutine-safe.
package fake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/powerdns"
)

// Server is an in-memory fake of the PowerDNS daemon. Construct via NewServer.
type Server struct {
	// HTTP is the underlying httptest.Server. Tests dial it via the URL
	// returned by HTTP.URL when constructing a real *powerdns.Provider.
	HTTP *httptest.Server

	// APIKey is the key the fake expects on every request in the
	// X-API-Key header. When non-empty, requests without (or with a
	// mismatched) key are rejected with 401. NewServer picks a stable
	// value; tests override via SetAPIKey.
	APIKey string

	mu             sync.Mutex
	zones          map[string]*fakeZone // canonical id -> zone
	serverVersion  string
	daemonType     string
	apiKeyRequired bool

	// emitHook is called by every mutating handler after the change
	// commits. Tests use it to assert the driver's event-synthesis path
	// routed through correctly.
	emitHook func(topic, zoneID string)
}

// fakeZone is the per-zone in-memory state.
type fakeZone struct {
	Zone       powerdns.Zone
	RRsets     map[string]*powerdns.RRset // "<name> <type>" -> rrset
	CryptoKeys map[int64]*powerdns.CryptoKey
	Metadata   map[string]*powerdns.Metadata
	nextKeyID  int64
}

// NewServer returns a started fake PowerDNS server. The server is alive;
// the caller is responsible for calling HTTP.Close at the end of the test.
//
// Defaults: serverVersion="4.9.0-fake", daemonType="authoritative",
// APIKey="test-key" (matches the test wiring in connectProvider).
func NewServer(t testing.TB) *Server {
	t.Helper()
	s := newServer()
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(func() { s.HTTP.Close() })
	return s
}

// NewServerStandalone returns a started fake PowerDNS server without
// wiring testing.TB.Cleanup. The caller MUST call s.HTTP.Close() when
// done (typically via defer). Used by non-test-binary callers (e.g. the
// WS-22 e2e harness under test/e2e/harness/, which drives the app from a
// long-running process instead of a *testing.T).
func NewServerStandalone() *Server {
	s := newServer()
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handler))
	return s
}

// newServer constructs the in-memory state shared by NewServer and
// NewServerStandalone so the seed + handler wiring cannot drift.
func newServer() *Server {
	s := &Server{
		zones:          make(map[string]*fakeZone),
		serverVersion:  "4.9.0-fake",
		daemonType:     "authoritative",
		APIKey:         "test-key",
		apiKeyRequired: true,
	}
	return s
}

// SetAPIKey overrides the API key the fake expects. Empty disables the
// API-key check (the fake accepts any request).
func (s *Server) SetAPIKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.APIKey = key
	s.apiKeyRequired = key != ""
}

// SetServerVersion overrides the reported server version.
func (s *Server) SetServerVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverVersion = v
}

// SetServerDaemonType overrides the reported daemon_type (defaults to
// "authoritative"; tests use "recursor" to exercise the ping check).
func (s *Server) SetServerDaemonType(t string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.daemonType = t
}

// SetEmitHook registers a callback fired after every successful mutation.
func (s *Server) SetEmitHook(fn func(topic, zoneID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitHook = fn
}

// Zones returns a snapshot of every zone currently in the fake. Used by
// tests that want to assert state outside of the driver's view.
func (s *Server) Zones() []powerdns.Zone {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]powerdns.Zone, 0, len(s.zones))
	for _, fz := range s.zones {
		out = append(out, snapshotZone(fz))
	}
	return out
}

// handler is the central mux. The PDNS REST API lives under /api/v1/.
func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	if s.apiKeyRequired && r.Header.Get(powerdnsAPIKeyHeader()) != s.APIKey {
		writePDNSError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, powerdnsAPIPrefix())
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "servers/localhost" && r.Method == http.MethodGet:
		s.handleServerInfo(w, r)
	case path == "servers/localhost/zones" && r.Method == http.MethodGet:
		s.handleZonesList(w, r)
	case path == "servers/localhost/zones" && r.Method == http.MethodPost:
		s.handleZoneCreate(w, r)
	case strings.HasPrefix(path, "servers/localhost/zones/"):
		s.handleZoneSub(w, r, strings.TrimPrefix(path, "servers/localhost/zones/"))
	default:
		writePDNSError(w, http.StatusNotFound, "not implemented in fake: %s %s", r.Method, path)
	}
}

// ---- /servers/localhost (server info / ping) ----

func (s *Server) handleServerInfo(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, powerdnsServerInfo{
		Type:       "Server",
		ID:         "localhost",
		URL:        "/api/v1/servers/localhost",
		DaemonType: s.daemonType,
		Version:    s.serverVersion,
		ConfigURL:  "/api/v1/servers/localhost/config",
		ZonesURL:   "/api/v1/servers/localhost/zones",
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writePDNSError(w http.ResponseWriter, status int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": fmt.Sprintf(format, args...),
	})
}

func decodeBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

// powerdnsServerInfo mirrors powerdns.server (unexported) via structural
// typing so the fake does not need to reach into the driver package's
// unexported type.
type powerdnsServerInfo struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	URL        string `json:"url"`
	DaemonType string `json:"daemon_type"`
	Version    string `json:"version"`
	ConfigURL  string `json:"config_url"`
	ZonesURL   string `json:"zones_url"`
}

// powerdnsAPIKeyHeader + powerdnsAPIPrefix expose the driver's wire-level
// constants from the test package without an import cycle.
func powerdnsAPIKeyHeader() string { return "X-API-Key" }
func powerdnsAPIPrefix() string    { return "/api/v1" }

// snapshotZone returns a deep copy of the per-zone in-memory state suitable
// for returning to the caller. Mutating the copy must not affect the fake.
func snapshotZone(fz *fakeZone) powerdns.Zone {
	out := fz.Zone
	if len(fz.RRsets) > 0 {
		out.RRsets = make([]powerdns.RRset, 0, len(fz.RRsets))
		for _, rr := range fz.RRsets {
			cp := *rr
			out.RRsets = append(out.RRsets, cp)
		}
	}
	return out
}

// emit fires the registered emit hook (if any). Called by every mutating
// handler after the change commits. Handlers MUST NOT hold s.mu when
// calling emit — the hook may itself need to inspect server state, which
// would deadlock. The standard pattern is:
//
//	s.mu.Lock()
//	... mutate ...
//	s.mu.Unlock()
//	s.emit(...)
func (s *Server) emit(topic, zoneID string) {
	fn := s.emitHookSnapshot()
	if fn != nil {
		fn(topic, zoneID)
	}
}

// emitHookSnapshot returns the current emit hook without holding s.mu across
// the call into the hook (the hook may itself need s.mu).
func (s *Server) emitHookSnapshot() func(topic, zoneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emitHook
}
