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
	"net/netip"

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

	// WS-25: snapshot + backup + policy audit actions. Mirror the rbac
	// slugs but use past-tense verbs so the audit row reads "what
	// happened" not "what was requested".
	AuditSnapshotCreate           = "compute.snapshot.create"
	AuditSnapshotDelete           = "compute.snapshot.delete"
	AuditSnapshotRestore          = "compute.snapshot.restore"
	AuditSnapshotPolicyCreate     = "compute.snapshot_policy.create"
	AuditSnapshotPolicyUpdate     = "compute.snapshot_policy.update"
	AuditSnapshotPolicyDelete     = "compute.snapshot_policy.delete"
	AuditBackupTargetCreate       = "compute.backup.target.create"
	AuditBackupTargetUpdate       = "compute.backup.target.update"
	AuditBackupTargetDelete       = "compute.backup.target.delete"
	AuditBackupCreate             = "compute.backup.create"
	AuditBackupDelete             = "compute.backup.delete"
	AuditBackupRestore            = "compute.backup.restore"
	AuditScheduledSnapshotTaken   = "compute.snapshot.taken"
	AuditScheduledSnapshotPruned  = "compute.snapshot.pruned"
	AuditScheduledBackupCompleted = "compute.backup.completed"

	// WS-26: cluster placement audit actions. Mirror the rbac slugs
	// but use past-tense verbs so the audit row reads "what
	// happened" not "what was requested".
	AuditInstanceMigrated  = "compute.instance.migrate"
	AuditClusterMemberList = "compute.cluster.member.list"
)

// ResourceSnapshot / ResourceSnapshotPolicy / ResourceBackupTarget /
// ResourceBackup mirror the audit_log.resource_type vocabulary for the
// WS-25 snapshot + backup surface.
const (
	ResourceSnapshot       = "compute_snapshot"
	ResourceSnapshotPolicy = "compute_snapshot_policy"
	ResourceBackupTarget   = "compute_backup_target"
	ResourceBackup         = "compute_backup"
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
	// placement is the per-instance scheduling driver (WS-26, ADR-0033).
	// Defaults to LocalPlacementDriver when nil so the service always has
	// a non-nil driver. ClusterPlacementDriver is wired by program.Start
	// when providers.incus.placement.mode == "cluster".
	placement PlacementDriver
	// crypto is the AES-GCM envelope the WS-25 backup-target path uses to
	// encrypt + decrypt the per-target credential blob at the repo seam.
	// nil-appropriate in tests / when WS-25 is disabled; the snapshot +
	// backup CRUD paths surface ErrCryptoRequired when the envelope is
	// missing on a path that needs it.
	crypto cryptoEnvelope
	// ptrPublisher is the reverse-DNS auto-publish seam the WS-30
	// floating-IP attach/detach path uses to write/remove PTR records
	// into the operator's reverse zone (ADR-0037 sub-decision A).
	// nil-appropriate in tests / when DNS module is disabled; the path
	// silently no-ops.
	ptrPublisher ptrPublisher
	// meter is the WS-17 usage-meter seam the WS-30 floating-IP
	// allocate/release path uses to start/stop per-IP-hour usage events
	// (ADR-0037). nil-appropriate in tests; the path silently no-ops.
	meter meter
	// forwardNetwork is the name of the Incus network the best-effort
	// forward is pushed to on attach (ADR-0037 sub-decision B). Empty
	// means the operator topology does not allow a forward and the
	// attach path records forward_push_status="unsupported".
	forwardNetwork string
}

// cryptoEnvelope is the narrow seam the service needs from
// *secrets.Crypto. Defined here so tests can stub the encrypt/decrypt
// pair without dragging the secrets package into every test file.
type cryptoEnvelope interface {
	Seal(plaintext string) (string, error)
	Open(ciphertext string) (string, error)
}

// incusProvider is the narrow seam the service needs from
// providers/incus.Provider. Defined here so tests can swap a fake without
// dragging the full incus surface into the test file. Split into per-area
// sub-interfaces so the surface stays reviewable; the concrete
// *incus.Provider satisfies the union.
//
//nolint:interfacebloat // intentional: 9 sub-interfaces composed by category; each is small
type incusProvider interface {
	incusProjectOps
	incusInstanceOps
	incusSnapshotOps
	incusProfileOps
	incusNetworkOps
	incusVolumeOps
	incusExecOps
	incusConsoleOps
	incusForwardOps
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

// incusForwardOps covers the WS-30 best-effort network-forward push path
// (ADR-0037 sub-decision B). The methods accept the project + network
// name + listen address; the compute service resolves these from the
// floating IP row + the configured forward network.
type incusForwardOps interface {
	CreateNetworkForward(ctx context.Context, project, network, listenAddress string, ports []map[string]any) error
	DeleteNetworkForward(ctx context.Context, project, network, listenAddress string) error
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

// incusSnapshotOps covers the WS-25 snapshot surface: create / list /
// get / rename / delete / restore / export. ExportSnapshot returns the
// raw tarball bytes the backup worker streams to a BackupTarget.
//
//nolint:interfacebloat // intentional: 8 small methods mirroring the Incus REST surface
type incusSnapshotOps interface {
	CreateSnapshot(ctx context.Context, params incus.CreateSnapshotParams) (*incus.Operation, error)
	ListInstanceSnapshots(ctx context.Context, project, instance string) ([]incus.InstanceSnapshot, error)
	GetSnapshot(ctx context.Context, project, instance, snapshot string) (*incus.InstanceSnapshot, error)
	RenameSnapshot(ctx context.Context, project, instance, snapshot, newName string) (*incus.Operation, error)
	DeleteSnapshot(ctx context.Context, project, instance, snapshot string) (*incus.Operation, error)
	RestoreSnapshot(ctx context.Context, project, instance, snapshot string, stateful bool) (*incus.Operation, error)
	ExportSnapshot(ctx context.Context, project, instance, snapshot string) ([]byte, error)
}

// eventBus is the narrow seam the service needs from *eventbus.Bus.
type eventBus interface {
	Emit(ctx context.Context, e eventbus.Event) error
}

// ptrPublisher is the narrow seam the WS-30 floating-IP path needs from
// the DNS module (ADR-0037 sub-decision A). PublishPTR writes a PTR
// record for the IP into the operator's reverse zone; UnpublishPTR
// removes it. Implementations are tenant-aware (the operator's reverse
// zone is owned by some tenant; the adapter from dns.Service sets up
// that tenant's context internally).
//
// nil-appropriate in tests; the path silently no-ops when the seam is
// unset (e.g. when the DNS module is disabled in config).
type ptrPublisher interface {
	PublishPTR(ctx context.Context, zoneID uuid.UUID, ip netip.Addr, target string) error
	UnpublishPTR(ctx context.Context, zoneID uuid.UUID, ip netip.Addr) error
}

// meter is the narrow seam the WS-30 floating-IP path needs from the
// billing module (ADR-0037). StartIPUsage begins emitting per-IP-hour
// usage events for the allocation; StopIPUsage stops them. The default
// cadence mirrors the compute metering (once per minute).
//
// nil-appropriate in tests; the path silently no-ops when the seam is
// unset (e.g. when billing is in ledger-only mode without metering).
type meter interface {
	StartIPUsage(ctx context.Context, tenantID uuid.UUID, ip netip.Addr, floatingIPID uuid.UUID) error
	StopIPUsage(ctx context.Context, tenantID uuid.UUID, ip netip.Addr) error
}

// Config carries the few process-wide knobs the service needs.
type Config struct {
	// Quotas is the per-tenant resource cap. The defaults are documented in
	// WS-14's "Open questions"; deployers override via config (TODO: WS-14
	// follow-up wires this to conf).
	Quotas QuotaConfig

	// Crypto is the AES-GCM envelope the WS-25 backup-target path uses to
	// encrypt + decrypt the per-target credential blob. Nil-appropriate in
	// tests that do not exercise the backup target surface; the
	// CreateBackupTarget / UpdateBackupTargetSecret paths surface
	// ErrCryptoRequired when nil.
	Crypto cryptoEnvelope

	// Placement is the per-instance scheduling driver (WS-26, ADR-0033).
	// Nil-appropriate — New falls back to LocalPlacementDriver. Pass a
	// ClusterPlacementDriver when providers.incus.placement.mode ==
	// "cluster".
	Placement PlacementDriver

	// PTRPublisher is the reverse-DNS auto-publish seam (WS-30,
	// ADR-0037). Nil-appropriate — the attach/detach path silently
	// no-ops when unset.
	PTRPublisher ptrPublisher

	// Meter is the per-IP-hour usage-meter seam (WS-30, ADR-0037).
	// Nil-appropriate — the allocate/release path silently no-ops when
	// unset.
	Meter meter

	// ForwardNetwork is the Incus network name the best-effort forward
	// is pushed to on attach (WS-30, ADR-0037 sub-decision B). Empty
	// means the operator topology does not allow a forward and the
	// attach path records forward_push_status="unsupported".
	ForwardNetwork string
}

// New builds a Service. Every dependency is required except `bus`,
// `policy`, and `crypto` (nil disables event emission / non-HTTP
// RequirePerm / backup-target credential encryption).
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
	placement := cfg.Placement
	if placement == nil {
		placement = NewLocalPlacementDriver()
	}
	return &Service{
		provider:       provider,
		repos:          r,
		audit:          emitter,
		bus:            bus,
		policy:         policy,
		quotas:         quotas,
		placement:      placement,
		crypto:         cfg.Crypto,
		ptrPublisher:   cfg.PTRPublisher,
		meter:          cfg.Meter,
		forwardNetwork: cfg.ForwardNetwork,
	}
}

// WithCrypto returns a copy of the service with the AES-GCM envelope
// replaced. Used by program.Start when the WS-25 backup-target path is
// wired after the rest of the service is built (the envelope is sourced
// from the same key the auth subsystem uses, but the compute service is
// constructed before that key is parsed in some bootstrap orders).
func (s *Service) WithCrypto(c cryptoEnvelope) *Service {
	if s == nil {
		return s
	}
	out := *s
	out.crypto = c
	return &out
}
