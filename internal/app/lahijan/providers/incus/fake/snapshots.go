// Package fake: snapshots.go implements the instance-snapshots sub-handlers.
// Supports create / list / get / rename (POST rename) / delete / restore /
// export. The state is kept in fakeProject.Snapshots keyed by
// "<instance>/<snapshot>"; the same key Incus uses on its URL path.
package fake

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// handleSnapshotsCollection handles GET/POST
// /1.0/instances/<name>/snapshots. POST creates; GET (with recursion=1)
// lists.
func (s *Server) handleSnapshotsCollection(w http.ResponseWriter, r *http.Request, instance string) {
	switch r.Method {
	case http.MethodPost:
		s.handleSnapshotCreate(w, r, instance)
	case http.MethodGet:
		s.handleSnapshotsList(w, r, instance)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

// handleSnapshotsList handles GET /1.0/instances/<name>/snapshots?recursion=1.
func (s *Server) handleSnapshotsList(w http.ResponseWriter, r *http.Request, instance string) {
	project := queryProject(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	if _, ok := fp.Instances[instance]; !ok {
		writeIncusError(w, http.StatusNotFound, "Instance %q not found", instance)
		return
	}
	prefix := instance + "/"
	out := make([]incus.InstanceSnapshot, 0)
	for key, snap := range fp.Snapshots {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		snap.InstanceName = key
		out = append(out, *snap)
	}
	writeIncusResult(w, http.StatusOK, out)
}

// handleSnapshotCreate handles POST /1.0/instances/<name>/snapshots.
func (s *Server) handleSnapshotCreate(w http.ResponseWriter, r *http.Request, instance string) {
	var body incus.InstanceSnapshotsPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	project := queryProject(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	inst, ok := fp.Instances[instance]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Instance %q not found", instance)
		return
	}
	if body.Name == "" {
		writeIncusError(w, http.StatusBadRequest, "snapshot name is required")
		return
	}
	key := instance + "/" + body.Name
	if _, exists := fp.Snapshots[key]; exists {
		writeIncusError(w, http.StatusConflict, "Snapshot %q already exists", body.Name)
		return
	}
	// The snapshot inherits the instance config + devices at capture time.
	configCopy := make(map[string]string, len(inst.Config))
	for k, v := range inst.Config {
		configCopy[k] = v
	}
	devicesCopy := make(map[string]map[string]string, len(inst.Devices))
	for k, v := range inst.Devices {
		row := make(map[string]string, len(v))
		for kk, vv := range v {
			row[kk] = vv
		}
		devicesCopy[k] = row
	}
	fp.Snapshots[key] = &incus.InstanceSnapshot{
		Name:         body.Name,
		InstanceName: key,
		Config:       configCopy,
		Devices:      devicesCopy,
		Architecture: inst.Architecture,
		CreatedAt:    time.Now().UTC(),
		Stateful:     body.Stateful,
		Size:         1024, // arbitrary small size so the service can record non-zero
	}
	opID := newOpID()
	s.registerOp(opID, nil)
	writeIncusAsync(w, opID)
}

// handleSnapshot routes the per-snapshot REST surface:
// GET/PUT/DELETE on the snapshot, POST for rename or restore (the
// presence of "/restore" disambiguates).
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, instance, snapshot string) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/restore") {
		s.handleSnapshotRestore(w, r, instance, snapshot)
		return
	}
	project := queryProject(r)
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		snap, ok := fp.Snapshots[instance+"/"+snapshot]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Snapshot %q not found", snapshot)
			return
		}
		writeIncusResult(w, http.StatusOK, snap)
	case http.MethodPost:
		// Rename via InstanceSnapshotPut.Name in the body (Incus accepts
		// POST /snapshots/<name> with a Name field).
		var body incus.InstanceSnapshotPut
		if err := decodeBody(r, &body); err != nil {
			writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
			return
		}
		if body.Name == "" {
			writeIncusError(w, http.StatusBadRequest, "rename requires a non-empty name")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		oldKey := instance + "/" + snapshot
		snap, ok := fp.Snapshots[oldKey]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Snapshot %q not found", snapshot)
			return
		}
		newKey := instance + "/" + body.Name
		if _, exists := fp.Snapshots[newKey]; exists {
			writeIncusError(w, http.StatusConflict, "Snapshot %q already exists", body.Name)
			return
		}
		snap.Name = body.Name
		snap.InstanceName = newKey
		fp.Snapshots[newKey] = snap
		delete(fp.Snapshots, oldKey)
		opID := newOpID()
		s.registerOp(opID, nil)
		writeIncusAsync(w, opID)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		oldKey := instance + "/" + snapshot
		if _, ok := fp.Snapshots[oldKey]; !ok {
			writeIncusError(w, http.StatusNotFound, "Snapshot %q not found", snapshot)
			return
		}
		delete(fp.Snapshots, oldKey)
		opID := newOpID()
		s.registerOp(opID, nil)
		writeIncusAsync(w, opID)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

// handleSnapshotRestore handles POST /1.0/instances/<name>/snapshots/<snap>/restore.
// The fake just acknowledges; no real state change is applied (the
// snapshot's Config/Devices would replace the instance's).
func (s *Server) handleSnapshotRestore(w http.ResponseWriter, r *http.Request, instance, snapshot string) {
	project := queryProject(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	if _, ok := fp.Instances[instance]; !ok {
		writeIncusError(w, http.StatusNotFound, "Instance %q not found", instance)
		return
	}
	if _, ok := fp.Snapshots[instance+"/"+snapshot]; !ok {
		writeIncusError(w, http.StatusNotFound, "Snapshot %q not found", snapshot)
		return
	}
	opID := newOpID()
	s.registerOp(opID, nil)
	writeIncusAsync(w, opID)
}

// handleSnapshotExport handles GET /1.0/instances/<name>/snapshots/<snap>/export.
// Returns a synthetic tarball so the WS-25 backup worker can stream real
// bytes through its driver pipeline in tests.
func (s *Server) handleSnapshotExport(w http.ResponseWriter, r *http.Request, instance, snapshot string) {
	project := queryProject(r)
	s.mu.Lock()
	fp, ok := s.projects[project]
	if !ok {
		s.mu.Unlock()
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	snap, ok := fp.Snapshots[instance+"/"+snapshot]
	if !ok {
		s.mu.Unlock()
		writeIncusError(w, http.StatusNotFound, "Snapshot %q not found", snapshot)
		return
	}
	size := snap.Size
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, snapshot))
	w.WriteHeader(http.StatusOK)
	// Write 'size' zero bytes. Real Incus streams a tarball; the fake just
	// satisfies the contract that the body length matches the snapshot size.
	if _, err := io.CopyN(w, zeroReader{}, size); err != nil {
		// Connection probably closed mid-stream; nothing else to do.
		return
	}
}

// zeroReader is an io.Reader that always returns zero bytes. Used by the
// snapshot-export handler to synthesise a body of a given length.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// -------------------------------------------------------------------------
// Path helpers. The Incus REST surface nests the snapshot API under the
// instance path, so we need precise shape matching before the bare
// /instances/<name> catch-all in the central mux.
// -------------------------------------------------------------------------

// isInstanceSnapshotsCollectionPath reports whether path is the snapshots
// collection of an instance: /1.0/instances/<name>/snapshots.
func isInstanceSnapshotsCollectionPath(path string) bool {
	return strings.HasPrefix(path, "instances/") && strings.HasSuffix(path, "/snapshots")
}

// isInstanceSnapshotPath reports whether path matches
// /1.0/instances/<name>/snapshots/<snap> with no further sub-path.
func isInstanceSnapshotPath(path string) bool {
	if !strings.HasPrefix(path, "instances/") {
		return false
	}
	if !strings.Contains(path, "/snapshots/") {
		return false
	}
	// Reject the /export and /restore sub-paths; those have their own routes.
	if strings.HasSuffix(path, "/export") || strings.HasSuffix(path, "/restore") {
		return false
	}
	// Exactly four slash-separated segments: instances, <name>, snapshots, <snap>.
	return len(strings.Split(path, "/")) == 4
}

// isInstanceSnapshotExportPath reports whether path is the binary-export
// endpoint: /1.0/instances/<name>/snapshots/<snap>/export.
func isInstanceSnapshotExportPath(path string) bool {
	return strings.HasPrefix(path, "instances/") &&
		strings.Contains(path, "/snapshots/") &&
		strings.HasSuffix(path, "/export")
}

// isInstanceSnapshotRestorePath reports whether path is the restore endpoint:
// /1.0/instances/<name>/snapshots/<snap>/restore.
func isInstanceSnapshotRestorePath(path string) bool {
	return strings.HasPrefix(path, "instances/") &&
		strings.Contains(path, "/snapshots/") &&
		strings.HasSuffix(path, "/restore")
}

// splitSnapshotSubPath takes the part after "instances/" (with the
// trailing suffix like "/export" or "/restore" already stripped) and
// returns the instance name + snapshot name.
//
// Example:
//
//	"foo/snapshots/bar" -> ("foo", "bar")
func splitSnapshotSubPath(p string) (instance, snapshot string) {
	parts := strings.SplitN(p, "/snapshots/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
