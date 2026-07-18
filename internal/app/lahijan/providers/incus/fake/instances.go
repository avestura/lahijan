// Package fake: instances.go implements the instance + instance-state +
// exec sub-handlers.
package fake

import (
	"net/http"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

func (s *Server) handleInstancesList(w http.ResponseWriter, r *http.Request) {
	project := queryProject(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	out := make([]incus.Instance, 0, len(fp.Instances))
	for _, inst := range fp.Instances {
		out = append(out, *inst)
	}
	writeIncusResult(w, http.StatusOK, out)
}

func (s *Server) handleInstanceCreate(w http.ResponseWriter, r *http.Request) {
	var body incus.InstancesPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	if body.Project == "" {
		body.Project = queryProject(r)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[body.Project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", body.Project)
		return
	}
	name := body.Name
	if _, exists := fp.Instances[name]; exists {
		writeIncusError(w, http.StatusConflict, "Instance %q already exists", name)
		return
	}
	inst := &incus.Instance{
		Name:        name,
		Project:     body.Project,
		Description: body.Description,
		Config:      body.Config,
		Devices:     body.Devices,
		Profiles:    body.Profiles,
		Type:        body.Type,
		Status:      "Stopped",
		StatusCode:  102,
	}
	if inst.Type == "" {
		inst.Type = "container"
	}
	fp.Instances[name] = inst

	opID := newOpID()
	s.registerOp(opID, nil)
	writeIncusAsync(w, opID)
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request, name string) {
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
		inst, ok := fp.Instances[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Instance %q not found", name)
			return
		}
		writeIncusResult(w, http.StatusOK, inst)
	case http.MethodPut:
		var body incus.InstancePut
		if err := decodeBody(r, &body); err != nil {
			writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		inst, ok := fp.Instances[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Instance %q not found", name)
			return
		}
		inst.Description = body.Description
		inst.Config = body.Config
		inst.Devices = body.Devices
		inst.Profiles = body.Profiles
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
		if _, ok := fp.Instances[name]; !ok {
			writeIncusError(w, http.StatusNotFound, "Instance %q not found", name)
			return
		}
		delete(fp.Instances, name)
		opID := newOpID()
		s.registerOp(opID, nil)
		writeIncusAsync(w, opID)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleInstanceState(w http.ResponseWriter, r *http.Request, name string) {
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
		inst, ok := fp.Instances[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Instance %q not found", name)
			return
		}
		writeIncusResult(w, http.StatusOK, incus.InstanceState{
			Status:     inst.Status,
			StatusCode: inst.StatusCode,
		})
	case http.MethodPut:
		var body incus.InstanceStatePut
		if err := decodeBody(r, &body); err != nil {
			writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
			return
		}
		s.mu.Lock()
		fp, ok := s.projects[project]
		if !ok {
			s.mu.Unlock()
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		inst, ok := fp.Instances[name]
		if !ok {
			s.mu.Unlock()
			writeIncusError(w, http.StatusNotFound, "Instance %q not found", name)
			return
		}
		applyAction(inst, body.Action)
		s.mu.Unlock()

		opID := newOpID()
		s.registerOp(opID, nil)
		writeIncusAsync(w, opID)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

// applyAction applies a lifecycle action to an instance in place. Caller
// holds the server's mu.
func applyAction(inst *incus.Instance, action string) {
	switch action {
	case "start":
		inst.Status = "Running"
		inst.StatusCode = 103
	case "stop":
		inst.Status = "Stopped"
		inst.StatusCode = 102
	case "restart":
		inst.Status = "Running"
		inst.StatusCode = 103
	case "freeze":
		inst.Status = "Frozen"
		inst.StatusCode = 110
	case "unfreeze":
		inst.Status = "Running"
		inst.StatusCode = 103
	}
}
