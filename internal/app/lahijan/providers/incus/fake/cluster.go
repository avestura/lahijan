// Package fake: cluster.go implements the cluster-members sub-handlers
// for the in-memory fake Incus daemon (WS-26). The fake models a small
// cluster as a slice of incus.ClusterMember; the default seed is a single
// member ("fake-host") that matches the ServerName returned by the
// existing /1.0 server-info probe.
package fake

import (
	"net/http"
	"strings"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// clusterMembers returns the current cluster members. Caller holds s.mu.
func (s *Server) clusterMembers() []incus.ClusterMember {
	out := make([]incus.ClusterMember, len(s.cluster.members))
	copy(out, s.cluster.members)
	return out
}

// clusterState carries the in-memory cluster membership. Stored on the
// server behind the central s.mu so reads + writes serialize with the
// other fake state.
type clusterState struct {
	// members is the canonical list. The first entry is the local
	// host; tests can add more via AddClusterMember.
	members []incus.ClusterMember
}

// AddClusterMember appends a member to the fake cluster. The member's
// Status defaults to "Online"; tests can mutate it via SetClusterMemberStatus.
// Idempotent on ServerName (re-adding an existing member is a no-op so
// tests can call it without a setup guard).
func (s *Server) AddClusterMember(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.cluster.members {
		if m.ServerName == name {
			return
		}
	}
	s.cluster.members = append(s.cluster.members, incus.ClusterMember{
		ServerName:    name,
		URL:           "/1.0/cluster/members/" + name,
		Status:        "Online",
		Architecture:  "x86_64",
		Database:      false,
		Roles:         []string{},
		Config:        map[string]string{},
		FailureDomain: "default",
	})
}

// SetClusterMemberStatus overrides a member's status ("Online", "Offline",
// "Evacuated"). Used by tests that need to exercise the placement
// driver's filter (Offline members are skipped during scheduling).
func (s *Server) SetClusterMemberStatus(name, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, m := range s.cluster.members {
		if m.ServerName == name {
			s.cluster.members[i].Status = status
			return
		}
	}
}

// ClusterMembers returns a snapshot of the current member list. Used by
// tests that assert on the placement driver's view of the cluster.
func (s *Server) ClusterMembers() []incus.ClusterMember {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clusterMembers()
}

// handleCluster dispatches /cluster/* paths. The Incus cluster API lives
// under /1.0/cluster; the only sub-paths the driver touches are
// /cluster/members (list + create) and /cluster/members/<name>
// (get + evacuate/restore).
func (s *Server) handleCluster(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "members" && r.Method == http.MethodGet:
		s.handleClusterMembersList(w, r)
	case rest == "members" && r.Method == http.MethodPost:
		s.handleClusterMemberJoin(w, r)
	case strings.HasPrefix(rest, "members/"):
		s.handleClusterMember(w, r, strings.TrimPrefix(rest, "members/"))
	default:
		writeIncusError(w, http.StatusNotFound, "not implemented in fake: %s %s", r.Method, rest)
	}
}

func (s *Server) handleClusterMembersList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeIncusResult(w, http.StatusOK, s.clusterMembers())
}

func (s *Server) handleClusterMemberJoin(w http.ResponseWriter, r *http.Request) {
	var body incus.ClusterMembersPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	if body.ServerName == "" {
		writeIncusError(w, http.StatusBadRequest, "server_name is required")
		return
	}
	s.mu.Lock()
	for _, m := range s.cluster.members {
		if m.ServerName == body.ServerName {
			s.mu.Unlock()
			writeIncusError(w, http.StatusConflict, "member %q already exists", body.ServerName)
			return
		}
	}
	s.cluster.members = append(s.cluster.members, incus.ClusterMember{
		ServerName:   body.ServerName,
		URL:          "/1.0/cluster/members/" + body.ServerName,
		Status:       "Online",
		Architecture: "x86_64",
		Config:       map[string]string{},
	})
	s.mu.Unlock()
	opID := newOpID()
	s.registerOp(opID, nil)
	writeIncusAsync(w, opID)
}

func (s *Server) handleClusterMember(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, m := range s.cluster.members {
			if m.ServerName == name {
				writeIncusResult(w, http.StatusOK, m)
				return
			}
		}
		writeIncusError(w, http.StatusNotFound, "member %q not found", name)
	case http.MethodPost:
		// Evacuate / restore. The fake records the action in the
		// member's status; the migration work itself is a no-op
		// (the instances stay where they are).
		var body incus.ClusterMemberPost
		if err := decodeBody(r, &body); err != nil {
			writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
			return
		}
		s.mu.Lock()
		var found bool
		for i, m := range s.cluster.members {
			if m.ServerName == name {
				found = true
				switch body.Action {
				case incus.ClusterMemberActionEvacuate:
					s.cluster.members[i].Status = "Evacuated"
				case incus.ClusterMemberActionRestore:
					s.cluster.members[i].Status = "Online"
				}
			}
		}
		s.mu.Unlock()
		if !found {
			writeIncusError(w, http.StatusNotFound, "member %q not found", name)
			return
		}
		opID := newOpID()
		s.registerOp(opID, nil)
		writeIncusAsync(w, opID)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}
