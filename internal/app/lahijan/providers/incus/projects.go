// Package incus: projects.go wraps the Incus projects API and implements the
// tenant -> Incus project mapping (ADR-0010). Each Lahijan tenant maps to
// exactly one Incus project named "<prefix><tenant-uuid>"; the restricted
// defaults enforced here mean a tenant cannot see, modify, or escape into
// another tenant's resources through Incus itself.
//
// The restricted defaults are derived from the Incus project documentation
// ("Restricted projects") and turned on for every Lahijan-managed project.
// The compute service in WS-14 calls EnsureProject on tenant creation.
package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ProjectName returns the Incus project name for a given tenant. The format is
// "<prefix><tenant-uuid>" with all hyphens preserved (Incus accepts hyphens in
// project names). This mapping is one-way: a tenant maps to exactly one
// project; a project maps back to exactly one tenant via TenantIDFromProject.
//
// Per ADR-0010 the default prefix is "lahijan-tenant-".
func (p *Provider) ProjectName(tenantID uuid.UUID) string {
	return p.projectPrefix + tenantID.String()
}

// TenantIDFromProject reverses ProjectName. Returns an error if the project
// name does not start with the prefix or the trailing UUID is malformed.
// Used by the events listener to scope Incus events back to a tenant.
func (p *Provider) TenantIDFromProject(project string) (uuid.UUID, error) {
	if !strings.HasPrefix(project, p.projectPrefix) {
		return uuid.Nil, fmt.Errorf("incus: project %q does not have prefix %q",
			project, p.projectPrefix)
	}
	idStr := strings.TrimPrefix(project, p.projectPrefix)
	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("incus: project %q does not encode a uuid: %w", project, err)
	}
	return id, nil
}

// CreateProject creates an Incus project with the given name and config.
// Returns ErrAlreadyExists if the project already exists; callers wishing to
// be idempotent should use EnsureProject instead.
func (p *Provider) CreateProject(ctx context.Context, name, description string, config map[string]string) error {
	ctx, span := startSpan(ctx, "project.create", projectAttr(name))
	defer span.End()
	body := ProjectsPost{
		Name:        name,
		Description: description,
		Config:      config,
	}
	_, err := p.do(ctx, "POST", "projects", body)
	if errors.Is(err, ErrAlreadyExists) {
		setStatus(span, nil)
		return err
	}
	setStatus(span, err)
	return err
}

// EnsureProject creates the project if it does not exist and applies the
// restricted-defaults config (RestrictedProjectDefaults). Idempotent: a second
// call against the same tenant updates the config rather than failing.
//
// In addition to creating the project, EnsureProject seeds the project's
// default profile with a root disk (on the daemon's first storage pool) and
// an eth0 NIC (on the daemon's first managed bridge). Without this seed,
// every instance create inside the new project fails with "No root device
// could be found" — a freshly-created project's default profile starts empty.
//
// This is the canonical tenant-bootstrap hook called by the compute service
// (WS-14) when a tenant is provisioned. The defaults enforce that the tenant
// cannot see, modify, or escape into another tenant's resources.
func (p *Provider) EnsureProject(ctx context.Context, tenantID uuid.UUID) error {
	name := p.ProjectName(tenantID)
	features := p.featureConfig()
	restricted := RestrictedProjectDefaults()
	config := make(map[string]string, len(features)+len(restricted))
	for k, v := range features {
		config[k] = v
	}
	for k, v := range restricted {
		config[k] = v
	}

	ctx, span := startSpan(ctx, "project.ensure", projectAttr(name))
	defer span.End()

	body := ProjectsPost{
		Name:        name,
		Description: "Lahijan tenant " + tenantID.String(),
		Config:      config,
	}
	_, err := p.do(ctx, "POST", "projects", body)
	if err == nil {
		setStatus(span, nil)
		// Fall through to seed the default profile (below).
	} else if errors.Is(err, ErrAlreadyExists) {
		// Project exists — apply the latest restricted defaults via PUT.
		setStatus(span, nil)
		if errUpd := p.UpdateProject(ctx, name, "Lahijan tenant "+tenantID.String(), config); errUpd != nil {
			setStatus(span, errUpd)
			return errUpd
		}
		// Fall through to seed the default profile (below).
	} else {
		setStatus(span, err)
		return err
	}
	// Seed (or refresh) the project's default profile with a root disk +
	// eth0 NIC. Runs on both the create-new and update-existing paths so
	// a tenant whose project was created before this code shipped gets the
	// seed retroactively on the next EnsureProject call.
	if errSeed := p.seedDefaultProfile(ctx, name); errSeed != nil {
		setStatus(span, errSeed)
		return fmt.Errorf("incus: seed default profile for project %q: %w", name, errSeed)
	}
	return nil
}

// seedDefaultProfile populates the project's default profile with a root
// disk on the daemon's first storage pool. Without this seed, every
// instance create inside the new project fails with "No root device
// could be found" — a freshly-created project's default profile starts empty.
//
// An eth0 NIC is seeded only when features.networks is off (the default;
// Incus requires OVN for per-project networks): the project then sees the
// default project's managed networks, and eth0 attaches to defaultNetwork,
// which restricted.devices.nic=managed permits. With features.networks=true
// each project has its own network namespace, the daemon's bridges are not
// visible, and networking is configured via the UI/API instead.
//
// Idempotent: re-running just overwrites the same device with the same value.
// Called on both the create-new and update-existing paths of EnsureProject.
func (p *Provider) seedDefaultProfile(ctx context.Context, project string) error {
	pool, err := p.firstStoragePoolName(ctx)
	if err != nil {
		return fmt.Errorf("lookup default storage pool: %w", err)
	}
	devices := map[string]map[string]string{
		"root": {
			"type": "disk",
			"path": "/",
			"pool": pool,
		},
	}
	desc := "Lahijan default profile (root disk)"
	if !p.projectFeatures.Networks && p.defaultNetwork != "" {
		devices["eth0"] = map[string]string{
			"type":    "nic",
			"name":    "eth0",
			"network": p.defaultNetwork,
		}
		desc = "Lahijan default profile (root disk + eth0)"
	}
	return p.EnsureProfile(ctx, CreateProfileParams{
		Project:     project,
		Name:        "default",
		Description: desc,
		Devices:     devices,
	})
}

// firstStoragePoolName returns the daemon's first storage pool name. Used by
// seedDefaultProfile to avoid hardcoding "default" (operators may rename it).
func (p *Provider) firstStoragePoolName(ctx context.Context) (string, error) {
	raw, err := p.do(ctx, "GET", "storage-pools?recursion=1", nil)
	if err != nil {
		return "", fmt.Errorf("list storage pools: %w", err)
	}
	var pools []StoragePool
	if err := json.Unmarshal(raw, &pools); err != nil {
		return "", fmt.Errorf("decode storage pools: %w", err)
	}
	if len(pools) == 0 {
		return "", errors.New("daemon has no storage pools; initialise Incus with at least one pool")
	}
	return pools[0].Name, nil
}

// UpdateProject replaces a project's config + description. Used by
// EnsureProject to keep the restricted defaults in sync on a tenant whose
// project pre-exists.
func (p *Provider) UpdateProject(ctx context.Context, name, desc string, config map[string]string) error {
	ctx, span := startSpan(ctx, "project.update", projectAttr(name))
	defer span.End()
	body := ProjectPut{Description: desc, Config: config}
	_, err := p.do(ctx, "PUT", "projects/"+name, body)
	setStatus(span, err)
	return err
}

// DeleteProject deletes an Incus project. The project must be empty (no
// instances, networks, profiles other than default). Callers should clean up
// resources before calling this; use ForceDeleteProject for the "remove
// everything then delete the project" path.
func (p *Provider) DeleteProject(ctx context.Context, name string) error {
	ctx, span := startSpan(ctx, "project.delete", projectAttr(name))
	defer span.End()
	_, err := p.do(ctx, "DELETE", "projects/"+name, nil)
	setStatus(span, err)
	return err
}

// GetProject fetches a single project's current state.
func (p *Provider) GetProject(ctx context.Context, name string) (*Project, error) {
	ctx, span := startSpan(ctx, "project.get", projectAttr(name))
	defer span.End()
	raw, err := p.do(ctx, "GET", "projects/"+name, nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var prj Project
	if err := json.Unmarshal(raw, &prj); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode project: %w", err)
	}
	setStatus(span, nil)
	return &prj, nil
}

// ListProjects lists all Incus projects the daemon manages.
func (p *Provider) ListProjects(ctx context.Context) ([]Project, error) {
	ctx, span := startSpan(ctx, "project.list")
	defer span.End()
	raw, err := p.do(ctx, "GET", "projects?recursion=1", nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	// Incus' recursion=1 returns either a JSON array of objects or a JSON
	// array of URL strings; we accept both for robustness.
	var items []Project
	if err := json.Unmarshal(raw, &items); err == nil {
		setStatus(span, nil)
		return items, nil
	}
	var urls []string
	if err := json.Unmarshal(raw, &urls); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode projects list: %w", err)
	}
	out := make([]Project, 0, len(urls))
	for _, u := range urls {
		name := projectNameFromURL(u)
		if name == "" {
			continue
		}
		out = append(out, Project{Name: name})
	}
	setStatus(span, nil)
	return out, nil
}

// projectNameFromURL extracts the trailing segment of a project URL like
// "/1.0/projects/<name>".
func projectNameFromURL(s string) string {
	idx := strings.LastIndex(s, "/")
	if idx < 0 {
		return ""
	}
	return s[idx+1:]
}

// featureConfig maps the per-feature flags (set in Config.ProjectFeatures)
// to the Incus "features.*" config keys. Only the keys whose flag is true
// are included; this lets a deployment turn off per-tenant image namespace
// (for example) at config time.
func (p *Provider) featureConfig() map[string]string {
	out := make(map[string]string, 5)
	if p.projectFeatures.Images {
		out["features.images"] = "true"
	}
	if p.projectFeatures.Profiles {
		out["features.profiles"] = "true"
	}
	if p.projectFeatures.Networks {
		out["features.networks"] = "true"
	}
	if p.projectFeatures.StorageVolumes {
		out["features.storage.volumes"] = "true"
	}
	if p.projectFeatures.StorageBuckets {
		out["features.storage.buckets"] = "true"
	}
	return out
}

// RestrictedProjectDefaults returns the canonical restricted-defaults config
// applied to every Lahijan-managed Incus project. These defaults make a
// tenant's project a security sandbox.
//
// Schema is Incus 6.0 LTS (verified against `incus project set` on 6.0.0):
//
//   - restricted=true                       — master toggle (was "restrict" pre-6.0)
//   - restricted.devices.unix-block=block   — no host unix-block devices (was "restricted.devices.unix" pre-6.0)
//   - restricted.devices.unix-char=block    — no host unix-char devices (same)
//   - restricted.devices.usb=block          — no USB passthrough
//   - restricted.devices.gpu=block          — no GPU passthrough
//   - restricted.devices.infiniband=block   — no Infiniband
//   - restricted.devices.nic=managed        — only managed networks
//   - restricted.devices.disk=allow         — disks allowed (own storage)
//   - restricted.networks.uplinks=block     — no direct uplink attachment
//   - restricted.cluster.target=block       — no targeting specific cluster members
//   - restricted.snapshots=allow            — restricted=true blocks snapshots by default (WS-25)
//   - restricted.backups=allow              — restricted=true blocks backups by default (WS-25)
//   - restricted.containers.lowlevel=block  — no raw container config
//   - restricted.virtual-machines.lowlevel=block — no raw VM config
//
// Note: restricted.cluster.groups is intentionally NOT set. It is the list
// of cluster groups the project MAY target, so a real daemon reads "block"
// as a group name and rejects the project ("Cluster group \"block\"
// doesn't exist"). restricted.cluster.target=block already forbids any
// explicit placement.
//
// Note: restricted.networks.subnets is intentionally NOT set. Incus 6.0
// rejects "block" — the value must be a comma-separated list of
// "<uplink>:<subnet>" entries (or empty to allow all). Since we already
// block every uplink via restricted.networks.uplinks=block above, the
// tenant has no uplink to allocate a subnet on, making subnets unreachable.
//
// Returns a fresh map on every call so callers can mutate without affecting
// other callers.
func RestrictedProjectDefaults() map[string]string {
	return map[string]string{
		"restricted":                           "true",
		"restricted.devices.unix-block":        "block",
		"restricted.devices.unix-char":         "block",
		"restricted.devices.usb":               "block",
		"restricted.devices.gpu":               "block",
		"restricted.devices.infiniband":        "block",
		"restricted.devices.nic":               "managed",
		"restricted.devices.disk":              "allow",
		"restricted.networks.uplinks":          "block",
		"restricted.cluster.target":            "block",
		"restricted.snapshots":                 "allow",
		"restricted.backups":                   "allow",
		"restricted.containers.lowlevel":       "block",
		"restricted.virtual-machines.lowlevel": "block",
	}
}
