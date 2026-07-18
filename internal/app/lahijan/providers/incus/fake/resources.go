// Package fake: resources.go implements the images, profiles, networks, and
// storage sub-handlers. Each is a thin CRUD over the per-server state.
package fake

import (
	"net/http"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// ---------- Images ----------

func (s *Server) handleImagesList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]incus.Image, 0, len(s.images))
	for _, img := range s.images {
		out = append(out, *img)
	}
	writeIncusResult(w, http.StatusOK, out)
}

func (s *Server) handleImageCreate(w http.ResponseWriter, r *http.Request) {
	var body incus.ImagesPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Resolve the alias to a fingerprint; if missing, synthesize one.
	fingerprint := body.Source.Fingerprint
	if fingerprint == "" && body.Source.Alias != "" {
		fingerprint = "fp-" + body.Source.Alias
	}
	if fingerprint == "" {
		writeIncusError(w, http.StatusBadRequest, "image source requires alias or fingerprint")
		return
	}
	img := &incus.Image{
		Fingerprint:  fingerprint,
		Architecture: "x86_64",
		Type:         "container",
		Public:       body.Public,
		Project:      body.Project,
		Aliases:      body.Aliases,
	}
	s.images[fingerprint] = img
	for _, alias := range body.Aliases {
		s.aliases[alias.Name] = fingerprint
	}
	opID := newOpID()
	s.registerOp(opID, nil)
	writeIncusAsync(w, opID)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request, fp string) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		img, ok := s.images[fp]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "image %q not found", fp)
			return
		}
		writeIncusResult(w, http.StatusOK, img)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.images[fp]; !ok {
			writeIncusError(w, http.StatusNotFound, "image %q not found", fp)
			return
		}
		delete(s.images, fp)
		// Clean up aliases that pointed at this fingerprint.
		for alias, target := range s.aliases {
			if target == fp {
				delete(s.aliases, alias)
			}
		}
		opID := newOpID()
		s.registerOp(opID, nil)
		writeIncusAsync(w, opID)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleImageAlias(w http.ResponseWriter, r *http.Request, alias string) {
	if r.Method != http.MethodGet {
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.aliases[alias]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "alias %q not found", alias)
		return
	}
	writeIncusResult(w, http.StatusOK, map[string]any{
		"name":        alias,
		"target":      fp,
		"description": "fake alias",
	})
}

// ---------- Profiles ----------

func (s *Server) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	project := queryProject(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	out := make([]incus.Profile, 0, len(fp.Profiles))
	for _, prf := range fp.Profiles {
		out = append(out, *prf)
	}
	writeIncusResult(w, http.StatusOK, out)
}

func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var body incus.ProfilesPost
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
	if _, exists := fp.Profiles[body.Name]; exists {
		writeIncusError(w, http.StatusConflict, "Profile %q already exists", body.Name)
		return
	}
	fp.Profiles[body.Name] = &incus.Profile{
		Name:        body.Name,
		Description: body.Description,
		Config:      body.Config,
		Devices:     body.Devices,
		Project:     body.Project,
	}
	writeIncusResult(w, http.StatusCreated, fp.Profiles[body.Name])
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request, name string) {
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
		prf, ok := fp.Profiles[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Profile %q not found", name)
			return
		}
		writeIncusResult(w, http.StatusOK, prf)
	case http.MethodPut:
		var body incus.ProfilePut
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
		prf, ok := fp.Profiles[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Profile %q not found", name)
			return
		}
		prf.Description = body.Description
		prf.Config = body.Config
		prf.Devices = body.Devices
		writeIncusResult(w, http.StatusOK, prf)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		if _, ok := fp.Profiles[name]; !ok {
			writeIncusError(w, http.StatusNotFound, "Profile %q not found", name)
			return
		}
		delete(fp.Profiles, name)
		writeIncusResult(w, http.StatusOK, nil)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

// ---------- Networks ----------

// handleNetworkOrForward dispatches /networks/<name>{,/forwards{,/<addr>}}
// to the right handler. Incus nests forwards under a network, so the path
// has to be split before we can decide which sub-resource is targeted.
func (s *Server) handleNetworkOrForward(w http.ResponseWriter, r *http.Request, rest string) {
	segments := splitNonEmpty(rest, "/")
	if len(segments) >= 2 && segments[1] == "forwards" {
		// segments[0] is the network name; segments[2:] is the optional
		// listen-address.
		s.handleNetworkForward(w, r, segments[0], segments[2:])
		return
	}
	s.handleNetwork(w, r, rest)
}

func (s *Server) handleNetworksList(w http.ResponseWriter, r *http.Request) {
	project := queryProject(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.projects[project]
	if !ok {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	out := make([]incus.Network, 0, len(fp.Networks))
	for _, net := range fp.Networks {
		out = append(out, *net)
	}
	writeIncusResult(w, http.StatusOK, out)
}

func (s *Server) handleNetworkCreate(w http.ResponseWriter, r *http.Request) {
	var body incus.NetworksPost
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
	if _, exists := fp.Networks[body.Name]; exists {
		writeIncusError(w, http.StatusConflict, "Network %q already exists", body.Name)
		return
	}
	fp.Networks[body.Name] = &incus.Network{
		Name:        body.Name,
		Description: body.Description,
		Type:        body.Type,
		Config:      body.Config,
		Project:     body.Project,
		Managed:     true,
	}
	writeIncusResult(w, http.StatusCreated, fp.Networks[body.Name])
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request, name string) {
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
		net, ok := fp.Networks[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Network %q not found", name)
			return
		}
		writeIncusResult(w, http.StatusOK, net)
	case http.MethodPut:
		var body incus.NetworkPut
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
		net, ok := fp.Networks[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Network %q not found", name)
			return
		}
		net.Description = body.Description
		net.Config = body.Config
		writeIncusResult(w, http.StatusOK, net)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		if _, ok := fp.Networks[name]; !ok {
			writeIncusError(w, http.StatusNotFound, "Network %q not found", name)
			return
		}
		delete(fp.Networks, name)
		writeIncusResult(w, http.StatusOK, nil)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleNetworkACL(w http.ResponseWriter, r *http.Request, rest string) {
	// rest is "" or "/<name>". The main handler strips "network-acls".
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			project := queryProject(r)
			s.mu.Lock()
			defer s.mu.Unlock()
			fp, ok := s.projects[project]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
				return
			}
			out := make([]incus.NetworkACL, 0, len(fp.NetworkACLs))
			for _, acl := range fp.NetworkACLs {
				out = append(out, *acl)
			}
			writeIncusResult(w, http.StatusOK, out)
		case http.MethodPost:
			var body incus.NetworkACL
			if err := decodeBody(r, &body); err != nil {
				writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
				return
			}
			// Prefer the body's project; fall back to the query param.
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
			fp.NetworkACLs[body.Name] = &body
			writeIncusResult(w, http.StatusCreated, body)
		default:
			writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		}
		return
	}
	name := rest[1:] // strip leading "/"
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
		acl, ok := fp.NetworkACLs[name]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "ACL %q not found", name)
			return
		}
		writeIncusResult(w, http.StatusOK, acl)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		fp, ok := s.projects[project]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
			return
		}
		if _, ok := fp.NetworkACLs[name]; !ok {
			writeIncusError(w, http.StatusNotFound, "ACL %q not found", name)
			return
		}
		delete(fp.NetworkACLs, name)
		writeIncusResult(w, http.StatusOK, nil)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

// ---------- Storage pools + volumes ----------

func (s *Server) handleStoragePoolsList(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]incus.StoragePool, 0, len(s.storagePools))
	for _, pool := range s.storagePools {
		out = append(out, *pool)
	}
	writeIncusResult(w, http.StatusOK, out)
}

func (s *Server) handleStoragePoolCreate(w http.ResponseWriter, r *http.Request) {
	var body incus.StoragePoolsPost
	if err := decodeBody(r, &body); err != nil {
		writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.storagePools[body.Name]; exists {
		writeIncusError(w, http.StatusConflict, "Storage pool %q already exists", body.Name)
		return
	}
	s.storagePools[body.Name] = &incus.StoragePool{
		Name:        body.Name,
		Description: body.Description,
		Driver:      body.Driver,
		Config:      body.Config,
	}
	writeIncusResult(w, http.StatusCreated, s.storagePools[body.Name])
}

func (s *Server) handleStoragePool(w http.ResponseWriter, r *http.Request, rest string) {
	// rest is "<pool>" or "<pool>/volumes" or "<pool>/volumes/<type>/<name>".
	segments := splitNonEmpty(rest, "/")
	if len(segments) == 0 {
		writeIncusError(w, http.StatusNotFound, "missing pool name")
		return
	}
	poolName := segments[0]
	if len(segments) >= 2 && segments[1] == "volumes" {
		s.handleStorageVolume(w, r, poolName, segments[2:])
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		pool, ok := s.storagePools[poolName]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Pool %q not found", poolName)
			return
		}
		writeIncusResult(w, http.StatusOK, pool)
	case http.MethodPut:
		var body incus.StoragePoolPut
		if err := decodeBody(r, &body); err != nil {
			writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		pool, ok := s.storagePools[poolName]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Pool %q not found", poolName)
			return
		}
		pool.Description = body.Description
		pool.Config = body.Config
		writeIncusResult(w, http.StatusOK, pool)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.storagePools[poolName]; !ok {
			writeIncusError(w, http.StatusNotFound, "Pool %q not found", poolName)
			return
		}
		delete(s.storagePools, poolName)
		writeIncusResult(w, http.StatusOK, nil)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

func (s *Server) handleStorageVolume(w http.ResponseWriter, r *http.Request, poolName string, rest []string) {
	project := queryProject(r)
	s.mu.Lock()
	fp, fpOk := s.projects[project]
	_, poolOk := s.storagePools[poolName]
	s.mu.Unlock()
	if !fpOk {
		writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
		return
	}
	if !poolOk {
		writeIncusError(w, http.StatusNotFound, "Pool %q not found", poolName)
		return
	}
	// rest is [] (list/create) or [type, name] (get/delete).
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			s.mu.Lock()
			defer s.mu.Unlock()
			vols, ok := fp.Volumes[poolName]
			if !ok {
				writeIncusResult(w, http.StatusOK, []incus.StorageVolume{})
				return
			}
			out := make([]incus.StorageVolume, 0, len(vols))
			for _, v := range vols {
				out = append(out, *v)
			}
			writeIncusResult(w, http.StatusOK, out)
		case http.MethodPost:
			var body incus.StorageVolumesPost
			if err := decodeBody(r, &body); err != nil {
				writeIncusError(w, http.StatusBadRequest, "decode: %v", err)
				return
			}
			// Prefer the body's project; the Lahijan driver puts the
			// project in the body for storage volumes (not in the URL).
			bodyProject := project
			if body.Project != "" {
				bodyProject = body.Project
			}
			s.mu.Lock()
			bodyFP, fpOK := s.projects[bodyProject]
			if !fpOK {
				s.mu.Unlock()
				writeIncusError(w, http.StatusNotFound, "Project %q not found", bodyProject)
				return
			}
			vols, ok := bodyFP.Volumes[poolName]
			if !ok {
				vols = make(map[string]*incus.StorageVolume)
				bodyFP.Volumes[poolName] = vols
			}
			if _, exists := vols[body.Name]; exists {
				s.mu.Unlock()
				writeIncusError(w, http.StatusConflict, "Volume %q already exists", body.Name)
				return
			}
			vols[body.Name] = &incus.StorageVolume{
				Name:    body.Name,
				Type:    body.Type,
				Config:  body.Config,
				Project: bodyProject,
				Pool:    poolName,
			}
			s.mu.Unlock()
			writeIncusResult(w, http.StatusCreated, vols[body.Name])
		default:
			writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		}
		return
	}
	if len(rest) != 2 {
		writeIncusError(w, http.StatusNotFound, "unsupported volume path %q", rest)
		return
	}
	volType := rest[0]
	volName := rest[1]
	_ = volType // not checked; the fake stores volumes by name only
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		vols, ok := fp.Volumes[poolName]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Volume %q not found", volName)
			return
		}
		vol, ok := vols[volName]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Volume %q not found", volName)
			return
		}
		writeIncusResult(w, http.StatusOK, vol)
	case http.MethodDelete:
		s.mu.Lock()
		defer s.mu.Unlock()
		vols, ok := fp.Volumes[poolName]
		if !ok {
			writeIncusError(w, http.StatusNotFound, "Volume %q not found", volName)
			return
		}
		if _, ok := vols[volName]; !ok {
			writeIncusError(w, http.StatusNotFound, "Volume %q not found", volName)
			return
		}
		delete(vols, volName)
		writeIncusResult(w, http.StatusOK, nil)
	default:
		writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
	}
}

// splitNonEmpty splits s on sep and drops empty segments.
func splitNonEmpty(s, sep string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if string(r) == sep {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// ---------- Network forwards (project-scoped, nested under a network) -----

// handleNetworkForward serves /networks/<net>/forwards{,/<listen_addr>}.
func (s *Server) handleNetworkForward(w http.ResponseWriter, r *http.Request, network string, rest []string) {
	project := queryProject(r)
	switch len(rest) {
	case 0:
		// Collection: list or create.
		switch r.Method {
		case http.MethodGet:
			s.mu.Lock()
			defer s.mu.Unlock()
			fp, ok := s.projects[project]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
				return
			}
			fws, ok := fp.Forwards[network]
			if !ok {
				writeIncusResult(w, http.StatusOK, []incus.NetworkForward{})
				return
			}
			out := make([]incus.NetworkForward, 0, len(fws))
			for _, fw := range fws {
				out = append(out, *fw)
			}
			writeIncusResult(w, http.StatusOK, out)
		case http.MethodPost:
			var body struct {
				ListenAddress string           `json:"listen_address"`
				Ports         []map[string]any `json:"ports,omitempty"`
			}
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
			fws, ok := fp.Forwards[network]
			if !ok {
				fws = make(map[string]*incus.NetworkForward)
				fp.Forwards[network] = fws
			}
			if _, exists := fws[body.ListenAddress]; exists {
				writeIncusError(w, http.StatusConflict, "Forward %q already exists", body.ListenAddress)
				return
			}
			fw := &incus.NetworkForward{
				ListenAddress: body.ListenAddress,
				Ports:         body.Ports,
				Project:       project,
				Network:       network,
			}
			fws[body.ListenAddress] = fw
			writeIncusResult(w, http.StatusCreated, fw)
		default:
			writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		}
		return
	case 1:
		// Element: get or delete.
		listenAddr := rest[0]
		switch r.Method {
		case http.MethodGet:
			s.mu.Lock()
			defer s.mu.Unlock()
			fp, ok := s.projects[project]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
				return
			}
			fws, ok := fp.Forwards[network]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Forward %q not found", listenAddr)
				return
			}
			fw, ok := fws[listenAddr]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Forward %q not found", listenAddr)
				return
			}
			writeIncusResult(w, http.StatusOK, fw)
		case http.MethodDelete:
			s.mu.Lock()
			defer s.mu.Unlock()
			fp, ok := s.projects[project]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Project %q not found", project)
				return
			}
			fws, ok := fp.Forwards[network]
			if !ok {
				writeIncusError(w, http.StatusNotFound, "Forward %q not found", listenAddr)
				return
			}
			if _, ok := fws[listenAddr]; !ok {
				writeIncusError(w, http.StatusNotFound, "Forward %q not found", listenAddr)
				return
			}
			delete(fws, listenAddr)
			writeIncusResult(w, http.StatusOK, nil)
		default:
			writeIncusError(w, http.StatusMethodNotAllowed, "%s not allowed", r.Method)
		}
	default:
		writeIncusError(w, http.StatusNotFound, "unsupported forward path %v", rest)
	}
}
