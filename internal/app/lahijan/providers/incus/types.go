// Package incus: types.go holds the JSON request/response types for the Incus
// REST API surface this driver needs. The shapes are intentionally narrow —
// Lahijan does not use every Incus field, and untracked fields flow through
// `json:"-"` RawMessage fields where useful. Types are documented against the
// Incus REST API version they target (Incus ≥ 6.0).
//
// Reference: https://linuxcontainers.org/incus/docs/main/rest-api-spec/
package incus

import (
	"encoding/json"
	"time"
)

// Response is the top-level envelope of every Incus REST response. The
// "type" field discriminates between a synchronous result, an async operation,
// or an error.
//
//	{ "type": "success" | "async" | "error", ... }
type Response struct {
	// Type is one of "success", "async", "error". Required on every response.
	Type string `json:"type"`

	// Status is the human-readable status ("Success", "Operation created").
	Status string `json:"status,omitempty"`

	// StatusCode is the numeric status (200, 202 for async).
	StatusCode int `json:"status_code,omitempty"`

	// Operation is the URL of the async operation ("/1.0/operations/<uuid>").
	// Present only on Type == "async".
	Operation string `json:"operation,omitempty"`

	// Metadata is the response payload. For sync responses it is the resource
	// itself; for async it is the operation metadata (operation id, class,
	// resources, metadata); for errors it is nil.
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// Operation describes an async Incus operation. Long-running calls (instance
// create, image copy, snapshot) return one of these; the caller polls
// WaitOperation or subscribes to the events stream.
type Operation struct {
	// ID is the operation UUID.
	ID string `json:"id"`

	// Class is "task", "websocket", or "token".
	Class string `json:"class"`

	// CreatedAt is when the operation was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the last update timestamp.
	UpdatedAt time.Time `json:"updated_at"`

	// Status is the human-readable status ("Running", "Success", "Failure").
	Status string `json:"status"`

	// StatusCode is the numeric status (200 = success).
	StatusCode int `json:"status_code"`

	// Resources is a map of resource URLs the operation touches.
	Resources map[string][]string `json:"resources,omitempty"`

	// Metadata is operation-specific data. For websocket operations it
	// carries the per-fd secrets; for task operations it carries the
	// final result.
	Metadata json.RawMessage `json:"metadata,omitempty"`

	// MayCancel is true when the operation can be cancelled.
	MayCancel bool `json:"may_cancel"`

	// Err is the error string on failed operations.
	Err string `json:"err,omitempty"`

	// Location is the cluster member the operation runs on (cluster mode).
	Location string `json:"location,omitempty"`
}

// Project is an Incus project (per ADR-0010, each Lahijan tenant maps to one).
type Project struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	// UsedBy is the list of URLs of objects that belong to this project.
	// Populated only on a GET; not required on POST.
	UsedBy []string `json:"used_by,omitempty"`
}

// ProjectsPost is the body of POST /1.0/projects.
type ProjectsPost struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// ProjectPut is the body of PUT /1.0/projects/<name>. Used to update config
// (e.g. toggle a feature flag).
type ProjectPut struct {
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// Instance is an Incus instance (system container or VM).
type Instance struct {
	// Name is the instance's name (unique within its project).
	Name string `json:"name"`

	// Project is the parent project name.
	Project string `json:"project,omitempty"`

	// Description is the user-visible description.
	Description string `json:"description,omitempty"`

	// Architecture is the CPU architecture string ("x86_64", "aarch64").
	Architecture string `json:"architecture,omitempty"`

	// Config is the instance's config map.
	Config map[string]string `json:"config,omitempty"`

	// Devices is the device map. Keys are device names; values are the
	// per-device config maps.
	Devices map[string]map[string]string `json:"devices,omitempty"`

	// Type is "container" or "virtual-machine".
	Type string `json:"type,omitempty"`

	// Profiles is the list of profiles applied to the instance.
	Profiles []string `json:"profiles,omitempty"`

	// Status is the human-readable status ("Running", "Stopped").
	Status string `json:"status,omitempty"`

	// StatusCode is the numeric status (103 = Running, 102 = Stopped, ...).
	StatusCode int `json:"status_code,omitempty"`

	// Location is the cluster member hosting the instance.
	Location string `json:"location,omitempty"`
}

// InstancesPost is the body of POST /1.0/instances.
type InstancesPost struct {
	Name         string                       `json:"name,omitempty"`
	Project      string                       `json:"project,omitempty"`
	Architecture string                       `json:"architecture,omitempty"`
	Type         string                       `json:"type,omitempty"`
	Config       map[string]string            `json:"config,omitempty"`
	Devices      map[string]map[string]string `json:"devices,omitempty"`
	Profiles     []string                     `json:"profiles,omitempty"`
	Source       InstanceSource               `json:"source"`
	Description  string                       `json:"description,omitempty"`
}

// InstanceSource is the source of an instance's rootfs (an image alias or
// fingerprint).
type InstanceSource struct {
	Type        string `json:"type"` // "image" | "migration" | "copy" | "none"
	Alias       string `json:"alias,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Project     string `json:"project,omitempty"`
	Server      string `json:"server,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	Secret      string `json:"secret,omitempty"`
}

// InstancePut is the body of PUT /1.0/instances/<name>.
type InstancePut struct {
	Description  string                       `json:"description,omitempty"`
	Architecture string                       `json:"architecture,omitempty"`
	Config       map[string]string            `json:"config,omitempty"`
	Devices      map[string]map[string]string `json:"devices,omitempty"`
	Profiles     []string                     `json:"profiles,omitempty"`
}

// InstanceSnapshotsPost is the body of POST /1.0/instances/<name>/snapshots.
// Name is the snapshot name (required); Stateful=true captures runtime state
// alongside the filesystem (only valid when the daemon + instance support it).
type InstanceSnapshotsPost struct {
	Name     string `json:"name"`
	Stateful bool   `json:"stateful,omitempty"`
	// ExpiresAt forwards Incus' per-snapshot retention hint. Zero value is
	// omitted so the daemon treats it as "no expiry".
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// InstanceSnapshot is the response from
// GET /1.0/instances/<name>/snapshots/<snap>. Mirrors Incus' shape;
// Architecture + Config + Devices are inherited from the instance at the
// moment the snapshot was taken.
type InstanceSnapshot struct {
	Name         string                       `json:"name"`
	InstanceName string                       `json:"instance_name,omitempty"` // "<instance>/<snapshot>" on read
	Description  string                       `json:"description,omitempty"`
	Config       map[string]string            `json:"config,omitempty"`
	Devices      map[string]map[string]string `json:"devices,omitempty"`
	Architecture string                       `json:"architecture,omitempty"`
	CreatedAt    time.Time                    `json:"created_at,omitempty"`
	// Size is the filesystem size in bytes (Incus reports MiB as int).
	Size int64 `json:"size,omitempty"`
	// Stateful mirrors the stateful flag captured at create time.
	Stateful bool `json:"stateful,omitempty"`
}

// InstanceSnapshotPut is the body of PUT /1.0/instances/<name>/snapshots/<snap>.
// Used to rename a snapshot (Name) or update its description / expiry.
type InstanceSnapshotPut struct {
	Name        string    `json:"name,omitempty"`
	Description string    `json:"description,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

// InstanceSnapshotRestorePost is the body of
// POST /1.0/instances/<name>/snapshots/<snap>/restore. Stateful=true
// restores runtime state alongside the filesystem.
type InstanceSnapshotRestorePost struct {
	Stateful bool `json:"stateful,omitempty"`
}

// InstanceStatePut is the body of PUT /1.0/instances/<name>/state. Action is
// one of "start", "stop", "restart", "freeze", "unfreeze", "exec".
type InstanceStatePut struct {
	Action   string `json:"action"`
	Timeout  int    `json:"timeout,omitempty"`
	Force    bool   `json:"force,omitempty"`
	Stateful bool   `json:"stateful,omitempty"`
}

// InstanceState is the response from GET /1.0/instances/<name>/state.
type InstanceState struct {
	Status     string         `json:"status"`
	StatusCode int            `json:"status_code"`
	Pid        int64          `json:"pid,omitempty"`
	CPU        map[string]any `json:"cpu,omitempty"`
	Memory     map[string]any `json:"memory,omitempty"`
	Disk       map[string]any `json:"disk,omitempty"`
	Network    map[string]any `json:"network,omitempty"`
}

// Image is an Incus image (template used to create instances).
type Image struct {
	// Aliases is the list of human-friendly names ("ubuntu/24.04").
	Aliases []ImageAlias `json:"aliases,omitempty"`

	// Fingerprint is the sha256 of the image manifest.
	Fingerprint string `json:"fingerprint"`

	// Size is the image size in bytes.
	Size int64 `json:"size,omitempty"`

	// Architecture is the CPU architecture.
	Architecture string `json:"architecture,omitempty"`

	// Type is "container" or "virtual-machine".
	Type string `json:"type,omitempty"`

	// Public is whether the image is in the public catalog.
	Public bool `json:"public,omitempty"`

	// Properties is free-form metadata (os, release, variant, ...).
	Properties map[string]string `json:"properties,omitempty"`

	// Project is the project that owns the image (project-scoped images).
	Project string `json:"project,omitempty"`
}

// ImageAlias is a name + optional description pointing at an image fingerprint.
type ImageAlias struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ImagesPost is the body of POST /1.0/images (image copy or import).
type ImagesPost struct {
	Aliases    []ImageAlias `json:"aliases,omitempty"`
	Public     bool         `json:"public,omitempty"`
	Source     ImageSource  `json:"source"`
	AutoUpdate bool         `json:"auto_update,omitempty"`
	Project    string       `json:"project,omitempty"`
}

// ImageSource describes where to copy an image from.
type ImageSource struct {
	ImageType      string `json:"image_type,omitempty"`
	Fingerprint    string `json:"fingerprint,omitempty"`
	Alias          string `json:"alias,omitempty"`
	Project        string `json:"project,omitempty"`
	Server         string `json:"server,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	Certificate    string `json:"certificate,omitempty"`
	Secret         string `json:"secret,omitempty"`
	IncludeAliases bool   `json:"include_aliases,omitempty"`
}

// Profile is an Incus profile (named bundle of config + devices applied to
// instances).
type Profile struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	Config      map[string]string            `json:"config,omitempty"`
	Devices     map[string]map[string]string `json:"devices,omitempty"`
	Project     string                       `json:"project,omitempty"`
	UsedBy      []string                     `json:"used_by,omitempty"`
}

// ProfilesPost is the body of POST /1.0/profiles.
type ProfilesPost struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	Config      map[string]string            `json:"config,omitempty"`
	Devices     map[string]map[string]string `json:"devices,omitempty"`
	Project     string                       `json:"project,omitempty"`
}

// ProfilePut is the body of PUT /1.0/profiles/<name>.
type ProfilePut struct {
	Description string                       `json:"description,omitempty"`
	Config      map[string]string            `json:"config,omitempty"`
	Devices     map[string]map[string]string `json:"devices,omitempty"`
}

// Network is an Incus network. Project-scoped when the project has
// features.networks=true.
type Network struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type"` // "bridge", "macvlan", "physical", ...
	Config      map[string]string `json:"config,omitempty"`
	Project     string            `json:"project,omitempty"`
	UsedBy      []string          `json:"used_by,omitempty"`
	Managed     bool              `json:"managed,omitempty"`
	Status      string            `json:"status,omitempty"`
}

// NetworksPost is the body of POST /1.0/networks.
type NetworksPost struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type"`
	Config      map[string]string `json:"config,omitempty"`
	Project     string            `json:"project,omitempty"`
}

// NetworkPut is the body of PUT /1.0/networks/<name>.
type NetworkPut struct {
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// NetworkACL is a project-scoped network ACL.
type NetworkACL struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Ingress     []map[string]any  `json:"ingress,omitempty"`
	Egress      []map[string]any  `json:"egress,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	Project     string            `json:"project,omitempty"`
}

// NetworkForward is a project-scoped port forward.
type NetworkForward struct {
	ListenAddress string            `json:"listen_address"`
	Description   string            `json:"description,omitempty"`
	Ports         []map[string]any  `json:"ports,omitempty"`
	Config        map[string]string `json:"config,omitempty"`
	Project       string            `json:"project,omitempty"`
	Network       string            `json:"network,omitempty"`
}

// StoragePool is an Incus storage pool (zfs, btrfs, dir, ceph, ...).
type StoragePool struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Driver      string            `json:"driver"`
	Config      map[string]string `json:"config,omitempty"`
	UsedBy      []string          `json:"used_by,omitempty"`
}

// StoragePoolsPost is the body of POST /1.0/storage-pools.
type StoragePoolsPost struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Driver      string            `json:"driver"`
	Config      map[string]string `json:"config,omitempty"`
}

// StoragePoolPut is the body of PUT /1.0/storage-pools/<name>.
type StoragePoolPut struct {
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
}

// StorageVolume is a project-scoped storage volume.
type StorageVolume struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"` // "container", "image", "custom", "virtual-machine"
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	Project     string            `json:"project,omitempty"`
	Pool        string            `json:"pool,omitempty"`
	Location    string            `json:"location,omitempty"`
}

// StorageVolumesPost is the body of POST /1.0/storage-pools/<pool>/volumes.
type StorageVolumesPost struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Config      map[string]string `json:"config,omitempty"`
	Project     string            `json:"project,omitempty"`
	Pool        string            `json:"pool,omitempty"`
}

// InstanceExecPost is the body of POST /1.0/instances/<name>/exec.
type InstanceExecPost struct {
	Command      []string          `json:"command"`
	Environment  map[string]string `json:"environment,omitempty"`
	WaitForWS    bool              `json:"wait-for-websocket"`
	Interactive  bool              `json:"interactive,omitempty"`
	Width        int               `json:"width,omitempty"`
	Height       int               `json:"height,omitempty"`
	RecordOutput string            `json:"record-output,omitempty"`
	User         int               `json:"user,omitempty"`
	Group        int               `json:"group,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
}

// ExecMetadata is the operation metadata returned by an exec call. It carries
// a per-fd secret; the client opens a websocket per fd using the secret.
type ExecMetadata struct {
	// FDs maps the fd number ("0", "1", "2") to a websocket secret.
	FDs map[string]string `json:"fds"`

	// OperationID is present when Incus returns it separately.
	OperationID string `json:"operation,omitempty"`
}

// EventEnvelope is the JSON envelope of one event on the Incus events stream.
type EventEnvelope struct {
	// Type is the event type: "logging", "lifecycle", "operation", "metric".
	Type string `json:"type"`

	// Timestamp is when the event was emitted by the daemon.
	Timestamp time.Time `json:"timestamp"`

	// Location is the cluster member the event originated from.
	Location string `json:"location,omitempty"`

	// Metadata is the type-specific payload. Decode per to Type.
	Metadata json.RawMessage `json:"metadata"`
}

// LifecycleEvent is the metadata payload for Type == "lifecycle" events.
type LifecycleEvent struct {
	Action    string         `json:"action"`
	Source    string         `json:"source"`
	Requester map[string]any `json:"requester,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
}

// serverInfo is the body of GET /1.0 — used by Ping + Capabilities.
type serverInfo struct {
	APIStatus       string            `json:"api_status"`
	APIVersion      string            `json:"api_version"`
	Auth            string            `json:"auth"`
	Server          string            `json:"server"` // "incus"
	ServerClustered bool              `json:"server_clustered"`
	ServerName      string            `json:"server_name"`
	ServerPID       int               `json:"server_pid"`
	ServerVersion   string            `json:"server_version"`
	Environment     map[string]string `json:"environment,omitempty"`
}
