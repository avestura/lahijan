// Package fake: projects.go implements the project sub-handlers.
package fake

import (
	"encoding/json"
	"net/http"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

func (s *Server) handleProjectsList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]incus.Project, 0, len(s.projects))
	for _, fp := range s.projects {
		out = append(out, fp.Project)
	}
	writeIncusResult(w, http.StatusOK, out)
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	var body incus.ProjectsPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.projects[body.Name]; exists {
		writeIncusError(w, http.StatusConflict, "Project %q already exists", body.Name)
		return
	}
	s.projects[body.Name] = newFakeProject(incus.Project{
		Name:        body.Name,
		Description: body.Description,
		Config:      body.Config,
	})
	writeIncusResult(w, http.StatusCreated, s.projects[body.Name].Project)
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", name)
			return
		}
		writeIncusResult(w, http.StatusOK, fp.Project)
	case http.MethodPut:
		var body incus.ProjectPut
		if err := decodeBody(r, &body); err != nil {
			writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", name)
			return
		}
		fp.Project.Description = body.Description
		fp.Project.Config = body.Config
		writeIncusResult(w, http.StatusOK, fp.Project)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.projects[name]; !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", name)
			return
		}
		delete(s.projects, name)
		writeIncusResult(w, http.StatusOK, json.RawMessage(`{}`))
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}
