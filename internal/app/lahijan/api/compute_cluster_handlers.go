// Package api: compute_cluster_handlers.go implements the WS-26 cluster
// admin surface on the OpenAPI-derived Fiber server. Three endpoints
// under /api/v1/compute/cluster/members, one under
// /api/v1/compute/instances/{id}/migrate.
//
// Every privileged route is gated by RequirePerm via the audit gate
// middleware (extended in router.go to cover the new paths). Handlers
// are intentionally thin: parse request -> call compute service ->
// render response. The service is the single point that owns audit
// emission + provider coordination.
//
// Per pillar 1 the user-facing copy says "compute node", never
// "Incus member". The DTO mapping (cluster.ClusterMember ->
// apigen.ComputeClusterMember) lives here so the i18n keys stay in
// one place.
package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
)

// ListComputeClusterMembers handles GET /api/v1/compute/cluster/members.
//
// Returns the cluster's members. On a single-node deployment the
// compute service returns one synthetic member so the UI panel renders
// without a cluster-mode branch. Granted to tenant.viewer + above.
func (s *Server) ListComputeClusterMembers(c *fiber.Ctx) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	members, err := s.computeSvc.ListClusterMembers(c.UserContext())
	if err != nil {
		return mapComputeClusterError(c, err)
	}
	items := make([]apigen.ComputeClusterMember, 0, len(members))
	for _, m := range members {
		items = append(items, toComputeClusterMemberDTO(m))
	}
	return c.JSON(apigen.ComputeClusterMemberList{Items: items})
}

// GetComputeClusterMember handles GET
// /api/v1/compute/cluster/members/{memberName}.
//
// Returns a single member's metadata + status. Used by the admin
// detail view.
func (s *Server) GetComputeClusterMember(c *fiber.Ctx, memberName string) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	if _, _, ok := s.computeTenantAndUser(c); !ok {
		return nil
	}
	m, err := s.computeSvc.GetClusterMember(c.UserContext(), memberName)
	if err != nil {
		return mapComputeClusterError(c, err)
	}
	return c.JSON(toComputeClusterMemberDTO(m))
}

// SetComputeClusterMemberState handles POST
// /api/v1/compute/cluster/members/{memberName}/{action}.
//
// action is "evacuate" or "restore". The compute service wraps the
// Incus call + emits the audit row; the handler renders the operation
// id so the UI can poll status. Admin-only.
func (s *Server) SetComputeClusterMemberState(c *fiber.Ctx, memberName, action string) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var opID string
	var err error
	switch action {
	case "evacuate":
		opID, err = s.computeSvc.EvacuateClusterMember(c.UserContext(), tid, uid, compute.EvacuateClusterMemberParams{
			MemberName: memberName,
		})
	case "restore":
		opID, err = s.computeSvc.RestoreClusterMember(c.UserContext(), tid, uid, memberName)
	default:
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	if err != nil {
		return mapComputeClusterError(c, err)
	}
	actionEnum := apigen.ComputeClusterMemberActionActionEvacuate
	if action == "restore" {
		actionEnum = apigen.ComputeClusterMemberActionActionRestore
	}
	return c.JSON(apigen.ComputeClusterMemberAction{
		OperationId: opID,
		MemberName:  &memberName,
		Action:      &actionEnum,
	})
}

// MigrateComputeInstance handles POST
// /api/v1/compute/instances/{instanceId}/migrate.
//
// Moves an existing instance to a different compute cluster member.
// Granted to tenant.member + above; the audit row records the source
// + target member so the operator can trace placement churn.
func (s *Server) MigrateComputeInstance(c *fiber.Ctx, instanceID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var req apigen.ComputeInstanceMigrateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	if req.TargetMember == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.MigrateInstance(c.UserContext(), tid, uid, compute.MigrateInstanceParams{
		InstanceID:   uuid.UUID(instanceID),
		TargetMember: req.TargetMember,
		Live:         boolValue(req.Live),
	})
	if err != nil {
		return mapComputeClusterError(c, err)
	}
	return c.JSON(toComputeInstanceDTO(row))
}

// mapComputeClusterError translates a compute cluster service error to
// the right envelope. Reuses the compute error mapping where it can;
// the WS-26-specific sentinels map to 409 / 503 below.
func mapComputeClusterError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, compute.ErrMigrationNotSupported):
		// Single-node daemon has nowhere to migrate to.
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "compute.err_migration_unsupported", nil), nil)
	case errors.Is(err, compute.ErrNoEligibleMember):
		return SendServiceUnavailable(c,
			i18n.T(c.UserContext(), "compute.err_no_eligible_member", nil))
	}
	return mapComputeError(c, err)
}

// toComputeClusterMemberDTO maps a compute.ClusterMember (the service
// layer's view) to the OpenAPI DTO. Lives here so the i18n keys stay
// in one place.
func toComputeClusterMemberDTO(m compute.ClusterMember) apigen.ComputeClusterMember {
	roles := m.Roles
	out := apigen.ComputeClusterMember{
		ServerName:    m.ServerName,
		Url:           &m.URL,
		Database:      &m.Database,
		Status:        apigen.ComputeClusterMemberStatus(m.Status),
		Message:       &m.Message,
		Roles:         &roles,
		Architecture:  &m.Architecture,
		FailureDomain: &m.FailureDomain,
		Description:   &m.Description,
	}
	return out
}

// boolValue dereferences a *bool with a false default. Used by the
// migrate handler so the request DTO's optional "live" flag defaults
// to false (the safer, non-live migration).
func boolValue(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}
