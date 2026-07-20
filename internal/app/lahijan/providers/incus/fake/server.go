// Package fake provides an httptest-based fake of the Incus REST API. The fake
// is intentionally in-memory and per-test scoped: every test that needs an
// Incus daemon spins one up via NewServer, points a real *incus.Provider at
// it via incus.NewClient, and tears it down at the end of the test.
//
// The fake implements the subset of the Incus REST surface Lahijan's driver
// uses: GET /1.0 (ping), projects, instances, instance state, images, image
// aliases, profiles, networks, network ACLs, network forwards, storage pools,
// storage volumes, async operations + wait, and the events + exec websockets.
//
// Concurrency: the fake serialises writes behind a sync.Mutex. Reads are
// goroutine-safe. The events websocket broadcasts to every connected listener.
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
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Server is an in-memory fake of the Incus daemon. Construct via NewServer.
type Server struct {
	// HTTP is the underlying httptest.Server. Tests dial it via the URL
	// returned by HTTP.URL when constructing a real *incus.Provider.
	HTTP *httptest.Server

	// WSUpgrader is the websocket upgrader used for events + exec.
	WSUpgrader websocket.Upgrader

	mu           sync.Mutex
	projects     map[string]*fakeProject
	images       map[string]*incus.Image
	aliases      map[string]string // alias -> fingerprint
	storagePools map[string]*incus.StoragePool
	operations   map[string]*fakeOperation

	// events fan-out
	eventsMu    sync.Mutex
	eventsConns []*websocket.Conn
	eventsLog   []string // every lifecycle event emitted (for assertion)
	eventsLogMu sync.Mutex

	// exec per-fd websocket hand-off: when a goroutine is waiting for the
	// driver to dial a given (opID, secret), it parks here. The HTTP
	// handler pops it and hands the *websocket.Conn over.
	execAcceptMu sync.Mutex
	execAccept   map[string]chan *websocket.Conn

	// execHandler is called for every exec POST. Tests can override the
	// default which echoes the command back on stdout + sets exit code 0.
	execHandler func(project, instance string, params incus.InstanceExecPost) (stdout, stderr []byte, exitCode int)

	// serverInfo overrides for Capabilities assertions.
	serverClustered bool
	serverVersion   string
}

// fakeProject holds the per-project state.
type fakeProject struct {
	Project     incus.Project
	Instances   map[string]*incus.Instance
	Profiles    map[string]*incus.Profile
	Networks    map[string]*incus.Network
	NetworkACLs map[string]*incus.NetworkACL
	Forwards    map[string]map[string]*incus.NetworkForward // network -> listen_address -> forward
	Volumes     map[string]map[string]*incus.StorageVolume  // pool -> name -> volume
}

// fakeOperation tracks an in-flight async operation.
type fakeOperation struct {
	op   incus.Operation
	done chan struct{}
}

// NewServer returns a started fake Incus server. The server is alive; the
// caller is responsible for calling HTTP.Close at the end of the test.
//
// The fake pre-seeds a default storage pool ("default") so storage-volume
// tests do not need to bootstrap one every time.
func NewServer(t testing.TB) *Server {
	t.Helper()
	s := newServer()
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(func() { s.HTTP.Close() })
	return s
}

// NewServerStandalone returns a started fake Incus server without wiring
// testing.TB.Cleanup. The caller MUST call s.HTTP.Close() when done
// (typically via defer). Used by non-test-binary callers (e.g. the WS-22
// e2e harness under test/e2e/harness/, which drives the app from a
// long-running process instead of a *testing.T).
func NewServerStandalone() *Server {
	s := newServer()
	s.HTTP = httptest.NewServer(http.HandlerFunc(s.handler))
	return s
}

// newServer builds the per-fake state. Both NewServer and
// NewServerStandalone share this constructor so the seed + handler wiring
// cannot drift between them.
func newServer() *Server {
	s := &Server{
		projects:      make(map[string]*fakeProject),
		images:        make(map[string]*incus.Image),
		aliases:       make(map[string]string),
		storagePools:  make(map[string]*incus.StoragePool),
		operations:    make(map[string]*fakeOperation),
		serverVersion: "6.0.0-fake",
		WSUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	s.execHandler = defaultExecHandler
	s.execAccept = make(map[string]chan *websocket.Conn)

	// Seed: default storage pool + default project.
	s.storagePools["default"] = &incus.StoragePool{
		Name:   "default",
		Driver: "dir",
		Config: map[string]string{},
	}
	s.projects["default"] = newFakeProject(incus.Project{Name: "default"})

	return s
}

// newFakeProject builds a *fakeProject with all the per-resource maps
// initialised.
func newFakeProject(p incus.Project) *fakeProject {
	return &fakeProject{
		Project:     p,
		Instances:   make(map[string]*incus.Instance),
		Profiles:    make(map[string]*incus.Profile),
		Networks:    make(map[string]*incus.Network),
		NetworkACLs: make(map[string]*incus.NetworkACL),
		Forwards:    make(map[string]map[string]*incus.NetworkForward),
		Volumes:     make(map[string]map[string]*incus.StorageVolume),
	}
}

// SetServerClustered flips the cluster-mode flag in the /1.0 response.
func (s *Server) SetServerClustered(clustered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverClustered = clustered
}

// SetServerVersion overrides the reported server version.
func (s *Server) SetServerVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverVersion = v
}

// SetExecHandler overrides the per-exec handler. The handler returns the
// stdout/stderr bytes + the process exit code the fake should record.
func (s *Server) SetExecHandler(h func(project, instance string, params incus.InstanceExecPost) (stdout, stderr []byte, exitCode int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execHandler = h
}

// EmitLifecycleEvent broadcasts a lifecycle event to every connected events
// listener. Used by tests that want to assert the driver fans Incus events
// into the bus.
func (s *Server) EmitLifecycleEvent(action, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env := incus.EventEnvelope{
		Type:      "lifecycle",
		Timestamp: time.Now().UTC(),
		Metadata: mustJSONMarshal(incus.LifecycleEvent{
			Action: action,
			Source: source,
		}),
	}
	payload, _ := json.Marshal(env)

	s.eventsLogMu.Lock()
	s.eventsLog = append(s.eventsLog, action+" "+source)
	s.eventsLogMu.Unlock()

	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	for _, conn := range s.eventsConns {
		_ = conn.WriteMessage(websocket.TextMessage, payload)
	}
}

// LifecycleLog returns the actions the server was asked to emit (via
// EmitLifecycleEvent). Used to assert ordering / count in tests.
func (s *Server) LifecycleLog() []string {
	s.eventsLogMu.Lock()
	defer s.eventsLogMu.Unlock()
	out := make([]string, len(s.eventsLog))
	copy(out, s.eventsLog)
	return out
}

// HasEventsListener reports whether at least one websocket client is currently
// subscribed to the events stream. Tests use this to wait for the driver's
// listener to register before emitting events.
func (s *Server) HasEventsListener() bool {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	return len(s.eventsConns) > 0
}

// handler is the central mux. The Incus REST API lives under /1.0/.
func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	// Strip the /1.0 prefix.
	path := strings.TrimPrefix(r.URL.Path, "/1.0")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.handleServerInfo(w, r)
		return
	case path == "events":
		s.handleEventsWS(w, r)
		return
	case strings.HasPrefix(path, "operations/"):
		s.handleOperations(w, r, strings.TrimPrefix(path, "operations/"))
		return
	case path == "projects" && r.Method == http.MethodPost:
		s.handleProjectCreate(w, r)
		return
	case strings.HasPrefix(path, "projects/"):
		s.handleProject(w, r, strings.TrimPrefix(path, "projects/"))
		return
	case path == "projects":
		s.handleProjectsList(w, r)
		return
	case path == "instances" && r.Method == http.MethodPost:
		s.handleInstanceCreate(w, r)
		return
	case strings.HasPrefix(path, "instances/") && strings.HasSuffix(path, "/exec"):
		s.handleExec(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "instances/"), "/exec"))
		return
	case strings.HasPrefix(path, "instances/") && strings.HasSuffix(path, "/state"):
		s.handleInstanceState(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "instances/"), "/state"))
		return
	case strings.HasPrefix(path, "instances/"):
		s.handleInstance(w, r, strings.TrimPrefix(path, "instances/"))
		return
	case path == "instances":
		s.handleInstancesList(w, r)
		return
	case path == "profiles" && r.Method == http.MethodPost:
		s.handleProfileCreate(w, r)
		return
	case strings.HasPrefix(path, "profiles/"):
		s.handleProfile(w, r, strings.TrimPrefix(path, "profiles/"))
		return
	case path == "profiles":
		s.handleProfilesList(w, r)
		return
	case path == "images" && r.Method == http.MethodPost:
		s.handleImageCreate(w, r)
		return
	case strings.HasPrefix(path, "images/aliases/"):
		s.handleImageAlias(w, r, strings.TrimPrefix(path, "images/aliases/"))
		return
	case strings.HasPrefix(path, "images/"):
		s.handleImage(w, r, strings.TrimPrefix(path, "images/"))
		return
	case path == "images":
		s.handleImagesList(w, r)
		return
	case path == "networks" && r.Method == http.MethodPost:
		s.handleNetworkCreate(w, r)
		return
	case strings.HasPrefix(path, "networks/"):
		s.handleNetworkOrForward(w, r, strings.TrimPrefix(path, "networks/"))
		return
	case path == "networks":
		s.handleNetworksList(w, r)
		return
	case strings.HasPrefix(path, "network-acls"):
		s.handleNetworkACL(w, r, strings.TrimPrefix(path, "network-acls"))
		return
	case path == "storage-pools" && r.Method == http.MethodPost:
		s.handleStoragePoolCreate(w, r)
		return
	case strings.HasPrefix(path, "storage-pools/"):
		s.handleStoragePool(w, r, strings.TrimPrefix(path, "storage-pools/"))
		return
	case path == "storage-pools":
		s.handleStoragePoolsList(w, r)
		return
	default:
		writeIncusError(w, http.StatusNotFound, "not implemented in fake: %s %s", r.Method, path)
	}
}

// ---- /1.0 (server info) ----

func (s *Server) handleServerInfo(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := incus.ServerInfo{
		APIStatus:       "stable",
		APIVersion:      "1.0",
		Auth:            "trusted",
		Server:          "incus",
		ServerClustered: s.serverClustered,
		ServerName:      "fake-host",
		ServerVersion:   s.serverVersion,
	}
	writeIncusResult(w, http.StatusOK, info)
}

// ---- helpers ----

func writeIncusResult(w http.ResponseWriter, status int, metadata any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	raw, _ := json.Marshal(metadata)
	_ = json.NewEncoder(w).Encode(incus.Response{
		Type:       resultType(status),
		Status:     "Success",
		StatusCode: status,
		Metadata:   raw,
	})
}

func writeIncusAsync(w http.ResponseWriter, opID string) {
	writeIncusAsyncWithMeta(w, opID, nil)
}

// writeIncusAsyncWithMeta is the extended form of writeIncusAsync that carries
// the operation's metadata (used by exec, where the metadata field holds the
// per-fd websocket secrets).
func writeIncusAsyncWithMeta(w http.ResponseWriter, opID string, metadata json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/1.0/operations/"+opID)
	w.WriteHeader(http.StatusAccepted)
	opMeta := map[string]any{
		"id":          opID,
		"class":       "async",
		"status":      "Running",
		"status_code": http.StatusOK,
		"may_cancel":  true,
		"err":         "",
		"created_at":  time.Now().UTC(),
		"updated_at":  time.Now().UTC(),
	}
	// When the caller provides explicit metadata (exec per-fd secrets), it
	// takes precedence; otherwise we serialise the opMeta above.
	finalMeta := mustJSONMarshal(opMeta)
	if len(metadata) > 0 {
		finalMeta = metadata
	}
	_ = json.NewEncoder(w).Encode(incus.Response{
		Type:       "async",
		Status:     "Operation created",
		StatusCode: http.StatusAccepted,
		Operation:  "/1.0/operations/" + opID,
		Metadata:   finalMeta,
	})
}

func writeIncusError(w http.ResponseWriter, status int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":       "error",
		"error":      fmt.Sprintf(format, args...),
		"error_code": status,
	})
}

// resultType maps an HTTP status code to the Incus response "type" field.
func resultType(status int) string {
	switch {
	case status >= 200 && status < 300:
		if status == http.StatusAccepted {
			return "async"
		}
		return "success"
	default:
		return "error"
	}
}

func mustJSONMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
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

// queryProject returns the project query param, defaulting to "default".
func queryProject(r *http.Request) string {
	if q := r.URL.Query().Get("project"); q != "" {
		return q
	}
	return "default"
}

// newOpID returns a fresh operation id.
func newOpID() string { return uuid.NewString() }

// defaultExecHandler is the default per-exec handler: it echoes the command on
// stdout and returns exit code 0.
func defaultExecHandler(_, _ string, params incus.InstanceExecPost) ([]byte, []byte, int) {
	out := []byte(strings.Join(params.Command, " "))
	return out, nil, 0
}
