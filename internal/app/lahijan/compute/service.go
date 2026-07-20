// Package compute implements Lahijan's user-facing compute module (WS-14).
// It orchestrates calls to the Incus provider driver (Phase 3, WS-11), the
// tenant-scoped compute_* tables (this WS), the RBAC policy + audit emitter
// (WS-08), the WASM event bus (WS-10b), the River job queue (WS-09), and the
// billing seam (WS-17, defined here as an interface so this WS ships
// standalone).
//
// Layering:
//
//	api/compute_handlers.go  -> compute.Service -> providers/incus + database
//	                                     \-> auth/audit
//	                                     \-> wasm/eventbus
//	                                     \-> auth/rbac.Require (websocket / job paths)
//
// Every privileged action calls RequirePerm via the api/middleware gate;
// every state-changing action emits an audit event before the side effect
// (status=pending) and marks the outcome after. Every lifecycle transition
// emits into the WASM event bus so plugins can react.
//
// Per pillar 1, this package is named "compute" — never "incus". End users
// do not see the word Incus anywhere in the API or UI.
package compute

import (
	"context"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/avestura/lahijan/internal/app/lahijan/auth/audit"
	"github.com/avestura/lahijan/internal/app/lahijan/auth/rbac"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
	"github.com/avestura/lahijan/internal/app/lahijan/wasm/eventbus"
)

// ResourceInstance is the audit resource_type for compute instances.
const ResourceInstance = "instance"

// ResourceImage / ResourceProfile / ResourceNetwork / ResourceVolume mirror
// the audit_log.resource_type vocabulary for the compute area.
const (
	ResourceImage   = "compute_image"
	ResourceProfile = "compute_profile"
	ResourceNetwork = "compute_network"
	ResourceVolume  = "compute_storage_volume"
)

// AuditAction constants for compute. Mirror the rbac.PermCompute* slugs but
// use past-tense verbs so the audit row reads "what happened" not "what was
// requested". WS-08's i18n keys are auto-derived from these (dots -> underscores).
const (
	AuditInstanceCreate  = "compute.instance.create"
	AuditInstanceStart   = "compute.instance.start"
	AuditInstanceStop    = "compute.instance.stop"
	AuditInstanceRestart = "compute.instance.restart"
	AuditInstanceFreeze  = "compute.instance.freeze"
	AuditInstanceDelete  = "compute.instance.delete"
	AuditInstanceUpdate  = "compute.instance.update"
	AuditInstanceExec    = "compute.instance.exec"
	// AuditInstanceConsoleVNCConnect records that a user opened a
	// graphical (noVNC) console session to a running VM (WS-24). Distinct
	// from exec: VM-only, RFB protocol, longer-lived session.
	AuditInstanceConsoleVNCConnect = "compute.instance.console.vnc.connect"
)

// Service is the entrypoint every compute API handler talks to. It owns the
// Incus provider (Phase 3 driver), the tenant-scoped repositories, the audit
// emitter, and the WASM event bus. Every method takes a context carrying the
// tenant id (set by the tenant middleware) and the user id (set by the auth
// middleware); the service enforces RBAC at the api/middleware layer for HTTP
// requests and via rbac.Require for non-HTTP entry points (websockets, jobs).
type Service struct {
	provider incusProvider
	repos    *database.Repos
	audit    audit.Emitter
	bus      eventBus
	policy   rbac.PolicyEvaluator
	quotas   QuotaConfig
}

// incusProvider is the narrow seam the service needs from
// providers/incus.Provider. Defined here so tests can swap a fake without
// dragging the full incus surface into the test file. Split into per-area
// sub-interfaces so the surface stays reviewable; the concrete
// *incus.Provider satisfies the union.
type incusProvider interface {
	incusProjectOps
	incusInstanceOps
	incusProfileOps
	incusNetworkOps
	incusVolumeOps
	incusExecOps
	incusConsoleOps
}

// incusProjectOps covers tenant -> Incus project mapping + bootstrap.
//
//nolint:interfacebloat // intentional: 3 small methods
type incusProjectOps interface {
	ProjectName(tenantID uuid.UUID) string
	EnsureProject(ctx context.Context, tenantID uuid.UUID) error
	Ping(ctx context.Context) error
}

// incusInstanceOps covers the instance lifecycle (CRUD + state).
type incusInstanceOps interface {
	CreateInstance(ctx context.Context, params incus.CreateInstanceParams) (*incus.Operation, error)
	GetInstance(ctx context.Context, project, name string) (*incus.Instance, error)
	ListInstances(ctx context.Context, project string) ([]incus.Instance, error)
	SetInstanceState(ctx context.Context, project, name string, action incus.InstanceAction, force bool, timeoutSecs int) (*incus.Operation, error)
	GetInstanceState(ctx context.Context, project, name string) (*incus.InstanceState, error)
	UpdateInstance(ctx context.Context, project, name string, body incus.InstancePut) (*incus.Operation, error)
	DeleteInstance(ctx context.Context, project, name string) (*incus.Operation, error)
}

// incusProfileOps covers profile CRUD.
type incusProfileOps interface {
	CreateProfile(ctx context.Context, params incus.CreateProfileParams) error
	DeleteProfile(ctx context.Context, project, name string) error
}

// incusNetworkOps covers network CRUD.
type incusNetworkOps interface {
	CreateNetwork(ctx context.Context, project string, body incus.NetworksPost) error
	DeleteNetwork(ctx context.Context, project, name string) error
}

// incusVolumeOps covers storage volume CRUD.
type incusVolumeOps interface {
	CreateStorageVolume(ctx context.Context, pool string, body incus.StorageVolumesPost) error
	DeleteStorageVolume(ctx context.Context, pool, project, volType, name string) error
}

// incusExecOps covers the exec websocket proxy.
type incusExecOps interface {
	Exec(ctx context.Context, params incus.ExecParams) (*incus.ExecResult, error)
}

// incusConsoleOps covers the WS-24 VNC console proxy: open an Incus console
// operation (returns the op id + per-fd secret) and dial the per-fd
// websocket (returns a *websocket.Conn speaking raw RFB bytes).
type incusConsoleOps interface {
	OpenVNCConsole(ctx context.Context, project, instance string) (incus.ConsoleSession, error)
	DialVNCConsole(ctx context.Context, opID, secret string) (*websocket.Conn, error)
}

// eventBus is the narrow seam the service needs from *eventbus.Bus.
type eventBus interface {
	Emit(ctx context.Context, e eventbus.Event) error
}

// Config carries the few process-wide knobs the service needs.
type Config struct {
	// Quotas is the per-tenant resource cap. The defaults are documented in
	// WS-14's "Open questions"; deployers override via config (TODO: WS-14
	// follow-up wires this to conf).
	Quotas QuotaConfig
}

// New builds a Service. Every dependency is required except `bus` and
// `policy` (nil disables event emission / non-HTTP RequirePerm).
func New(
	provider incusProvider,
	r *database.Repos,
	emitter audit.Emitter,
	bus eventBus,
	policy rbac.PolicyEvaluator,
	cfg Config,
) *Service {
	if emitter == nil {
		emitter = audit.NoopEmitter{}
	}
	quotas := cfg.Quotas
	if quotas.IsZero() {
		quotas = DefaultQuotas()
	}
	return &Service{
		provider: provider,
		repos:    r,
		audit:    emitter,
		bus:      bus,
		policy:   policy,
		quotas:   quotas,
	}
}
