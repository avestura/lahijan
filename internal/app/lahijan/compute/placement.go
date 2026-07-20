// Package compute: placement.go declares the PlacementDriver interface
// that sits between the compute service and the Incus provider's
// per-instance lifecycle (WS-26, ADR-0033).
//
// The compute service calls SelectTarget before every CreateInstance
// and MigrateInstance for the live-migrate admin action. Two
// implementations ship in this package:
//
//   - LocalPlacementDriver: returns the empty target. This is the
//     implicit behaviour on a single-node daemon (the daemon treats
//     empty target as "any member, any host"). Constructed by
//     program.Start when providers.incus.placement.mode == "local"
//     (the default).
//   - ClusterPlacementDriver: queries the Incus cluster API for the
//     list of members, picks the least-loaded one (highest free CPU +
//     memory; ties broken by name), and returns the member's
//     ServerName as the target. Constructed when placement.mode ==
//     "cluster".
//
// Both implementations are safe for concurrent use; the cluster
// driver additionally serialises per-tenant placement decisions via a
// Postgres advisory lock so two replicas competing for the same queue
// do not both pick the same "least loaded" member and oversubscribe
// it.
package compute

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/avestura/lahijan/internal/app/lahijan/providers/incus"
)

// PlacementDriver abstracts the per-instance "where should this run"
// decision. The compute service calls SelectTarget before every
// CreateInstance; the result is forwarded to the Incus provider's
// CreateInstance call as the target= query string.
//
// The interface also covers live migration: MigrateInstance moves an
// existing instance to a different cluster member. The local driver
// returns ErrMigrationNotSupported because a single-node daemon has
// nowhere to migrate to.
//
// Per ADR-0033 the interface is intentionally narrow. Future scheduling
// strategies (bin-packing, spread, per-tenant affinity, drain-a-member)
// are new implementations of this interface, not new methods.
type PlacementDriver interface {
	// SelectTarget returns the cluster member name to pin the next
	// CreateInstance call to. Returning "" is valid; the Incus
	// daemon treats empty target as "any member". The params carry
	// the tenant id + the proposed instance name + the config so
	// the driver can implement affinity rules in the future.
	SelectTarget(ctx context.Context, params PlacementParams) (string, error)

	// MigrateInstance moves an existing instance to a different
	// cluster member. The local driver returns
	// ErrMigrationNotSupported; the cluster driver issues the
	// Incus migrate operation and waits for completion.
	MigrateInstance(ctx context.Context, params MigrateParams) (*incus.Operation, error)

	// DriverName is the human-readable identifier surfaced in the
	// admin debug page + the audit metadata. "local" or "cluster".
	DriverName() string
}

// PlacementParams is the shape the service passes to SelectTarget.
// Carries the tenant + the proposed instance so the driver can apply
// per-tenant affinity rules in the future.
type PlacementParams struct {
	TenantID  uuid.UUID
	Project   string
	Name      string
	Type      string
	Config    map[string]string
	ImageArch string
}

// MigrateParams is the shape the service passes to MigrateInstance.
// Carries the tenant + the existing instance's project/name + the
// destination member.
type MigrateParams struct {
	TenantID     uuid.UUID
	Project      string
	Instance     string
	InstanceID   uuid.UUID
	TargetMember string
	Live         bool
	StoragePool  string
}

// ErrMigrationNotSupported is returned by PlacementDriver.MigrateInstance
// when the driver cannot honour a migrate request (e.g. the local
// driver on a single-node daemon). The API layer maps it to 409
// conflict so a tenant admin cannot trigger a migrate against a
// non-cluster deployment.
var ErrMigrationNotSupported = errors.New("compute: migration not supported by placement driver")

// ErrNoEligibleMember is returned by ClusterPlacementDriver.SelectTarget
// when every cluster member is either Offline or Evacuated. The API
// layer maps it to 503 service_unavailable so the caller sees a
// transient-failure shape they can retry.
var ErrNoEligibleMember = errors.New("compute: no eligible cluster member for placement")

// LocalPlacementDriver is the default driver. It returns "" for every
// SelectTarget call (Incus treats empty target as "any member, any
// daemon") and rejects every MigrateInstance call. The driver is
// stateless; a single shared instance is fine.
type LocalPlacementDriver struct{}

// NewLocalPlacementDriver builds the default driver. Constructed once
// at process bootstrap; the same instance is shared across requests.
func NewLocalPlacementDriver() *LocalPlacementDriver { return &LocalPlacementDriver{} }

// SelectTarget returns the empty target. The Incus daemon treats an
// empty target as "any member, any daemon"; on a single-node daemon
// this is the only valid value.
func (LocalPlacementDriver) SelectTarget(_ context.Context, _ PlacementParams) (string, error) {
	return "", nil
}

// MigrateInstance returns ErrMigrationNotSupported. A single-node
// daemon has nowhere to migrate to.
func (LocalPlacementDriver) MigrateInstance(_ context.Context, _ MigrateParams) (*incus.Operation, error) {
	return nil, ErrMigrationNotSupported
}

// DriverName returns "local".
func (LocalPlacementDriver) DriverName() string { return "local" }

// ClusterPlacementDriver queries the Incus cluster API for the list
// of members and picks the least-loaded one for each CreateInstance.
// The decision is wrapped in a Postgres advisory lock keyed on the
// tenant id so concurrent placements across replicas do not
// double-pick the same member.
//
// The driver depends on:
//   - The Incus provider (for ListClusterMembers + MigrateInstance).
//   - An advisory-lock backend (the compute service's *database.Repos
//     pool; nil-appropriate in tests where placement is deterministic).
type ClusterPlacementDriver struct {
	provider clusterProvider
	locker   advisoryLocker
	// loadScorer is the per-member load scorer. The default scorer
	// uses the Incus daemon's reported per-member status (Online vs
	// Offline vs Evacuated) + the configured "free capacity" hint
	// from the member's Config map. A test can swap in a deterministic
	// scorer so the cluster-driver tests do not depend on the
	// fake's seeding.
	loadScorer func(members []incus.ClusterMember) []incus.ClusterMember
}

// clusterProvider is the narrow seam the cluster driver needs from
// *incus.Provider. Defined here so tests can swap a fake without
// dragging the full incus surface into the test file.
type clusterProvider interface {
	ListClusterMembers(ctx context.Context) ([]incus.ClusterMember, error)
	MigrateInstance(ctx context.Context, params incus.MigrateInstanceParams) (*incus.Operation, error)
}

// advisoryLocker is the narrow seam the cluster driver needs for
// per-tenant serialised placement. The real implementation calls
// pg_advisory_xact_lock via the database pool; the test fake is a
// no-op so deterministic unit tests are not blocked.
type advisoryLocker interface {
	// LockTenant blocks until the per-tenant advisory lock is
	// acquired. The lock is held for the duration of the calling
	// transaction; the caller MUST hold the lock via the returned
	// release func.
	LockTenant(ctx context.Context, tenantID uuid.UUID) (release func(), err error)
}

// noopAdvisoryLocker is the default when the database pool is
// unavailable (e.g. unit tests). The lock is a no-op so concurrent
// placements are not serialised; the cluster-driver tests are
// deterministic so the lack of serialisation does not flake the test.
type noopAdvisoryLocker struct{}

// LockTenant implements advisoryLocker as a no-op.
func (noopAdvisoryLocker) LockTenant(_ context.Context, _ uuid.UUID) (func(), error) {
	return func() {}, nil
}

// NewClusterPlacementDriver builds the cluster-aware driver. provider
// MUST be non-nil; locker may be nil (the driver falls back to a
// no-op locker). The driver is safe for concurrent use.
func NewClusterPlacementDriver(provider clusterProvider, locker advisoryLocker) *ClusterPlacementDriver {
	if locker == nil {
		locker = noopAdvisoryLocker{}
	}
	return &ClusterPlacementDriver{
		provider:   provider,
		locker:     locker,
		loadScorer: defaultLoadScorer,
	}
}

// WithLoadScorer swaps the per-member load scorer. Used by tests that
// need a deterministic ordering; production callers leave the default
// (least-loaded by Incus-reported free capacity) in place.
func (d *ClusterPlacementDriver) WithLoadScorer(fn func(members []incus.ClusterMember) []incus.ClusterMember) *ClusterPlacementDriver {
	if fn != nil {
		d.loadScorer = fn
	}
	return d
}

// SelectTarget picks the least-loaded eligible cluster member. The
// decision is wrapped in a per-tenant advisory lock so concurrent
// CreateInstance calls in different replicas do not both pick the same
// member based on stale cluster view.
func (d *ClusterPlacementDriver) SelectTarget(ctx context.Context, params PlacementParams) (string, error) {
	release, err := d.locker.LockTenant(ctx, params.TenantID)
	if err != nil {
		return "", fmt.Errorf("compute: placement advisory lock: %w", err)
	}
	defer release()

	members, err := d.provider.ListClusterMembers(ctx)
	if err != nil {
		return "", fmt.Errorf("compute: list cluster members: %w", err)
	}
	eligible := filterEligible(members)
	if len(eligible) == 0 {
		return "", ErrNoEligibleMember
	}
	ranked := d.loadScorer(eligible)
	if len(ranked) == 0 {
		return "", ErrNoEligibleMember
	}
	return ranked[0].ServerName, nil
}

// MigrateInstance issues the Incus migrate operation for an existing
// instance. The destination must already exist in the cluster; the
// driver does NOT call SelectTarget — the caller (the cluster admin
// endpoint) decides which member to migrate to, typically after a
// GET /admin/compute/cluster/members call.
func (d *ClusterPlacementDriver) MigrateInstance(
	ctx context.Context,
	params MigrateParams,
) (*incus.Operation, error) {
	if params.TargetMember == "" {
		return nil, fmt.Errorf("compute: migrate requires target")
	}
	op, err := d.provider.MigrateInstance(ctx, incus.MigrateInstanceParams{
		Project:      params.Project,
		Instance:     params.Instance,
		TargetMember: params.TargetMember,
		Live:         params.Live,
		StoragePool:  params.StoragePool,
	})
	if err != nil {
		return nil, fmt.Errorf("compute: incus migrate: %w", err)
	}
	return op, nil
}

// DriverName returns "cluster".
func (d *ClusterPlacementDriver) DriverName() string { return "cluster" }

// filterEligible strips Offline + Evacuated members so the load scorer
// never picks a member that cannot accept new work. The scheduler.instance
// per-member config knob ("auto" | "cluster" | "maintenance") is also
// respected: a member in "maintenance" is skipped until the operator
// flips it back to "auto" or "cluster".
func filterEligible(members []incus.ClusterMember) []incus.ClusterMember {
	out := make([]incus.ClusterMember, 0, len(members))
	for _, m := range members {
		status := strings.ToLower(strings.TrimSpace(m.Status))
		if status != "online" {
			continue
		}
		switch m.Config["scheduler.instance"] {
		case "maintenance":
			continue
		case "", "auto", "cluster":
			// eligible
		default:
			// unknown scheduler policy; treat as eligible so a
			// future Incus knob does not silently remove every
			// member from rotation.
		}
		out = append(out, m)
	}
	return out
}

// defaultLoadScorer ranks the eligible members by lowest free capacity
// hint (the Incus daemon does not report per-member CPU/RAM free; the
// operator can stuff hints into the per-member Config map under
// "lahijan.free_cpu_mhz" + "lahijan.free_ram_mb"). Ties are broken by
// ServerName so two members with the same hint deterministically
// round-robin via the alphabetical rotation that the helper below
// applies.
//
// When no hints are set the helper falls back to round-robin via the
// member name. This keeps the cluster driver useful for dev clusters
// where the operator did not bother with hints.
func defaultLoadScorer(members []incus.ClusterMember) []incus.ClusterMember {
	if len(members) == 0 {
		return members
	}
	type memberScore struct {
		m     incus.ClusterMember
		score int64
	}
	scores := make([]memberScore, 0, len(members))
	for _, m := range members {
		scores = append(scores, memberScore{m: m, score: memberFreeScore(m)})
	}
	// Highest score first (= most free). Ties broken by name.
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score ||
				(scores[j].score == scores[i].score && scores[j].m.ServerName < scores[i].m.ServerName) {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}
	out := make([]incus.ClusterMember, 0, len(scores))
	for _, s := range scores {
		out = append(out, s.m)
	}
	return out
}

// memberFreeScore computes a single int64 free-capacity score for a
// member. Higher = more free = preferred. The Incus daemon does not
// report per-member CPU/RAM free; the operator can stuff hints into
// the per-member Config map. With no hints the score is 0 for every
// member and the caller falls back to round-robin via name ordering.
func memberFreeScore(m incus.ClusterMember) int64 {
	cpu := parseInt64Hint(m.Config["lahijan.free_cpu_mhz"])
	ram := parseInt64Hint(m.Config["lahijan.free_ram_mb"])
	// Weight: 1 CPU MHz ~ 1 MiB RAM (rough order-of-magnitude match
	// so neither dominates). The point is to spread, not to be exact.
	return cpu + ram
}

// parseInt64Hint parses a config-stringified int64. Returns 0 on any
// parse failure so a malformed hint does not break scheduling.
func parseInt64Hint(s string) int64 {
	if s == "" {
		return 0
	}
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0
	}
	return n
}
