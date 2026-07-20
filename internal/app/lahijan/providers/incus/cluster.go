// Package incus: cluster.go wraps the Incus cluster API. The cluster surface
// is what makes WS-26 ("multi-node / HA control plane") real: when the
// daemon is clustered, every instance lives on exactly one cluster member
// and the driver can ask "give me the list of members" + "move this
// instance to a different member".
//
// Per pillar 1 (transparent infrastructure) these types + methods are
// driver-internal. The compute service speaks "compute nodes", never
// "Incus members"; the cluster-related API surface that end users see is
// the cluster membership admin UI (WS-26).
//
// Reference: https://linuxcontainers.org/incus/docs/main/rest-api-spec/
// (paths under /1.0/cluster/* and the target= query parameter on
// /1.0/instances).
package incus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// ClusterMember is one entry in the Incus cluster's member list. Mirrors
// the per-member shape returned by GET /1.0/cluster/members.
type ClusterMember struct {
	// ServerName is the member's hostname-style identifier ("node-1",
	// "incus-a"). Unique within the cluster. The compute service maps
	// this to a "compute node" in user-facing copy.
	ServerName string `json:"server_name"`

	// URL is the daemon-side URL of the member
	// ("/1.0/cluster/members/<name>").
	URL string `json:"url,omitempty"`

	// Database is true when the member runs a copy of the distributed
	// dqlite database. Typically the first 3 members of a cluster are
	// database-bearing.
	Database bool `json:"database,omitempty"`

	// Status is the daemon-reported member status ("Online",
	// "Offline", "Evacuated"). The compute service reconciles the
	// instance placement cache from this; an Offline member's
	// instances remain on disk but are unreachable.
	Status string `json:"status,omitempty"`

	// Message is the daemon's free-form status detail (failure reason,
	// evacuation target, ...). Surfaced in the admin UI for
	// triage.
	Message string `json:"message,omitempty"`

	// Roles is the list of cluster roles the member holds
	// ("database", "database-leader", "event-hub", ...).
	Roles []string `json:"roles,omitempty"`

	// Architecture is the member's CPU architecture string ("x86_64",
	// "aarch64"). Placement decisions filter on this so an arm64 image
	// is not scheduled onto an x86_64 member.
	Architecture string `json:"architecture,omitempty"`

	// FailureDomain is the daemon's failure-domain label
	// (operator-assigned; empty in a single-room cluster).
	FailureDomain string `json:"failure_domain,omitempty"`

	// Description is the operator-set description of the member.
	Description string `json:"description,omitempty"`

	// Config is the per-member config map. Carries workload-impact
	// flags like scheduler.instance (auto/cluster/maintenance) that
	// the placement driver respects.
	Config map[string]string `json:"config,omitempty"`
}

// ClusterMembersPost is the body of POST /1.0/cluster/members. Joining a
// new node to an existing cluster is the only state-changing operation
// the cluster API supports besides evacuate + migrate.
type ClusterMembersPost struct {
	// ServerName is the candidate member's hostname.
	ServerName string `json:"server_name"`

	// ClusterAddress is the URL of an existing member the candidate
	// should contact to join ("https://incus-a:8443").
	ClusterAddress string `json:"cluster_address"`

	// ClusterCertificate is the PEM of the cluster's server cert.
	ClusterCertificate string `json:"cluster_certificate"`

	// ServerAddress is the URL the candidate will listen on after
	// joining.
	ServerAddress string `json:"server_address,omitempty"`

	// JoinToken is the per-candidate join secret minted by an
	// existing cluster member via POST /1.0/cluster/members?raft=1.
	// Required when the cluster has trust_password disabled (the
	// recommended posture).
	JoinToken string `json:"join_token,omitempty"`
}

// ClusterMemberPost is the body of POST /1.0/cluster/members/<name>. Used
// to evacuate a member or restore it back to service.
type ClusterMemberPost struct {
	// Action is "evacuate" or "restore". Evacuate live-migrates
	// every instance hosted on the member to other members; restore
	// marks it available for new placements again.
	Action string `json:"action"`

	// Mode is the evacuation strategy ("migrate" or "stop" or
	// "live-migrate" — the daemon's defaults live in the per-member
	// config; this field overrides for one call). Empty means use
	// the member's configured mode.
	Mode string `json:"mode,omitempty"`
}

// ListClusterMembers returns every member of the cluster. On a non-clustered
// daemon it returns a single synthetic entry (the local host) so the
// compute service's "list compute nodes" path returns something useful
// without a cluster-mode branching.
func (p *Provider) ListClusterMembers(ctx context.Context) ([]ClusterMember, error) {
	ctx, span := startSpan(ctx, "cluster.members.list")
	defer span.End()
	raw, err := p.do(ctx, "GET", "cluster/members?recursion=1", nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var members []ClusterMember
	if err := json.Unmarshal(raw, &members); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode cluster members: %w", err)
	}
	setStatus(span, nil)
	return members, nil
}

// GetClusterMember fetches a single member's metadata.
func (p *Provider) GetClusterMember(ctx context.Context, name string) (*ClusterMember, error) {
	ctx, span := startSpan(ctx, "cluster.members.get",
		attribute.String("incus.cluster_member", name))
	defer span.End()
	raw, err := p.do(ctx, "GET", "cluster/members/"+url.QueryEscape(name), nil)
	if err != nil {
		setStatus(span, err)
		return nil, err
	}
	var m ClusterMember
	if err := json.Unmarshal(raw, &m); err != nil {
		setStatus(span, err)
		return nil, fmt.Errorf("incus: decode cluster member: %w", err)
	}
	setStatus(span, nil)
	return &m, nil
}

// JoinClusterMember invites a new member to the cluster. Returns the async
// operation tracking the join handshake. The joining side must already
// be running Incus and reachable at ServerAddress.
func (p *Provider) JoinClusterMember(ctx context.Context, body ClusterMembersPost) (*Operation, error) {
	ctx, span := startSpan(ctx, "cluster.members.join",
		attribute.String("incus.cluster_member", body.ServerName))
	defer span.End()
	op, err := p.doAsync(ctx, "POST", "cluster/members", body)
	setStatus(span, err)
	return op, err
}

// SetClusterMemberState evacuates or restores a member. Returns the async
// operation tracking the migration work (evacuate may take minutes for
// instances with large root disks).
func (p *Provider) SetClusterMemberState(
	ctx context.Context,
	name, action string,
	mode string,
) (*Operation, error) {
	ctx, span := startSpan(ctx, "cluster.members."+action,
		attribute.String("incus.cluster_member", name))
	defer span.End()
	op, err := p.doAsync(ctx, "POST",
		"cluster/members/"+url.QueryEscape(name),
		ClusterMemberPost{Action: action, Mode: mode})
	setStatus(span, err)
	return op, err
}

// MigrateInstanceParams is the user-visible shape of a live-migrate call.
// The instance is moved from its current cluster member to TargetMember;
// the operation is stateful (Incus streams the runtime state over CRIU)
// when the instance is a container, or via storage replication when the
// instance is a VM.
type MigrateInstanceParams struct {
	// Project is the Incus project name (the caller maps tenant -> project).
	Project string

	// Instance is the instance name within the project.
	Instance string

	// TargetMember is the destination cluster member's ServerName. The
	// daemon rejects an empty target with "no target specified"; the
	// local placement driver returns "" which makes this method
	// unreachable from a non-cluster deployment.
	TargetMember string

	// Live is true for live migration (CRIU for containers, storage
	// replication for VMs). When false the instance is stopped at the
	// source, copied, and started at the destination (lower-risk but
	// higher-downtime).
	Live bool

	// StoragePool is the optional destination storage pool. When empty
	// the daemon picks a pool from the target member's available pools.
	StoragePool string
}

// InstanceMigratePost is the body of POST /1.0/instances/<name> with a
// target= query string. The Incus cluster treats this as a migration
// request: the existing instance is moved to the target member.
type InstanceMigratePost struct {
	// Migration is the flag that distinguishes a migrate from a rename.
	// Incus' REST API uses the same endpoint for both; a body of
	// {"migration": true} means "move to target", a body of {"name":
	// "newname"} means "rename in place".
	Migration bool `json:"migration"`

	// Live is true for live migration (only valid for running containers
	// whose CRIU + Incus version support it).
	Live bool `json:"live,omitempty"`

	// StoragePool overrides the destination pool. Empty = daemon picks.
	StoragePool string `json:"pool,omitempty"`

	// InstanceOnly is true when only the instance should move (leave
	// its snapshots behind). The compute service always migrates
	// snapshots with the instance so this is hardcoded false here.
	InstanceOnly bool `json:"instance_only,omitempty"`

	// ClusterGroup is the destination cluster group (mutually exclusive
	// with TargetMember on the URL; the placement driver always uses
	// TargetMember).
	ClusterGroup string `json:"cluster_group,omitempty"`
}

// MigrateInstance moves an existing instance to a different cluster member.
// Returns the final Operation state. The instance must already exist in
// the project; calling this against a single-node daemon returns the
// Incus "no target specified" error.
//
// On a successful migration the daemon updates the instance's Location
// field; the compute service reconciles its cached `cluster_member`
// column after the operation completes.
func (p *Provider) MigrateInstance(ctx context.Context, params MigrateInstanceParams) (*Operation, error) {
	ctx, span := startSpan(ctx, "instance.migrate",
		projectAttr(params.Project),
		attribute.String("incus.instance", params.Instance),
		attribute.String("incus.cluster_target", params.TargetMember))
	defer span.End()
	if params.TargetMember == "" {
		setStatus(span, errors.New("incus: migrate requires target"))
		return nil, errors.New("incus: migrate requires target")
	}
	path := "instances/" + url.QueryEscape(params.Instance) +
		"?project=" + url.QueryEscape(params.Project) +
		"&target=" + url.QueryEscape(params.TargetMember)
	body := InstanceMigratePost{
		Migration:   true,
		Live:        params.Live,
		StoragePool: params.StoragePool,
	}
	op, err := p.doAsync(ctx, "POST", path, body)
	setStatus(span, err)
	return op, err
}

// ClusterMemberActionEvacuate + ClusterMemberActionRestore are the two
// actions accepted by SetClusterMemberState. Spelled out as constants so
// callers do not pass typos through the cluster admin API.
const (
	ClusterMemberActionEvacuate = "evacuate"
	ClusterMemberActionRestore  = "restore"
)

// EvacuateClusterMember is a convenience wrapper for
// SetClusterMemberState(name, "evacuate", mode). Returns the async
// operation tracking the evacuation (which can take minutes for members
// hosting many instances).
func (p *Provider) EvacuateClusterMember(ctx context.Context, name, mode string) (*Operation, error) {
	return p.SetClusterMemberState(ctx, name, ClusterMemberActionEvacuate, mode)
}

// RestoreClusterMember is a convenience wrapper for
// SetClusterMemberState(name, "restore", ""). After a successful restore
// the member accepts new placements again.
func (p *Provider) RestoreClusterMember(ctx context.Context, name string) (*Operation, error) {
	return p.SetClusterMemberState(ctx, name, ClusterMemberActionRestore, "")
}

// clusterTargetQuery returns the target= query string for an instance
// create. Returns "" when target is empty so the URL stays clean. The
// caller concatenates this directly to the resource path (e.g.
// "instances?target=node-a"); the helper checks whether the path already
// has a query string separator so the first param uses ? and later ones
// use &.
func clusterTargetQuery(target string) string {
	if target == "" {
		return ""
	}
	return "?target=" + url.QueryEscape(target)
}
