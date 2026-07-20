// Package api: compute_snapshots_handlers.go implements the OpenAPI-derived
// snapshot + backup + policy endpoints (WS-25). Mirrors the thin-handler
// pattern of compute_handlers.go: parse request -> call service -> render.
// Every privileged route is gated by RequirePerm via the audit gate
// middleware (extended in router.go to cover the new paths).
package api

import (
	"encoding/json"
	"errors"

	"github.com/avestura/lahijan/api/gen/go"
	"github.com/avestura/lahijan/internal/app/lahijan/compute"
	"github.com/avestura/lahijan/internal/app/lahijan/database"
	"github.com/avestura/lahijan/internal/app/lahijan/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// mapComputeSnapshotError translates a snapshot/policy/backup error to the
// right envelope. Reuses the compute error mapping where it can; the
// WS-25-specific sentinels map to 404 / 409 / 400 below.
func mapComputeSnapshotError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, compute.ErrSnapshotNotFound),
		errors.Is(err, compute.ErrSnapshotPolicyNotFound),
		errors.Is(err, compute.ErrBackupNotFound),
		errors.Is(err, compute.ErrBackupTargetNotFound):
		return SendNotFound(c, i18n.T(c.UserContext(), "compute.err_not_found", nil))
	case errors.Is(err, compute.ErrSnapshotNameTaken),
		errors.Is(err, compute.ErrSnapshotPolicyNameTaken),
		errors.Is(err, compute.ErrBackupTargetNameTaken):
		return SendError(c, fiber.StatusConflict, CodeConflict,
			i18n.T(c.UserContext(), "compute.err_name_taken", nil), nil)
	case errors.Is(err, compute.ErrInvalidCadence),
		errors.Is(err, compute.ErrUnknownBackupTargetKind):
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	case errors.Is(err, compute.ErrCryptoRequired):
		return SendInternal(c, i18n.T(c.UserContext(), "auth.err_internal", nil))
	}
	return mapComputeError(c, err)
}

// ---------------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------------

// ListComputeSnapshots handles GET /api/v1/compute/instances/{instanceId}/snapshots.
func (s *Server) ListComputeSnapshots(c *fiber.Ctx, instanceID openapi_types.UUID, params apigen.ListComputeSnapshotsParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListSnapshots(c.UserContext(), tid, instanceID, limit, offset)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	items := make([]apigen.ComputeSnapshot, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeSnapshotDTO(row))
	}
	total, err := s.computeSvc.CountSnapshots(c.UserContext(), tid, instanceID)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(apigen.ComputeSnapshotPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeSnapshot handles POST /api/v1/compute/instances/{instanceId}/snapshots.
func (s *Server) CreateComputeSnapshot(c *fiber.Ctx, instanceID openapi_types.UUID, _ apigen.CreateComputeSnapshotParams) error {
	var req apigen.ComputeSnapshotCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Name == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	stateful := false
	if req.Stateful != nil {
		stateful = *req.Stateful
	}
	row, err := s.computeSvc.TakeSnapshot(c.UserContext(), tid, uid, compute.CreateSnapshotParams{
		InstanceID:  instanceID,
		Name:        req.Name,
		Stateful:    stateful,
		Description: ptrString(req.Description),
	}, nil)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeSnapshotDTO(row))
}

// GetComputeSnapshot handles GET /api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}.
func (s *Server) GetComputeSnapshot(c *fiber.Ctx, instanceID, snapshotID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	_ = instanceID // unused; the snapshot id is unique within the tenant
	row, err := s.computeSvc.GetSnapshot(c.UserContext(), tid, snapshotID)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(toComputeSnapshotDTO(row))
}

// DeleteComputeSnapshot handles DELETE /api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}.
func (s *Server) DeleteComputeSnapshot(c *fiber.Ctx, instanceID, snapshotID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	_ = instanceID
	if err := s.computeSvc.DeleteSnapshot(c.UserContext(), tid, uid, snapshotID); err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// RestoreComputeSnapshot handles POST /api/v1/compute/instances/{instanceId}/snapshots/{snapshotId}/restore.
func (s *Server) RestoreComputeSnapshot(c *fiber.Ctx, instanceID, snapshotID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	_ = instanceID
	stateful := false
	var req apigen.ComputeSnapshotRestoreRequest
	if err := c.BodyParser(&req); err == nil && req.Stateful != nil {
		stateful = *req.Stateful
	}
	row, err := s.computeSvc.RestoreSnapshot(c.UserContext(), tid, uid, snapshotID, stateful)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(toComputeSnapshotDTO(row))
}

// ---------------------------------------------------------------------------
// Snapshot policies
// ---------------------------------------------------------------------------

// ListComputeSnapshotPolicies handles GET /api/v1/compute/snapshot-policies.
func (s *Server) ListComputeSnapshotPolicies(c *fiber.Ctx, params apigen.ListComputeSnapshotPoliciesParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListSnapshotPolicies(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	items := make([]apigen.ComputeSnapshotPolicy, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeSnapshotPolicyDTO(row))
	}
	total, err := s.computeSvc.CountSnapshotPolicies(c.UserContext(), tid)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(apigen.ComputeSnapshotPolicyPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeSnapshotPolicy handles POST /api/v1/compute/snapshot-policies.
func (s *Server) CreateComputeSnapshotPolicy(c *fiber.Ctx, _ apigen.CreateComputeSnapshotPolicyParams) error {
	var req apigen.ComputeSnapshotPolicyCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Name == "" || req.Cadence == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var retain int32 = 7
	if req.RetainCount != nil {
		retain = int32(*req.RetainCount)
	}
	row, err := s.computeSvc.CreateSnapshotPolicy(c.UserContext(), tid, uid, compute.CreateSnapshotPolicyParams{
		Name:        req.Name,
		InstanceID:  openAPIUUIDPtr(req.InstanceId),
		Cadence:     req.Cadence,
		RetainCount: retain,
		TargetID:    openAPIUUIDPtr(req.TargetId),
		Enabled:     boolPtrOr(req.Enabled, true),
	})
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeSnapshotPolicyDTO(row))
}

// GetComputeSnapshotPolicy handles GET /api/v1/compute/snapshot-policies/{policyId}.
func (s *Server) GetComputeSnapshotPolicy(c *fiber.Ctx, policyID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetSnapshotPolicy(c.UserContext(), tid, policyID)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(toComputeSnapshotPolicyDTO(row))
}

// UpdateComputeSnapshotPolicy handles PATCH /api/v1/compute/snapshot-policies/{policyId}.
func (s *Server) UpdateComputeSnapshotPolicy(c *fiber.Ctx, policyID openapi_types.UUID) error {
	var req apigen.ComputeSnapshotPolicyUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	var retain int32 = 7
	if req.RetainCount != nil {
		retain = int32(*req.RetainCount)
	}
	row, err := s.computeSvc.UpdateSnapshotPolicy(c.UserContext(), tid, uid, policyID, compute.UpdateSnapshotPolicyParams{
		Name:        req.Name,
		InstanceID:  openAPIUUIDPtr(req.InstanceId),
		Cadence:     req.Cadence,
		RetainCount: retain,
		TargetID:    openAPIUUIDPtr(req.TargetId),
		Enabled:     boolPtrOr(req.Enabled, true),
	})
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(toComputeSnapshotPolicyDTO(row))
}

// DeleteComputeSnapshotPolicy handles DELETE /api/v1/compute/snapshot-policies/{policyId}.
func (s *Server) DeleteComputeSnapshotPolicy(c *fiber.Ctx, policyID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteSnapshotPolicy(c.UserContext(), tid, uid, policyID); err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Backup targets
// ---------------------------------------------------------------------------

// ListComputeBackupTargets handles GET /api/v1/compute/backup-targets.
func (s *Server) ListComputeBackupTargets(c *fiber.Ctx, params apigen.ListComputeBackupTargetsParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListBackupTargets(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	items := make([]apigen.ComputeBackupTarget, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeBackupTargetDTO(row))
	}
	total, err := s.computeSvc.CountBackupTargets(c.UserContext(), tid)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(apigen.ComputeBackupTargetPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// CreateComputeBackupTarget handles POST /api/v1/compute/backup-targets.
func (s *Server) CreateComputeBackupTarget(c *fiber.Ctx, _ apigen.CreateComputeBackupTargetParams) error {
	var req apigen.ComputeBackupTargetCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "auth.err_bad_request", nil), nil)
	}
	if req.Name == "" || req.Kind == "" {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	cfg, err := convertAnyToConfig(&req.Config)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	secret, err := convertAnyToSecret(&req.Secret)
	if err != nil {
		return SendBadRequest(c, i18n.T(c.UserContext(), "compute.err_bad_request", nil), nil)
	}
	row, err := s.computeSvc.CreateBackupTarget(c.UserContext(), tid, uid, compute.CreateBackupTargetParams{
		Name:        req.Name,
		Kind:        string(req.Kind),
		Description: ptrString(req.Description),
		Config:      cfg,
		Secret:      secret,
		Enabled:     boolPtrOr(req.Enabled, true),
	})
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(toComputeBackupTargetDTO(row))
}

// GetComputeBackupTarget handles GET /api/v1/compute/backup-targets/{targetId}.
func (s *Server) GetComputeBackupTarget(c *fiber.Ctx, targetID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetBackupTarget(c.UserContext(), tid, targetID)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(toComputeBackupTargetDTO(row))
}

// DeleteComputeBackupTarget handles DELETE /api/v1/compute/backup-targets/{targetId}.
func (s *Server) DeleteComputeBackupTarget(c *fiber.Ctx, targetID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteBackupTarget(c.UserContext(), tid, uid, targetID); err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

// ListComputeBackups handles GET /api/v1/compute/backups.
func (s *Server) ListComputeBackups(c *fiber.Ctx, params apigen.ListComputeBackupsParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListBackups(c.UserContext(), tid, limit, offset)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	items := make([]apigen.ComputeBackup, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeBackupDTO(row))
	}
	total, err := s.computeSvc.CountBackups(c.UserContext(), tid)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(apigen.ComputeBackupPage{
		Items: items, Total: int(total), Limit: int(limit), Offset: int(offset),
	})
}

// ListComputeBackupsByInstance handles GET /api/v1/compute/instances/{instanceId}/backups.
func (s *Server) ListComputeBackupsByInstance(c *fiber.Ctx, instanceID openapi_types.UUID, params apigen.ListComputeBackupsByInstanceParams) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	limit, offset := computePageParams(params.Limit, params.Offset)
	rows, err := s.computeSvc.ListBackupsByInstance(c.UserContext(), tid, instanceID, limit, offset)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	items := make([]apigen.ComputeBackup, 0, len(rows))
	for _, row := range rows {
		items = append(items, toComputeBackupDTO(row))
	}
	return c.JSON(apigen.ComputeBackupPage{
		Items: items, Total: len(items), Limit: int(limit), Offset: int(offset),
	})
}

// GetComputeBackup handles GET /api/v1/compute/backups/{backupId}.
func (s *Server) GetComputeBackup(c *fiber.Ctx, backupID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, _, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	row, err := s.computeSvc.GetBackup(c.UserContext(), tid, backupID)
	if err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.JSON(toComputeBackupDTO(row))
}

// DeleteComputeBackup handles DELETE /api/v1/compute/backups/{backupId}.
func (s *Server) DeleteComputeBackup(c *fiber.Ctx, backupID openapi_types.UUID) error {
	if s.computeSvc == nil {
		return SendError(c, fiber.StatusNotImplemented, CodeNotImplemented, computeDisabledMsg(c), nil)
	}
	tid, uid, ok := s.computeTenantAndUser(c)
	if !ok {
		return nil
	}
	if err := s.computeSvc.DeleteBackup(c.UserContext(), tid, uid, backupID); err != nil {
		return mapComputeSnapshotError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ---------------------------------------------------------------------------
// DTO helpers
// ---------------------------------------------------------------------------

func toComputeSnapshotDTO(row database.ComputeSnapshot) apigen.ComputeSnapshot {
	out := apigen.ComputeSnapshot{
		Id:          row.ID,
		InstanceId:  row.InstanceID,
		Name:        row.Name,
		Stateful:    row.Stateful,
		SizeBytes:   &row.SizeBytes,
		Description: ptrIfNonEmpty(row.Description),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   &row.UpdatedAt,
	}
	if row.ExpiresAt != nil {
		t := *row.ExpiresAt
		out.ExpiresAt = &t
	}
	if row.PolicyID != nil {
		p := *row.PolicyID
		out.PolicyId = &p
	}
	return out
}

func toComputeSnapshotPolicyDTO(row database.ComputeSnapshotPolicy) apigen.ComputeSnapshotPolicy {
	out := apigen.ComputeSnapshotPolicy{
		Id:          row.ID,
		Name:        row.Name,
		Cadence:     row.Cadence,
		RetainCount: int(row.RetainCount),
		Enabled:     row.Enabled,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   &row.UpdatedAt,
	}
	if row.InstanceID != nil {
		v := *row.InstanceID
		out.InstanceId = &v
	}
	if row.TargetID != nil {
		v := *row.TargetID
		out.TargetId = &v
	}
	if row.LastRunAt != nil {
		t := *row.LastRunAt
		out.LastRunAt = &t
	}
	if row.NextRunAt != nil {
		t := *row.NextRunAt
		out.NextRunAt = &t
	}
	return out
}

func toComputeBackupTargetDTO(row database.ComputeBackupTarget) apigen.ComputeBackupTarget {
	out := apigen.ComputeBackupTarget{
		Id:          row.ID,
		Name:        row.Name,
		Kind:        apigen.ComputeBackupTargetKind(row.Kind),
		Description: ptrIfNonEmpty(row.Description),
		Enabled:     row.Enabled,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   &row.UpdatedAt,
	}
	if len(row.ConfigJson) > 0 && string(row.ConfigJson) != "{}" {
		var raw map[string]any
		if err := json.Unmarshal(row.ConfigJson, &raw); err == nil {
			out.Config = &raw
		}
	}
	return out
}

func toComputeBackupDTO(row database.ComputeBackup) apigen.ComputeBackup {
	out := apigen.ComputeBackup{
		Id:             row.ID,
		SnapshotId:     row.SnapshotID,
		InstanceId:     row.InstanceID,
		TargetId:       row.TargetID,
		RemoteLocation: ptrIfNonEmpty(row.RemoteLocation),
		SizeBytes:      &row.SizeBytes,
		Status:         apigen.ComputeBackupStatus(row.Status),
		ErrorMessage:   ptrIfNonEmpty(row.ErrorMessage),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      &row.UpdatedAt,
	}
	if row.ChecksumSha256 != "" {
		out.ChecksumSha256 = &row.ChecksumSha256
	}
	return out
}

// convertAnyToConfig converts the apigen "map[string]any" shape into the
// typed BackupTargetConfig. Returns a zero-value config when raw is nil.
func convertAnyToConfig(raw *map[string]any) (compute.BackupTargetConfig, error) {
	if raw == nil {
		return compute.BackupTargetConfig{}, nil
	}
	bytes, err := json.Marshal(*raw)
	if err != nil {
		return compute.BackupTargetConfig{}, err
	}
	return compute.DecodeConfig(bytes)
}

// convertAnyToSecret converts the apigen "map[string]any" shape into the
// typed BackupTargetSecret. Returns a zero-value secret when raw is nil.
func convertAnyToSecret(raw *map[string]any) (compute.BackupTargetSecret, error) {
	if raw == nil {
		return compute.BackupTargetSecret{}, nil
	}
	bytes, err := json.Marshal(*raw)
	if err != nil {
		return compute.BackupTargetSecret{}, err
	}
	return compute.DecodeSecret(bytes)
}

// openAPIUUIDPtr dereferences an openapi_types.UUID pointer. Returns nil
// when the input is nil so the caller's *uuid.UUID param stays nil too.
func openAPIUUIDPtr(p *openapi_types.UUID) *uuid.UUID {
	if p == nil {
		return nil
	}
	v := uuid.UUID(*p)
	return &v
}

// boolPtrOr returns the dereferenced value when non-nil, otherwise the
// default. Used for the optional bool params the create/update handlers
// forward to the service.
func boolPtrOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
